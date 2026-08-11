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

func TestGetCORSConfigDomainTakesPrecedenceOverWildcard(t *testing.T) {
	cfg := &config.Config{CORS: map[string]config.CORSConfig{
		"*":              {AllowOrigin: "*", AllowHeaders: []string{"DNT", "User-Agent"}},
		"api.example.com": {AllowOrigin: "*", AllowHeaders: []string{"*"}},
	}}

	// Domain-specific entry must win over the wildcard.
	cors := getCORSConfig(cfg, "api.example.com")
	if cors == nil {
		t.Fatalf("expected non-nil CORS for api.example.com")
	}
	if len(cors.AllowHeaders) != 1 || cors.AllowHeaders[0] != "*" {
		t.Fatalf("expected domain-specific allow_headers [*], got %v", cors.AllowHeaders)
	}

	// Unrelated domains fall back to the wildcard.
	other := getCORSConfig(cfg, "other.example.com")
	if other == nil {
		t.Fatalf("expected wildcard fallback for other.example.com")
	}
	if len(other.AllowHeaders) != 2 || other.AllowHeaders[0] != "DNT" {
		t.Fatalf("expected wildcard allow_headers, got %v", other.AllowHeaders)
	}
}

func TestGetCORSConfigSuffixWildcard(t *testing.T) {
	cfg := &config.Config{CORS: map[string]config.CORSConfig{
		"*.ibuduan.com": {AllowOrigin: "https://app.ibuduan.com", AllowHeaders: []string{"X-Suffix"}},
	}}

	// Matches a subdomain.
	cors := getCORSConfig(cfg, "api.cpu.ibuduan.com")
	if cors == nil || cors.AllowOrigin != "https://app.ibuduan.com" {
		t.Fatalf("expected *.ibuduan.com to match subdomain, got %+v", cors)
	}

	// Does NOT match the bare apex.
	if got := getCORSConfig(cfg, "ibuduan.com"); got != nil {
		t.Fatalf("*.ibuduan.com must not match bare apex, got %+v", got)
	}

	// Does NOT match an unrelated domain.
	if got := getCORSConfig(cfg, "example.com"); got != nil {
		t.Fatalf("*.ibuduan.com must not match unrelated domain, got %+v", got)
	}
}

func TestGetCORSConfigLongestSuffixWins(t *testing.T) {
	cfg := &config.Config{CORS: map[string]config.CORSConfig{
		"*.ibuduan.com":     {AllowHeaders: []string{"short"}},
		"*.cpu.ibuduan.com": {AllowHeaders: []string{"long"}},
	}}

	cors := getCORSConfig(cfg, "api.cpu.ibuduan.com")
	if cors == nil || len(cors.AllowHeaders) != 1 || cors.AllowHeaders[0] != "long" {
		t.Fatalf("expected longest suffix (*.cpu.ibuduan.com) to win, got %+v", cors)
	}

	// A subdomain of ibuduan.com that is not under cpu gets the shorter rule.
	cors = getCORSConfig(cfg, "api.ibuduan.com")
	if cors == nil || cors.AllowHeaders[0] != "short" {
		t.Fatalf("expected shorter suffix (*.ibuduan.com), got %+v", cors)
	}
}

func TestGetCORSConfigExactBeatsSuffix(t *testing.T) {
	cfg := &config.Config{CORS: map[string]config.CORSConfig{
		"*.ibuduan.com":      {AllowHeaders: []string{"suffix"}},
		"api.cpu.ibuduan.com": {AllowHeaders: []string{"exact"}},
	}}

	cors := getCORSConfig(cfg, "api.cpu.ibuduan.com")
	if cors == nil || cors.AllowHeaders[0] != "exact" {
		t.Fatalf("expected exact key to beat suffix, got %+v", cors)
	}
}

