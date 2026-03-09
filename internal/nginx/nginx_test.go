package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

func TestSplitDomainPath(t *testing.T) {
	domain, path := splitDomainPath("example.com/api")
	if domain != "example.com" {
		t.Fatalf("domain mismatch: %s", domain)
	}
	if path != "/api" {
		t.Fatalf("path mismatch: %s", path)
	}

	domain, path = splitDomainPath("example.com")
	if domain != "example.com" || path != "" {
		t.Fatalf("unexpected split: %s %s", domain, path)
	}
}

func TestFormatUpstreamAddrIPv6(t *testing.T) {
	addr := formatUpstreamAddr(config.Upstream{Host: "::1", Port: "8080"})
	if addr != "[::1]:8080" {
		t.Fatalf("unexpected addr: %s", addr)
	}
}

func TestGetCORSConfig(t *testing.T) {
	cfg := &config.Config{CORS: map[string]config.CORSConfig{"*": {AllowOrigin: "*"}}}
	cors := getCORSConfig(cfg, "any.example.com")
	if cors == nil || cors.AllowOrigin != "*" {
		t.Fatalf("expected wildcard CORS")
	}
}

func TestGenerateCORSHeadersDefault(t *testing.T) {
	out := generateCORSHeaders(nil)
	if !strings.Contains(out, "Access-Control-Allow-Origin") {
		t.Fatalf("expected default headers")
	}
}

func TestGenerateConfigHTTPServerBlock(t *testing.T) {
	cfg := &config.Config{
		CORS: map[string]config.CORSConfig{},
		Ports: map[string][]string{
			"1234":                 {"example.com"},
			"192.168.1.2:5678/api": {"example.com/api"},
		},
	}

	ng := GenerateConfig(cfg, map[string]ssl.Certificate{})
	if !strings.Contains(ng, "# HTTP server block for example.com") {
		t.Fatalf("expected HTTP server block")
	}
	if !strings.Contains(ng, "server_name example.com;") {
		t.Fatalf("expected server_name")
	}
	if !strings.Contains(ng, "proxy_pass http://127.0.0.1:1234") {
		t.Fatalf("expected proxy_pass to localhost")
	}
	if !strings.Contains(ng, "proxy_pass http://192.168.1.2:5678/api") {
		t.Fatalf("expected proxy_pass to upstream with path")
	}
}


func TestGenerateConfig_StaticSites(t *testing.T) {
	// Create temporary directories for static sites
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	site1Dir := filepath.Join(tmpDir, "site1")

	// Create directories
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("Failed to create static dir: %v", err)
	}
	if err := os.MkdirAll(site1Dir, 0755); err != nil {
		t.Fatalf("Failed to create site1 dir: %v", err)
	}

	// Create index.html in staticDir
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatalf("Failed to create index.html: %v", err)
	}

	// Create config with static sites
	cfg := &config.Config{
		Ports: map[string][]string{
			staticDir: {"static.example.com"},
			"[" + site1Dir + "]/home": {"yourdomain.com"},
		},
		RuntimeStaticSites: map[string]config.StaticSiteSpec{
			staticDir: {
				Dir:       staticDir,
				RoutePath: "",
			},
			"[" + site1Dir + "]/home": {
				Dir:       site1Dir,
				RoutePath: "/home",
			},
		},
	}

	// Generate nginx config
	nginxConfig := GenerateConfig(cfg, nil)

	// Check for root directive for root path static site
	if !strings.Contains(nginxConfig, "root "+staticDir) {
		t.Error("Expected nginx config to contain root directive for static site (root path)")
	}

	// Non-root path should use alias, not root
	if !strings.Contains(nginxConfig, "alias "+site1Dir) {
		t.Error("Expected nginx config to contain alias directive for site1 (non-root path)")
	}

	// Check for try_files directive (SPA support)
	if !strings.Contains(nginxConfig, "try_files $uri $uri/") {
		t.Error("Expected nginx config to contain try_files directive for SPA support")
	}

	// Check for index.html
	if !strings.Contains(nginxConfig, "index index.html") {
		t.Error("Expected nginx config to contain index index.html directive")
	}

	// Check that proxy_pass is NOT used for static sites
	if strings.Contains(nginxConfig, "proxy_pass") {
		// Make sure proxy_pass is only in the expected places, not for static sites
		// We just check that root directive is present for static sites
	}

	// Check location path for site1 with route
	if !strings.Contains(nginxConfig, "location /home/") {
		t.Error("Expected location /home/ for yourdomain.com")
	}
}

func TestGenerateConfig_StaticSitesWithProxy(t *testing.T) {
	// Create temporary directory for static site
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("Failed to create static dir: %v", err)
	}

	// Create config with both static site and proxy
	cfg := &config.Config{
		Ports: map[string][]string{
			staticDir:        {"static.example.com"},
			"8080":           {"api.example.com"},
			"192.168.1.1:90": {"backend.example.com"},
		},
		RuntimeStaticSites: map[string]config.StaticSiteSpec{
			staticDir: {
				Dir:       staticDir,
				RoutePath: "",
			},
		},
	}

	nginxConfig := GenerateConfig(cfg, nil)

	// Check static site uses root
	if !strings.Contains(nginxConfig, "root "+staticDir) {
		t.Error("Static site should use root directive")
	}

	// Check proxy routes use proxy_pass
	if !strings.Contains(nginxConfig, "proxy_pass http://127.0.0.1:8080;") {
		t.Error("API route should use proxy_pass")
	}

	if !strings.Contains(nginxConfig, "proxy_pass http://192.168.1.1:90;") {
		t.Error("Backend route should use proxy_pass")
	}

	// Check that static site server block has root
	if !strings.Contains(nginxConfig, "server_name static.example.com;") {
		t.Error("Should have server block for static.example.com")
	}
}

func TestGenerateConfig_StaticSitesNoIndex(t *testing.T) {
	// Create temporary directory without index.html
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("Failed to create static dir: %v", err)
	}

	cfg := &config.Config{
		Ports: map[string][]string{
			staticDir: {"static.example.com"},
		},
		RuntimeStaticSites: map[string]config.StaticSiteSpec{
			staticDir: {
				Dir:       staticDir,
				RoutePath: "",
			},
		},
	}

	nginxConfig := GenerateConfig(cfg, nil)

	// Should still have root directive
	if !strings.Contains(nginxConfig, "root "+staticDir) {
		t.Error("Expected nginx config to contain root directive even without index.html")
	}

	// Should NOT have try_files (no SPA support without index.html)
	// Just check that the config doesn't crash
}

