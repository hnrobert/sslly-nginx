package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

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

	// Capture top-level proxy.yaml key order so nginx server blocks can be
	// emitted deterministically in declaration order.
	if keys, err := orderedTopLevelKeys(proxyData); err == nil {
		config.OrderedPorts = keys
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
	delete(config.Ports, "no_trailing_slash")

	// Keep only keys that remain valid ports (drops cors/log/no_trailing_slash
	// and any key not present in Ports), preserving file order.
	var kept []string
	for _, k := range config.OrderedPorts {
		if _, ok := config.Ports[k]; ok {
			kept = append(kept, k)
		}
	}
	config.OrderedPorts = kept

	if len(config.Ports) == 0 {
		return nil, fmt.Errorf("config is empty or invalid (%s has no proxy mappings)", proxyConfigFile)
	}

	return &config, nil
}

func orderedTopLevelKeys(data []byte) ([]string, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	mapping := node.Content[0]
	var keys []string
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Kind == yaml.ScalarNode {
			keys = append(keys, mapping.Content[i].Value)
		}
	}
	return keys, nil
}
