package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

func Prepare(configDir string) error {
	if err := migrateLegacyConfigIfPresent(configDir); err != nil {
		return err
	}
	return ensureSplitConfigFiles(configDir)
}

func migrateLegacyConfigIfPresent(configDir string) error {
	legacyPaths := []string{
		filepath.Join(configDir, legacyConfigYAML),
		filepath.Join(configDir, legacyConfigYML),
	}

	var legacyPath string
	for _, p := range legacyPaths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			legacyPath = p
			break
		}
	}
	if legacyPath == "" {
		return nil
	}

	proxyPath := filepath.Join(configDir, proxyConfigFile)
	corsPath := filepath.Join(configDir, corsConfigFile)
	logsPath := filepath.Join(configDir, logsConfigFile)

	anySplitExists := fileExists(proxyPath) || fileExists(corsPath) || fileExists(logsPath)
	if !anySplitExists {
		data, err := os.ReadFile(legacyPath)
		if err != nil {
			return fmt.Errorf("failed to read legacy config %s: %w", filepath.Base(legacyPath), err)
		}

		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("failed to parse legacy config %s: %w", filepath.Base(legacyPath), err)
		}

		proxyDoc, corsDoc, logsDoc, err := splitLegacyDocPreserveComments(&doc)
		if err != nil {
			return err
		}

		// Extract cors/log blocks (if any). These files contain the inner object, without outer 'cors:'/'log:'.
		if corsDoc != nil && !fileExists(corsPath) {
			if err := WriteYAMLNodeFile(corsPath, corsDoc); err != nil {
				return err
			}
		}
		if logsDoc != nil && !fileExists(logsPath) {
			if err := WriteYAMLNodeFile(logsPath, logsDoc); err != nil {
				return err
			}
		}
		if !fileExists(proxyPath) {
			if err := WriteYAMLNodeFile(proxyPath, proxyDoc); err != nil {
				return err
			}
		}
	}

	// After splitting, ensure any missing files are created from examples.
	if err := ensureSplitConfigFiles(configDir); err != nil {
		return err
	}

	// Finally, rename legacy config to config.backup.yaml (or .yml)
	legacyBase := filepath.Base(legacyPath)
	backupName := "config.backup" + filepath.Ext(legacyBase)
	backupPath := filepath.Join(configDir, backupName)
	if fileExists(backupPath) {
		ts := time.Now().UTC().Format("20060102T150405Z")
		backupName = "config.backup." + ts + filepath.Ext(legacyBase)
		backupPath = filepath.Join(configDir, backupName)
	}
	if err := os.Rename(legacyPath, backupPath); err != nil {
		return fmt.Errorf("failed to rename legacy config to %s: %w", backupName, err)
	}
	return nil
}

func splitLegacyDocPreserveComments(doc *yaml.Node) (proxyDoc, corsDoc, logsDoc *yaml.Node, err error) {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil, nil, fmt.Errorf("invalid legacy config: expected a YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, nil, fmt.Errorf("invalid legacy config: expected a mapping at the document root")
	}

	proxyDoc = deepCopyYAMLNode(doc)
	proxyRoot := proxyDoc.Content[0]

	var corsKey, corsVal, logKey, logVal *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		switch k.Value {
		case "cors":
			corsKey = deepCopyYAMLNode(k)
			corsVal = deepCopyYAMLNode(v)
		case "log":
			logKey = deepCopyYAMLNode(k)
			logVal = deepCopyYAMLNode(v)
		}
	}

	// Remove extracted keys from proxy output.
	filtered := proxyRoot.Content[:0]
	for i := 0; i+1 < len(proxyRoot.Content); i += 2 {
		k := proxyRoot.Content[i]
		if k.Kind == yaml.ScalarNode && (k.Value == "cors" || k.Value == "log") {
			continue
		}
		filtered = append(filtered, k, proxyRoot.Content[i+1])
	}
	proxyRoot.Content = filtered

	if corsVal != nil {
		mergeDroppedKeyComments(corsKey, corsVal)
		corsDoc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{corsVal}}
	}
	if logVal != nil {
		mergeDroppedKeyComments(logKey, logVal)
		logsDoc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{logVal}}
	}

	return proxyDoc, corsDoc, logsDoc, nil
}

func deepCopyYAMLNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	cpy := *n
	if n.Alias != nil {
		cpy.Alias = deepCopyYAMLNode(n.Alias)
	}
	if len(n.Content) > 0 {
		cpy.Content = make([]*yaml.Node, 0, len(n.Content))
		for _, c := range n.Content {
			cpy.Content = append(cpy.Content, deepCopyYAMLNode(c))
		}
	}
	return &cpy
}

func mergeDroppedKeyComments(key, val *yaml.Node) {
	if key == nil || val == nil {
		return
	}

	// Comments that were attached to the removed key should still be preserved.
	if key.HeadComment != "" {
		val.HeadComment = joinComments(key.HeadComment, val.HeadComment)
	}
	if key.LineComment != "" {
		// Inline comment on the dropped key can't remain inline; preserve it as a head comment.
		val.HeadComment = joinComments(key.LineComment, val.HeadComment)
	}
	if key.FootComment != "" {
		val.FootComment = joinComments(val.FootComment, key.FootComment)
	}
}

func ensureSplitConfigFiles(configDir string) error {
	if err := ensureFileFromExample(configDir, proxyConfigFile, proxyExampleFile); err != nil {
		return err
	}
	if err := ensureFileFromExample(configDir, corsConfigFile, corsExampleFile); err != nil {
		return err
	}
	if err := ensureFileFromExample(configDir, logsConfigFile, logsExampleFile); err != nil {
		return err
	}
	return nil
}

func ensureFileFromExample(configDir, filename, exampleFilename string) error {
	dst := filepath.Join(configDir, filename)
	if fileExists(dst) {
		return nil
	}
	// Prefer split example file
	examplePath := filepath.Join(exampleDir(), exampleFilename)
	if fileExists(examplePath) {
		return copyFile(examplePath, dst)
	}

	// proxy.yaml is required; do not silently create an empty config.
	if filename == proxyConfigFile {
		return fmt.Errorf("missing required %s and no example found at %s", proxyConfigFile, examplePath)
	}

	// Optional files: keep behaviour of ensuring they exist.
	return os.WriteFile(dst, []byte{}, 0666)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filepath.Base(src), err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0777); err != nil {
		return fmt.Errorf("failed to create config dir for %s: %w", filepath.Base(dst), err)
	}
	if err := os.WriteFile(dst, data, 0666); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(dst), err)
	}
	return nil
}
