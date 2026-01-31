package config

import (
	"fmt"
	"os"
	"path/filepath"
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

		var root map[any]any
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("failed to parse legacy config %s: %w", filepath.Base(legacyPath), err)
		}

		// Extract cors/log blocks (if any)
		if corsVal, ok := root["cors"]; ok {
			if !fileExists(corsPath) {
				if err := writeYAMLFile(corsPath, corsVal); err != nil {
					return err
				}
			}
			delete(root, "cors")
		}
		if logVal, ok := root["log"]; ok {
			if !fileExists(logsPath) {
				if err := writeYAMLFile(logsPath, logVal); err != nil {
					return err
				}
			}
			delete(root, "log")
		}

		if !fileExists(proxyPath) {
			if err := writeYAMLFile(proxyPath, root); err != nil {
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

	// Optional files: keep behavior of ensuring they exist.
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
