package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Upstream represents a backend server configuration
type Upstream struct {
	Host string // IP address or hostname (default: 127.0.0.1)
	Port string // Port number
}

type Config struct {
	Ports map[string][]string `yaml:",inline"`
}

// ParseUpstream parses the key format which can be:
// - "1234" -> Upstream{Host: "127.0.0.1", Port: "1234"}
// - "192.168.31.6:1234" -> Upstream{Host: "192.168.31.6", Port: "1234"}
// - "[::1]:9000" -> Upstream{Host: "::1", Port: "9000"} (IPv6 format)
func ParseUpstream(key string) Upstream {
	// Remove trailing colon if present (for YAML keys like "192.168.31.6:1234:")
	key = strings.TrimSuffix(key, ":")
	
	// Handle IPv6 format [host]:port
	if strings.HasPrefix(key, "[") {
		closeBracket := strings.Index(key, "]")
		if closeBracket > 0 && closeBracket < len(key)-1 && key[closeBracket+1] == ':' {
			return Upstream{
				Host: key[1:closeBracket],
				Port: key[closeBracket+2:],
			}
		}
	}
	
	// Check if key contains a colon (IP:port format)
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
					Host: "127.0.0.1",
					Port: key,
				}
			}
			// Single colon at start (:8080) - treat as plain port
			if host == "" {
				return Upstream{
					Host: "127.0.0.1",
					Port: port,
				}
			}
		}
		
		// Valid host:port format
		return Upstream{
			Host: host,
			Port: port,
		}
	}
	
	// Plain port format (default to localhost)
	return Upstream{
		Host: "127.0.0.1",
		Port: key,
	}
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

	if len(config.Ports) == 0 {
		return nil, fmt.Errorf("config file is empty or invalid")
	}

	return &config, nil
}
