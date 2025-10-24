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

func TestParseUpstream(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHost string
		wantPort string
	}{
		{
			name:     "Plain port number",
			input:    "1234",
			wantHost: "127.0.0.1",
			wantPort: "1234",
		},
		{
			name:     "IP:port format",
			input:    "192.168.31.6:1234",
			wantHost: "192.168.31.6",
			wantPort: "1234",
		},
		{
			name:     "IP:port with trailing colon (YAML key format)",
			input:    "192.168.31.6:1234:",
			wantHost: "192.168.31.6",
			wantPort: "1234",
		},
		{
			name:     "Plain port with trailing colon",
			input:    "5678:",
			wantHost: "127.0.0.1",
			wantPort: "5678",
		},
		{
			name:     "Localhost with port",
			input:    "localhost:8080",
			wantHost: "localhost",
			wantPort: "8080",
		},
		{
			name:     "IPv6 localhost with brackets",
			input:    "[::1]:9000",
			wantHost: "::1",
			wantPort: "9000",
		},
		{
			name:     "IPv6 address with brackets",
			input:    "[2001:db8::1]:8080",
			wantHost: "2001:db8::1",
			wantPort: "8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := ParseUpstream(tt.input)
			if upstream.Host != tt.wantHost {
				t.Errorf("ParseUpstream(%q).Host = %q, want %q", tt.input, upstream.Host, tt.wantHost)
			}
			if upstream.Port != tt.wantPort {
				t.Errorf("ParseUpstream(%q).Port = %q, want %q", tt.input, upstream.Port, tt.wantPort)
			}
		})
	}
}
