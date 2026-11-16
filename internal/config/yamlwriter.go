package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func joinComments(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n" + b
}

func WriteYAMLNodeFile(path string, doc *yaml.Node) error {
	if doc == nil {
		return fmt.Errorf("failed to write %s: nil yaml document", filepath.Base(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
		return fmt.Errorf("failed to create config dir for %s: %w", filepath.Base(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(path), err)
	}
	defer file.Close()

	enc := yaml.NewEncoder(file)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return fmt.Errorf("failed to encode yaml for %s: %w", filepath.Base(path), err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("failed to finalize yaml for %s: %w", filepath.Base(path), err)
	}
	return nil
}

func WriteYAMLNodeFileAtomic(path string, doc *yaml.Node) error {
	if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
		return fmt.Errorf("failed to create config dir for %s: %w", filepath.Base(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("failed to stage write of %s: %w", filepath.Base(path), err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName) // no-op after a successful rename
		}
	}()

	enc := yaml.NewEncoder(tmp)
	enc.SetIndent(2)
	err = enc.Encode(doc)
	_ = enc.Close()
	if err != nil {
		tmp.Close()
		return fmt.Errorf("failed to encode yaml for %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Chmod(0666); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to chmod staged %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close staged %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to finalize %s: %w", filepath.Base(path), err)
	}
	tmpName = "" // renamed; nothing to clean up
	return nil
}
