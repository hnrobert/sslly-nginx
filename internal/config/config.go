package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	legacyConfigYAML = "config.yaml"
	legacyConfigYML  = "config.yml"

	proxyConfigFile = "proxy.yaml"
	corsConfigFile  = "cors.yaml"
	logsConfigFile  = "logs.yaml"

	exampleDirDefault = "/etc/sslly/configs/"

	proxyExampleFile = "proxy.example.yaml"
	corsExampleFile  = "cors.example.yaml"
	logsExampleFile  = "logs.example.yaml"
)

func exampleDir() string {
	if v := os.Getenv("SSLLY_EXAMPLE_DIR"); v != "" {
		return v
	}
	return exampleDirDefault
}

// CORSConfig represents CORS configuration for a domain or wildcard
type CORSConfig struct {
	AllowOrigin      string   `yaml:"allow_origin"`      // Access-Control-Allow-Origin (default: "*")
	AllowMethods     []string `yaml:"allow_methods"`     // Access-Control-Allow-Methods
	AllowHeaders     []string `yaml:"allow_headers"`     // Access-Control-Allow-Headers
	ExposeHeaders    []string `yaml:"expose_headers"`    // Access-Control-Expose-Headers
	MaxAge           int      `yaml:"max_age"`           // Access-Control-Max-Age in seconds (default: 1728000)
	AllowCredentials bool     `yaml:"allow_credentials"` // Access-Control-Allow-Credentials (default: false)
}

// LogLevelConfig represents log level configuration for a component
type LogLevelConfig struct {
	Level string `yaml:"level"` // Log level: debug, info, warn, error (case insensitive, default: info)
}

// NginxLogConfig represents nginx-specific log configuration
type NginxLogConfig struct {
	Level      string `yaml:"level"`       // Display log level: debug, info, warn, error (default: info)
	StderrAs   string `yaml:"stderr_as"`   // Nginx stderr log level: warn or error (default: error)
	StderrShow string `yaml:"stderr_show"` // Display stderr as: warn or error (default: same as stderr_as)
}

// LogConfig represents logging configuration
type LogConfig struct {
	SSLLY LogLevelConfig `yaml:"sslly"` // SSLLY-NGINX component log level
	Nginx NginxLogConfig `yaml:"nginx"` // NGINX-PROCS component log configuration
}

// Upstream represents a backend server configuration
type Upstream struct {
	Scheme string // Protocol scheme: "http" or "https" (default: "http")
	Host   string // IP address or hostname (default: 127.0.0.1)
	Port   string // Port number
	Path   string // Optional path prefix for routing
}

type Config struct {
	Log   LogConfig             `yaml:"log"`
	CORS  map[string]CORSConfig `yaml:"cors"`
	Ports map[string][]string   `yaml:",inline"`
}

type StaticSiteSpec struct {
	Dir     string
	Port    int
	HasPort bool
}

// ParseStaticSiteKey parses a proxy.yaml mapping key that represents a local static directory.
//
// Rules:
// - If the key starts with '.' or '/', it's treated as a filesystem directory.
// - If the key ends with ':PORT' (PORT must be numeric), that port is used.
// - Otherwise, the app will auto-assign an available local port (starting from 10000).
func ParseStaticSiteKey(key string) (StaticSiteSpec, bool, error) {
	k := strings.TrimSpace(strings.TrimSuffix(key, ":"))
	if k == "" {
		return StaticSiteSpec{}, false, nil
	}
	if !(strings.HasPrefix(k, ".") || strings.HasPrefix(k, "/")) {
		return StaticSiteSpec{}, false, nil
	}

	// Optional ':PORT' suffix.
	if idx := strings.LastIndex(k, ":"); idx > 0 && idx < len(k)-1 {
		portPart := k[idx+1:]
		if isNumeric(portPart) {
			p, err := strconv.Atoi(portPart)
			if err != nil {
				return StaticSiteSpec{}, true, fmt.Errorf("invalid static site port %q: %w", portPart, err)
			}
			if p <= 0 || p > 65535 {
				return StaticSiteSpec{}, true, fmt.Errorf("invalid static site port %d", p)
			}
			dir := strings.TrimSpace(k[:idx])
			if dir == "" {
				return StaticSiteSpec{}, true, fmt.Errorf("invalid static site path: empty")
			}
			return StaticSiteSpec{Dir: dir, Port: p, HasPort: true}, true, nil
		}
	}

	return StaticSiteSpec{Dir: k, HasPort: false}, true, nil
}

