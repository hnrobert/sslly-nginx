package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	// Create temporary config directory
	tmpDir := t.TempDir()

	// Test valid config
	configContent := `1234:
  - a.com
  - b.a.com
5678:
  - b.com
`
	configPath := filepath.Join(tmpDir, "proxy.yaml")
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
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	tmpDir := t.TempDir()

	_, err := Load(tmpDir)
	if err == nil {
		t.Error("Expected error when config file not found")
	}
}

func TestLoadConfigEmpty(t *testing.T) {
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, "proxy.yaml")
	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := Load(tmpDir)
	if err == nil {
		t.Error("Expected error when config is empty")
	}
}

func TestParseUpstream(t *testing.T) {
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

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
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	tmpDir := t.TempDir()

	corsContent := "\"*\":\n" +
		"  allow_origin: \"*\"\n" +
		"  allow_methods:\n" +
		"    - GET\n" +
		"    - POST\n" +
		"    - OPTIONS\n" +
		"  allow_headers:\n" +
		"    - Content-Type\n" +
		"    - Authorization\n" +
		"  expose_headers:\n" +
		"    - Content-Length\n" +
		"  max_age: 3600\n" +
		"  allow_credentials: false\n"
	proxyContent := "1234:\n  - example.com\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "cors.yaml"), []byte(corsContent), 0644); err != nil {
		t.Fatalf("Failed to write test cors config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "proxy.yaml"), []byte(proxyContent), 0644); err != nil {
		t.Fatalf("Failed to write test proxy config: %v", err)
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

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestPrepare_NewUser_CreatesSplitFilesFromExample(t *testing.T) {
	exampleDir := t.TempDir()
	t.Setenv("SSLLY_EXAMPLE_DIR", exampleDir)

	tmpDir := t.TempDir()

	// New user: no proxy.yaml yet; only example exists in the example directory.
	writeFile(t, filepath.Join(exampleDir, proxyExampleFile), "1234:\n  - example.com\n")

	if err := Prepare(tmpDir); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// proxy.yaml should exist and be loadable.
	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Ports) != 1 {
		t.Fatalf("expected 1 port mapping, got %d", len(cfg.Ports))
	}
	if got := cfg.Ports["1234"]; len(got) != 1 || got[0] != "example.com" {
		t.Fatalf("unexpected ports mapping: %#v", cfg.Ports)
	}

	// Split optional files should be present after Prepare.
	for _, f := range []string{proxyConfigFile, corsConfigFile, logsConfigFile} {
		if _, err := os.Stat(filepath.Join(tmpDir, f)); err != nil {
			t.Fatalf("expected %s to exist: %v", f, err)
		}
	}
}

func TestPrepare_LegacyUser_MigratesAndBacksUp(t *testing.T) {
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	tmpDir := t.TempDir()

	legacy := `log:
  sslly:
    level: debug
  nginx:
    level: warn
    stderr_as: error
    stderr_show: warn

cors:
  "*":
    allow_origin: "*"
    allow_methods: [GET, POST]
    allow_headers: [Content-Type]
    expose_headers: [Content-Length]
    max_age: 10
    allow_credentials: false

1234:
  - a.com
  - b.com
`
	legacyPath := filepath.Join(tmpDir, legacyConfigYAML)
	writeFile(t, legacyPath, legacy)

	if err := Prepare(tmpDir); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Legacy file should have been renamed.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("expected legacy file to be renamed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "config.backup.yaml")); err != nil {
		t.Fatalf("expected backup file to exist: %v", err)
	}

	// Proxy should contain only port mappings (no top-level log/cors).
	proxyBytes, err := os.ReadFile(filepath.Join(tmpDir, proxyConfigFile))
	if err != nil {
		t.Fatalf("read proxy: %v", err)
	}
	var proxyRoot map[string]any
	if err := yaml.Unmarshal(proxyBytes, &proxyRoot); err != nil {
		t.Fatalf("unmarshal proxy: %v", err)
	}
	if _, ok := proxyRoot["log"]; ok {
		t.Fatalf("proxy.yaml should not contain 'log' key")
	}
	if _, ok := proxyRoot["cors"]; ok {
		t.Fatalf("proxy.yaml should not contain 'cors' key")
	}
	if _, ok := proxyRoot["1234"]; !ok {
		t.Fatalf("proxy.yaml should contain migrated port mapping")
	}

	// Logs should contain inner content (no outer 'log:').
	logsBytes, err := os.ReadFile(filepath.Join(tmpDir, logsConfigFile))
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	var logsRoot map[string]any
	if err := yaml.Unmarshal(logsBytes, &logsRoot); err != nil {
		t.Fatalf("unmarshal logs: %v", err)
	}
	if _, ok := logsRoot["sslly"]; !ok {
		t.Fatalf("logs.yaml should contain 'sslly' key")
	}
	if _, ok := logsRoot["nginx"]; !ok {
		t.Fatalf("logs.yaml should contain 'nginx' key")
	}

	// CORS should contain inner content (no outer 'cors:').
	corsBytes, err := os.ReadFile(filepath.Join(tmpDir, corsConfigFile))
	if err != nil {
		t.Fatalf("read cors: %v", err)
	}
	var corsRoot map[string]any
	if err := yaml.Unmarshal(corsBytes, &corsRoot); err != nil {
		t.Fatalf("unmarshal cors: %v", err)
	}
	if _, ok := corsRoot["*"]; !ok {
		t.Fatalf("cors.yaml should contain wildcard config")
	}
}

func TestPrepare_LegacyUser_PreservesComments(t *testing.T) {
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	tmpDir := t.TempDir()

	legacy := `# global header comment
log: # log key inline comment
  # sslly comment
  sslly:
    level: debug # sslly level inline
  nginx:
    level: warn

# cors section header
cors:
  "*":
    allow_origin: "*" # cors inline

# port section header
1234: # port inline
  - a.com # domain inline
`
	writeFile(t, filepath.Join(tmpDir, legacyConfigYAML), legacy)

	if err := Prepare(tmpDir); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	proxyBytes, err := os.ReadFile(filepath.Join(tmpDir, proxyConfigFile))
	if err != nil {
		t.Fatalf("read proxy: %v", err)
	}
	logsBytes, err := os.ReadFile(filepath.Join(tmpDir, logsConfigFile))
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	corsBytes, err := os.ReadFile(filepath.Join(tmpDir, corsConfigFile))
	if err != nil {
		t.Fatalf("read cors: %v", err)
	}

	proxyText := string(proxyBytes)
	logsText := string(logsBytes)
	corsText := string(corsBytes)

	// Ensure we didn't lose comments while splitting.
	if !strings.Contains(proxyText, "global header comment") &&
		!strings.Contains(logsText, "global header comment") &&
		!strings.Contains(corsText, "global header comment") {
		t.Fatalf("expected global header comment to be preserved in at least one split file")
	}
	if !strings.Contains(logsText, "log key inline comment") {
		t.Fatalf("expected log key inline comment to be preserved in logs.yaml, got:\n%s", logsText)
	}
	if !strings.Contains(logsText, "sslly level inline") {
		t.Fatalf("expected sslly inline comment to be preserved in logs.yaml, got:\n%s", logsText)
	}
	if !strings.Contains(corsText, "cors section header") {
		t.Fatalf("expected cors section header comment to be preserved in cors.yaml, got:\n%s", corsText)
	}
	if !strings.Contains(corsText, "cors inline") {
		t.Fatalf("expected cors inline comment to be preserved in cors.yaml, got:\n%s", corsText)
	}
	if !strings.Contains(proxyText, "port section header") {
		t.Fatalf("expected port section header comment to be preserved in proxy.yaml, got:\n%s", proxyText)
	}
	if !strings.Contains(proxyText, "domain inline") {
		t.Fatalf("expected domain inline comment to be preserved in proxy.yaml, got:\n%s", proxyText)
	}
}

func TestPrepare_LegacyUser_WhenBackupExists_UsesTimestampSuffix(t *testing.T) {
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	tmpDir := t.TempDir()

	writeFile(t, filepath.Join(tmpDir, legacyConfigYAML), "1234:\n  - a.com\n")
	// Pre-existing backup should force timestamped name.
	writeFile(t, filepath.Join(tmpDir, "config.backup.yaml"), "already-here")

	if err := Prepare(tmpDir); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// We should now have at least one additional backup besides the pre-existing one.
	matches, err := filepath.Glob(filepath.Join(tmpDir, "config.backup.*.yaml"))
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 timestamped backup, got %d: %v", len(matches), matches)
	}
}

