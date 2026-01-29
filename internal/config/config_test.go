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
		name       string
		input      string
		wantScheme string
		wantHost   string
		wantPort   string
		wantPath   string
	}{
		{
			name:       "Plain port number",
			input:      "1234",
			wantScheme: "http",
			wantHost:   "127.0.0.1",
			wantPort:   "1234",
			wantPath:   "",
		},
		{
			name:       "IP:port format",
			input:      "192.168.31.6:1234",
			wantScheme: "http",
			wantHost:   "192.168.31.6",
			wantPort:   "1234",
			wantPath:   "",
		},
		{
			name:       "IP:port with trailing colon (YAML key format)",
			input:      "192.168.31.6:1234:",
			wantScheme: "http",
			wantHost:   "192.168.31.6",
			wantPort:   "1234",
			wantPath:   "",
		},
		{
			name:       "Plain port with trailing colon",
			input:      "5678:",
			wantScheme: "http",
			wantHost:   "127.0.0.1",
			wantPort:   "5678",
			wantPath:   "",
		},
		{
			name:       "Localhost with port",
			input:      "localhost:8080",
			wantScheme: "http",
			wantHost:   "localhost",
			wantPort:   "8080",
			wantPath:   "",
		},
		{
			name:       "IPv6 localhost with brackets",
			input:      "[::1]:9000",
			wantScheme: "http",
			wantHost:   "::1",
			wantPort:   "9000",
			wantPath:   "",
		},
		{
			name:       "IPv6 address with brackets",
			input:      "[2001:db8::1]:8080",
			wantScheme: "http",
			wantHost:   "2001:db8::1",
			wantPort:   "8080",
			wantPath:   "",
		},
		{
			name:       "Hostname with port",
			input:      "example-server.local:8080",
			wantScheme: "http",
			wantHost:   "example-server.local",
			wantPort:   "8080",
			wantPath:   "",
		},
		{
			name:       "IP:port with path",
			input:      "192.168.50.2:5678/api",
			wantScheme: "http",
			wantHost:   "192.168.50.2",
			wantPort:   "5678",
			wantPath:   "/api",
		},
		{
			name:       "Plain port with path",
			input:      "9012/admin",
			wantScheme: "http",
			wantHost:   "127.0.0.1",
			wantPort:   "9012",
			wantPath:   "/admin",
		},
		{
			name:       "IPv6 with path",
			input:      "[2001:db8::1]:3000/api/v1",
			wantScheme: "http",
			wantHost:   "2001:db8::1",
			wantPort:   "3000",
			wantPath:   "/api/v1",
		},
		{
			name:       "HTTPS scheme with IP:port",
			input:      "[https]192.168.50.2:8443",
			wantScheme: "https",
			wantHost:   "192.168.50.2",
			wantPort:   "8443",
			wantPath:   "",
		},
		{
			name:       "HTTPS scheme with plain port",
			input:      "[https]8443",
			wantScheme: "https",
			wantHost:   "127.0.0.1",
			wantPort:   "8443",
			wantPath:   "",
		},
		{
			name:       "HTTPS scheme with hostname",
			input:      "[https]backend.local:8443",
			wantScheme: "https",
			wantHost:   "backend.local",
			wantPort:   "8443",
			wantPath:   "",
		},
		{
			name:       "HTTPS scheme with path",
			input:      "[https]192.168.50.2:8443/api",
			wantScheme: "https",
			wantHost:   "192.168.50.2",
			wantPort:   "8443",
			wantPath:   "/api",
		},
		{
			name:       "HTTPS scheme with IPv6",
			input:      "[https][2001:db8::1]:8443",
			wantScheme: "https",
			wantHost:   "2001:db8::1",
			wantPort:   "8443",
			wantPath:   "",
		},
		{
			name:       "Domain name without port (http)",
			input:      "www.example.com",
			wantScheme: "http",
			wantHost:   "www.example.com",
			wantPort:   "80",
			wantPath:   "",
		},
		{
			name:       "Domain name without port (https)",
			input:      "[https]www.baidu.com",
			wantScheme: "https",
			wantHost:   "www.baidu.com",
			wantPort:   "443",
			wantPath:   "",
		},
		{
			name:       "Domain name with path (https)",
			input:      "[https]api.example.com/v1",
			wantScheme: "https",
			wantHost:   "api.example.com",
			wantPort:   "443",
			wantPath:   "/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := ParseUpstream(tt.input)
			if upstream.Scheme != tt.wantScheme {
				t.Errorf("ParseUpstream(%q).Scheme = %q, want %q", tt.input, upstream.Scheme, tt.wantScheme)
			}
			if upstream.Host != tt.wantHost {
				t.Errorf("ParseUpstream(%q).Host = %q, want %q", tt.input, upstream.Host, tt.wantHost)
			}
			if upstream.Port != tt.wantPort {
				t.Errorf("ParseUpstream(%q).Port = %q, want %q", tt.input, upstream.Port, tt.wantPort)
			}
			if upstream.Path != tt.wantPath {
				t.Errorf("ParseUpstream(%q).Path = %q, want %q", tt.input, upstream.Path, tt.wantPath)
			}
		})
	}
}

func TestLoadConfigWithCORS(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `cors:
  "*":
    allow_origin: "*"
    allow_methods:
      - GET
      - POST
      - OPTIONS
    allow_headers:
      - Content-Type
      - Authorization
    expose_headers:
      - Content-Length
    max_age: 3600
    allow_credentials: false

1234:
  - example.com
`
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Check CORS config loaded
	if len(cfg.CORS) != 1 {
		t.Errorf("Expected 1 CORS config, got %d", len(cfg.CORS))
	}

	if corsConfig, ok := cfg.CORS["*"]; ok {
		if len(corsConfig.AllowMethods) != 3 {
			t.Errorf("Expected 3 CORS methods, got %d", len(corsConfig.AllowMethods))
		}
		if corsConfig.AllowOrigin != "*" {
			t.Errorf("Expected allow_origin '*', got %s", corsConfig.AllowOrigin)
		}
		if len(corsConfig.AllowHeaders) != 2 {
			t.Errorf("Expected 2 allow headers, got %d", len(corsConfig.AllowHeaders))
		}
		if len(corsConfig.ExposeHeaders) != 1 {
			t.Errorf("Expected 1 expose header, got %d", len(corsConfig.ExposeHeaders))
		}
		if corsConfig.MaxAge != 3600 {
			t.Errorf("Expected max_age 3600, got %d", corsConfig.MaxAge)
		}
		if corsConfig.AllowCredentials != false {
			t.Errorf("Expected allow_credentials false, got %v", corsConfig.AllowCredentials)
		}
	} else {
		t.Error("Wildcard CORS config not found")
	}

	// Ensure "cors" is not in Ports map
	if _, exists := cfg.Ports["cors"]; exists {
		t.Error("'cors' should not appear in Ports map")
	}

	// Check regular port mapping still works
	if len(cfg.Ports) != 1 {
		t.Errorf("Expected 1 port mapping, got %d", len(cfg.Ports))
	}
}