// ParseUpstream parses the key format which can be:
// - "1234" -> Upstream{Scheme: "http", Host: "127.0.0.1", Port: "1234", Path: ""}
// - "192.168.31.6:1234" -> Upstream{Scheme: "http", Host: "192.168.31.6", Port: "1234", Path: ""}
// - "192.168.31.6:1234/api" -> Upstream{Scheme: "http", Host: "192.168.31.6", Port: "1234", Path: "/api"}
// - "[::1]:9000" -> Upstream{Scheme: "http", Host: "::1", Port: "9000", Path: ""} (IPv6 format)
// - "example-server.local:8080" -> Upstream{Scheme: "http", Host: "example-server.local", Port: "8080", Path: ""}
// - "[https]192.168.50.2:1234" -> Upstream{Scheme: "https", Host: "192.168.50.2", Port: "1234", Path: ""}
// - "[https]www.baidu.com" -> Upstream{Scheme: "https", Host: "www.baidu.com", Port: "443", Path: ""}
// - "www.example.com" -> Upstream{Scheme: "http", Host: "www.example.com", Port: "80", Path: ""}
func ParseUpstream(key string) Upstream {
	// Remove trailing colon if present (for YAML keys like "192.168.31.6:1234:")
	key = strings.TrimSuffix(key, ":")

	// Check for [https] prefix
	scheme := "http"
	if strings.HasPrefix(key, "[https]") {
		scheme = "https"
		key = strings.TrimPrefix(key, "[https]")
	}

	// Check for path suffix (e.g., "/api")
	path := ""
	if slashIdx := strings.Index(key, "/"); slashIdx > 0 {
		path = key[slashIdx:]
		key = key[:slashIdx]
	}

	// Handle IPv6 format [host]:port
	if strings.HasPrefix(key, "[") {
		closeBracket := strings.Index(key, "]")
		if closeBracket > 0 && closeBracket < len(key)-1 && key[closeBracket+1] == ':' {
			return Upstream{
				Scheme: scheme,
				Host:   key[1:closeBracket],
				Port:   key[closeBracket+2:],
				Path:   path,
			}
		}
	}

	// Check if key contains a colon (IP:port or hostname:port format)
	if strings.Contains(key, ":") {
		// Use LastIndex to handle cases like "::1:9000" (split from the last colon)
		lastColon := strings.LastIndex(key, ":")
		host := key[:lastColon]
		port := key[lastColon+1:]

		// If host part is empty or port part contains colon, it's likely plain port or invalid
		// Examples: ":8080" should be treated as port 8080, "::1:9000" needs special handling
		if host == "" || strings.Contains(port, ":") {
			// If it looks like IPv6 (multiple colons and no brackets), treat whole thing as plain port
			if strings.Count(key, ":") > 1 {
				// This is likely malformed - default to treating as plain port
				return Upstream{
					Scheme: scheme,
					Host:   "127.0.0.1",
					Port:   key,
					Path:   path,
				}
			}
			// Single colon at start (:8080) - treat as plain port
			if host == "" {
				return Upstream{
					Scheme: scheme,
					Host:   "127.0.0.1",
					Port:   port,
					Path:   path,
				}
			}
		}

		// Valid host:port format (could be IP or hostname)
		return Upstream{
			Scheme: scheme,
			Host:   host,
			Port:   port,
			Path:   path,
		}
	}

	// Check if it's a pure number (port only)
	if isNumeric(key) {
		return Upstream{
			Scheme: scheme,
			Host:   "127.0.0.1",
			Port:   key,
			Path:   path,
		}
	}

	// Otherwise it's a hostname/domain without port - use default port
	defaultPort := "80"
	if scheme == "https" {
		defaultPort = "443"
	}
	return Upstream{
		Scheme: scheme,
		Host:   key,
		Port:   defaultPort,
		Path:   path,
	}
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func Load(configDir string) (*Config, error) {
	if err := Prepare(configDir); err != nil {
		return nil, err
	}

	proxyPath := filepath.Join(configDir, proxyConfigFile)
	proxyData, err := os.ReadFile(proxyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", proxyConfigFile, err)
	}

	var config Config
	if err := yaml.Unmarshal(proxyData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", proxyConfigFile, err)
	}

	// Load optional logs config (content is the inner object, without outer 'log:')
	logsPath := filepath.Join(configDir, logsConfigFile)
	if data, err := os.ReadFile(logsPath); err == nil {
		var logsCfg LogConfig
		if err := yaml.Unmarshal(data, &logsCfg); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", logsConfigFile, err)
		}
		config.Log = logsCfg
	}

	// Load optional CORS config (content is the inner object, without outer 'cors:')
	corsPath := filepath.Join(configDir, corsConfigFile)
	if data, err := os.ReadFile(corsPath); err == nil {
		var corsCfg map[string]CORSConfig
		if err := yaml.Unmarshal(data, &corsCfg); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", corsConfigFile, err)
		}
		config.CORS = corsCfg
	}

	// Defensive: do not allow these keys to appear as ports.
	delete(config.Ports, "cors")
	delete(config.Ports, "log")

	if len(config.Ports) == 0 {
		return nil, fmt.Errorf("config is empty or invalid (%s has no proxy mappings)", proxyConfigFile)
	}

	return &config, nil
}

// Prepare ensures the configuration directory is ready for loading:
// - If legacy config.yaml/config.yml exists, it is migrated to split files.
// - If split files are missing, they are created from the corresponding example files.
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
			if err := writeYAMLNodeFile(corsPath, corsDoc); err != nil {
				return err
			}
		}
		if logsDoc != nil && !fileExists(logsPath) {
			if err := writeYAMLNodeFile(logsPath, logsDoc); err != nil {
				return err
			}
		}
		if !fileExists(proxyPath) {
			if err := writeYAMLNodeFile(proxyPath, proxyDoc); err != nil {
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

func joinComments(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n" + b
}

func writeYAMLNodeFile(path string, doc *yaml.Node) error {
	if doc == nil {
		return fmt.Errorf("failed to write %s: nil yaml document", filepath.Base(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
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

func writeYAMLFile(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal yaml for %s: %w", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config dir for %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, data, 0666); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(path), err)
	}
	return nil
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
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create config dir for %s: %w", filepath.Base(dst), err)
	}
	if err := os.WriteFile(dst, data, 0666); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(dst), err)
	}
	return nil
}
