package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// CORSConfig represents CORS configuration for a domain or wildcard
type CORSConfig struct {
	AllowOrigin      string   `yaml:"allow_origin"`      // Access-Control-Allow-Origin (default: "*")
	AllowMethods     []string `yaml:"allow_methods"`     // Access-Control-Allow-Methods
	AllowHeaders     []string `yaml:"allow_headers"`     // Access-Control-Allow-Headers
	ExposeHeaders    []string `yaml:"expose_headers"`    // Access-Control-Expose-Headers
	MaxAge           int      `yaml:"max_age"`           // Access-Control-Max-Age in seconds (default: 1728000)
	AllowCredentials bool     `yaml:"allow_credentials"` // Access-Control-Allow-Credentials (default: false)
}

// Upstream represents a backend server configuration
type Upstream struct {
	Scheme string // Protocol scheme: "http" or "https" (default: "http")
	Host   string // IP address or hostname (default: 127.0.0.1)
	Port   string // Port number
	Path   string // Optional path prefix for routing
}

type Config struct {
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
	// Try config.yaml first, then config.yml
	configPaths := []string{
		filepath.Join(configDir, "config.yaml"),
		filepath.Join(configDir, "config.yml"),
	}

	var configPath string
	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			configPath = path
			break
		}
	}

	if configPath == "" {
		return nil, fmt.Errorf("no config file found in %s (tried config.yaml and config.yml)", configDir)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Remove "cors" from Ports map if it exists (it's already in config.CORS)
	delete(config.Ports, "cors")

	if len(config.Ports) == 0 {
		return nil, fmt.Errorf("config file is empty or invalid")
	}

	return &config, nil
}