func TestPrepare_LegacyUser_DoesNotOverwriteExistingSplitFiles(t *testing.T) {
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	tmpDir := t.TempDir()

	// Existing split proxy.yaml should be kept.
	existingProxy := "1234:\n  - keep.me\n"
	writeFile(t, filepath.Join(tmpDir, proxyConfigFile), existingProxy)

	// Legacy config has different mapping; it should be backed up but NOT overwrite proxy.yaml.
	writeFile(t, filepath.Join(tmpDir, legacyConfigYAML), "5678:\n  - should.not.win\n")

	if err := Prepare(tmpDir); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(tmpDir, proxyConfigFile))
	if err != nil {
		t.Fatalf("read proxy: %v", err)
	}
	if string(got) != existingProxy {
		t.Fatalf("proxy.yaml was overwritten; got=%q want=%q", string(got), existingProxy)
	}

	// Legacy should still be renamed.
	if _, err := os.Stat(filepath.Join(tmpDir, "config.backup.yaml")); err != nil {
		t.Fatalf("expected backup file to exist: %v", err)
	}
}

func TestLoad_LoadsLogsConfig(t *testing.T) {
	t.Setenv("SSLLY_EXAMPLE_DIR", t.TempDir())

	tmpDir := t.TempDir()

	writeFile(t, filepath.Join(tmpDir, proxyConfigFile), "1234:\n  - example.com\n")
	writeFile(t, filepath.Join(tmpDir, logsConfigFile), "sslly:\n  level: debug\nnginx:\n  level: info\n  stderr_as: error\n  stderr_show: warn\n")

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Log.SSLLY.Level != "debug" {
		t.Fatalf("expected sslly log level debug, got %q", cfg.Log.SSLLY.Level)
	}
	if cfg.Log.Nginx.Level != "info" {
		t.Fatalf("expected nginx log level info, got %q", cfg.Log.Nginx.Level)
	}
	if cfg.Log.Nginx.StderrAs != "error" {
		t.Fatalf("expected nginx stderr_as error, got %q", cfg.Log.Nginx.StderrAs)
	}
	if cfg.Log.Nginx.StderrShow != "warn" {
		t.Fatalf("expected nginx stderr_show warn, got %q", cfg.Log.Nginx.StderrShow)
	}
}
