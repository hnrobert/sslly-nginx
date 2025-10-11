package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create temporary config directory
	tmpDir := t.TempDir()

	// Test valid config
	configContent := `1234:
  - a.com
  - b.a.com
5678:
  - b.com
`
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if len(cfg.Ports) != 2 {
		t.Errorf("Expected 2 ports, got %d", len(cfg.Ports))
	}

	if domains, ok := cfg.Ports["1234"]; ok {
		if len(domains) != 2 {
			t.Errorf("Expected 2 domains for port 1234, got %d", len(domains))
		}
	} else {
		t.Error("Port 1234 not found in config")
	}
}

func TestLoadConfigNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := Load(tmpDir)
	if err == nil {
		t.Error("Expected error when config file not found")
	}
}

func TestLoadConfigEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := Load(tmpDir)
	if err == nil {
		t.Error("Expected error when config is empty")
	}
}