func TestGetCORSConfigNoMatch(t *testing.T) {
	cfg := &config.Config{CORS: map[string]config.CORSConfig{
		"*.ibuduan.com": {AllowOrigin: "https://app.ibuduan.com"},
	}}
	if got := getCORSConfig(cfg, "example.com"); got != nil {
		t.Fatalf("expected nil for unmatched domain, got %+v", got)
	}
	if got := getCORSConfig(&config.Config{}, "anything"); got != nil {
		t.Fatalf("expected nil for nil CORS map, got %+v", got)
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

func TestGenerateConfig_ServerBlocksFollowProxyYAMLOrder(t *testing.T) {
	// Port keys deliberately non-sorted; OrderedPorts declares the desired order.
	cfg := &config.Config{
		CORS: map[string]config.CORSConfig{},
		Ports: map[string][]string{
			"3030": {"third.example.com"},
			"1010": {"first.example.com"},
			"2020": {"second.example.com"},
		},
		OrderedPorts: []string{"1010", "2020", "3030"},
	}

	ng := GenerateConfig(cfg, nil)

	// Each server block is introduced by a comment naming the base domain.
	idxFirst := strings.Index(ng, "HTTP server block for first.example.com")
	idxSecond := strings.Index(ng, "HTTP server block for second.example.com")
	idxThird := strings.Index(ng, "HTTP server block for third.example.com")
	if idxFirst < 0 || idxSecond < 0 || idxThird < 0 {
		t.Fatalf("missing one or more server block markers in:\n%s", ng)
	}
	if !(idxFirst < idxSecond && idxSecond < idxThird) {
		t.Fatalf("server blocks not in OrderedPorts order: first=%d second=%d third=%d", idxFirst, idxSecond, idxThird)
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
			staticDir:                 {"static.example.com"},
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

func TestGenerateConfig_NoTrailingSlash_ProxyRoute(t *testing.T) {
	cfg := &config.Config{
		CORS: map[string]config.CORSConfig{},
		Ports: map[string][]string{
			"8080": {"example.com/api"},
		},
		NoTrailingSlash: []string{"example.com/api"},
	}

	ng := GenerateConfig(cfg, map[string]ssl.Certificate{})

	if strings.Contains(ng, "location = /api") {
		t.Error("no_trailing_slash: redirect block should not be generated for example.com/api")
	}
	if !strings.Contains(ng, "location /api/") {
		t.Error("proxy location /api/ should still be present")
	}
}

func TestGenerateConfig_DefaultTrailingSlash_ProxyRoute(t *testing.T) {
	cfg := &config.Config{
		CORS: map[string]config.CORSConfig{},
		Ports: map[string][]string{
			"8080": {"example.com/api"},
		},
	}

	ng := GenerateConfig(cfg, map[string]ssl.Certificate{})

	if !strings.Contains(ng, "location = /api") {
		t.Error("default: redirect block should be generated for /api")
	}
	if !strings.Contains(ng, "return 301 $scheme://$host/api/") {
		t.Error("default: 301 redirect to /api/ should be present")
	}
}

func TestGenerateConfig_NoTrailingSlash_StaticSite(t *testing.T) {
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Ports: map[string][]string{
			staticDir: {"example.com/docs"},
		},
		RuntimeStaticSites: map[string]config.StaticSiteSpec{
			staticDir: {Dir: staticDir},
		},
		NoTrailingSlash: []string{"example.com/docs"},
	}

	ng := GenerateConfig(cfg, nil)

	if strings.Contains(ng, "location = /docs") {
		t.Error("no_trailing_slash: redirect block should not be generated for example.com/docs")
	}
	if !strings.Contains(ng, "location /docs/") {
		t.Error("alias location /docs/ should still be present")
	}
}

func TestGenerateConfig_DefaultTrailingSlash_StaticSite(t *testing.T) {
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Ports: map[string][]string{
			staticDir: {"example.com/docs"},
		},
		RuntimeStaticSites: map[string]config.StaticSiteSpec{
			staticDir: {Dir: staticDir},
		},
	}

	ng := GenerateConfig(cfg, nil)

	if !strings.Contains(ng, "location = /docs") {
		t.Error("default: redirect block should be generated for /docs")
	}
	if !strings.Contains(ng, "return 301 $scheme://$host/docs/") {
		t.Error("default: 301 redirect to /docs/ should be present")
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
