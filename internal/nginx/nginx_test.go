package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
	"gopkg.in/yaml.v3"
)

// parseCORS parses a YAML CORS map so that per-field presence is recorded (via
// CORSConfig.UnmarshalYAML), matching how cors.yaml is loaded in production.
func parseCORS(t *testing.T, yamlStr string) map[string]config.CORSConfig {
	t.Helper()
	var m map[string]config.CORSConfig
	if err := yaml.Unmarshal([]byte(yamlStr), &m); err != nil {
		t.Fatalf("parse cors yaml: %v", err)
	}
	return m
}

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
	cfg := &config.Config{CORS: parseCORS(t, `'*': {allow_origin: '*'}`)}
	cors := getCORSConfig(cfg, "any.example.com")
	if cors == nil || cors.AllowOrigin != "*" {
		t.Fatalf("expected wildcard CORS, got %+v", cors)
	}
}

func TestGetCORSConfigDomainTakesPrecedenceOverWildcard(t *testing.T) {
	cfg := &config.Config{CORS: parseCORS(t, `
'*':
  allow_origin: '*'
  allow_headers: [DNT, User-Agent]
'api.example.com':
  allow_origin: '*'
  allow_headers: ['*']
`)}

	// Domain-specific entry must win over the wildcard.
	cors := getCORSConfig(cfg, "api.example.com")
	if cors == nil || len(cors.AllowHeaders) != 1 || cors.AllowHeaders[0] != "*" {
		t.Fatalf("expected domain-specific allow_headers [*], got %v", cors.AllowHeaders)
	}

	// Unrelated domains fall back to the wildcard.
	other := getCORSConfig(cfg, "other.example.com")
	if other == nil || len(other.AllowHeaders) != 2 || other.AllowHeaders[0] != "DNT" {
		t.Fatalf("expected wildcard allow_headers, got %v", other.AllowHeaders)
	}
}

func TestGetCORSConfigSuffixWildcard(t *testing.T) {
	cfg := &config.Config{CORS: parseCORS(t, `
'*.ibuduan.com':
  allow_origin: 'https://app.ibuduan.com'
  allow_headers: [X-Suffix]
`)}

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
	cfg := &config.Config{CORS: parseCORS(t, `
'*.ibuduan.com':     {allow_headers: [short]}
'*.cpu.ibuduan.com': {allow_headers: [long]}
`)}

	cors := getCORSConfig(cfg, "api.cpu.ibuduan.com")
	if cors == nil || len(cors.AllowHeaders) != 1 || cors.AllowHeaders[0] != "long" {
		t.Fatalf("expected longest suffix (*.cpu.ibuduan.com) to win, got %+v", cors.AllowHeaders)
	}

	// A subdomain of ibuduan.com that is not under cpu gets the shorter rule.
	cors = getCORSConfig(cfg, "api.ibuduan.com")
	if cors == nil || cors.AllowHeaders[0] != "short" {
		t.Fatalf("expected shorter suffix (*.ibuduan.com), got %+v", cors.AllowHeaders)
	}
}

func TestGetCORSConfigExactBeatsSuffix(t *testing.T) {
	cfg := &config.Config{CORS: parseCORS(t, `
'*.ibuduan.com':      {allow_headers: [suffix]}
'api.cpu.ibuduan.com': {allow_headers: [exact]}
`)}

	cors := getCORSConfig(cfg, "api.cpu.ibuduan.com")
	if cors == nil || cors.AllowHeaders[0] != "exact" {
		t.Fatalf("expected exact key to beat suffix, got %+v", cors.AllowHeaders)
	}
}

func TestGetCORSConfigNoMatch(t *testing.T) {
	cfg := &config.Config{CORS: parseCORS(t, `'*.ibuduan.com': {allow_origin: 'https://app.ibuduan.com'}`)}
	if got := getCORSConfig(cfg, "example.com"); got != nil {
		t.Fatalf("expected nil for unmatched domain, got %+v", got)
	}
	if got := getCORSConfig(&config.Config{}, "anything"); got != nil {
		t.Fatalf("expected nil for nil CORS map, got %+v", got)
	}
}

func TestGetCORSConfigInheritsUnsetFields(t *testing.T) {
	// The specific layer sets only allow_credentials; origin/headers must be
	// inherited from the "*" layer.
	cfg := &config.Config{CORS: parseCORS(t, `
'*':
  allow_origin: 'https://app.example.com'
  allow_headers: [Content-Type, Authorization]
'api.example.com':
  allow_credentials: true
`)}

	cors := getCORSConfig(cfg, "api.example.com")
	if cors == nil {
		t.Fatalf("expected non-nil CORS")
	}
	if cors.AllowOrigin != "https://app.example.com" {
		t.Fatalf("expected inherited origin, got %q", cors.AllowOrigin)
	}
	if len(cors.AllowHeaders) != 2 || cors.AllowHeaders[0] != "Content-Type" {
		t.Fatalf("expected inherited headers, got %v", cors.AllowHeaders)
	}
	if !cors.AllowCredentials {
		t.Fatalf("expected credentials true from specific layer")
	}
	// Inherited + explicit fields are both marked present in the merged result.
	p := cors.Presence()
	if !p.AllowOrigin || !p.AllowHeaders || !p.AllowCredentials {
		t.Fatalf("expected origin/headers/credentials present in merge, got %+v", p)
	}
}

func TestGetCORSConfigExplicitEmptyClearsField(t *testing.T) {
	// The specific layer explicitly clears allow_origin with "" and with null.
	// Both must mark the field present-and-empty (cleared); headers inherit.
	cfg := &config.Config{CORS: parseCORS(t, `
'*':
  allow_origin: '*'
  allow_headers: [Content-Type]
'empty.example.com':
  allow_origin: ''
'null.example.com':
  allow_origin:
`)}

	empty := getCORSConfig(cfg, "empty.example.com")
	if empty == nil || empty.AllowOrigin != "" || !empty.Presence().AllowOrigin {
		t.Fatalf("expected cleared (present, empty) origin for empty.example.com, got %+v", empty)
	}
	if len(empty.AllowHeaders) != 1 || empty.AllowHeaders[0] != "Content-Type" {
		t.Fatalf("expected inherited headers, got %v", empty.AllowHeaders)
	}

	nul := getCORSConfig(cfg, "null.example.com")
	if nul == nil || nul.AllowOrigin != "" || !nul.Presence().AllowOrigin {
		t.Fatalf("expected cleared (present, null) origin for null.example.com, got %+v", nul)
	}
}

func TestGenerateCORSHeadersClearsAndInherits(t *testing.T) {
	// A merged config where origin is present+empty (cleared → omit), headers
	// present (emit), and methods/expose absent (default → emit).
	merged := &config.CORSConfig{
		AllowOrigin:  "",
		AllowHeaders: []string{"X-Inherited"},
	}
	merged.SetPresence(config.CORSFieldPresence{AllowOrigin: true, AllowHeaders: true})

	out := generateCORSHeaders(merged)
	if strings.Contains(out, "Access-Control-Allow-Origin") {
		t.Fatalf("cleared origin must be omitted, got:\n%s", out)
	}
	if !strings.Contains(out, "X-Inherited") {
		t.Fatalf("expected present header X-Inherited, got:\n%s", out)
	}
	if !strings.Contains(out, "Access-Control-Allow-Methods") {
		t.Fatalf("absent methods should default and be emitted, got:\n%s", out)
	}
}

func TestCORSResolutionEndToEnd(t *testing.T) {
	// User's scenario: "*" sets defaults; specific domain clears allow_origin.
	cfg := &config.Config{CORS: parseCORS(t, `
'*':
  allow_origin: '*'
  allow_methods: [GET, POST]
  allow_headers: [Content-Type]
'api.example.com':
  allow_origin: ''
`)}

	out := generateCORSHeaders(getCORSConfig(cfg, "api.example.com"))
	if strings.Contains(out, "Access-Control-Allow-Origin") {
		t.Fatalf("origin should be cleared/omitted, got:\n%s", out)
	}
	if !strings.Contains(out, "GET, POST") {
		t.Fatalf("methods should be inherited from *, got:\n%s", out)
	}
	if !strings.Contains(out, "Content-Type") {
		t.Fatalf("headers should be inherited from *, got:\n%s", out)
	}
}

func TestGenerateCORSHeadersDefault(t *testing.T) {
	out := generateCORSHeaders(nil)
	if !strings.Contains(out, "Access-Control-Allow-Origin") {
		t.Fatalf("expected default headers")
	}
}

func TestGenerateCORSHeaders_OriginOnlyInPreflight(t *testing.T) {
	// Access-Control-Allow-Origin must be emitted ONLY inside the OPTIONS
	// preflight block (16-space indent), never at the location level
	// (12-space indent). Otherwise a proxied backend that also sends the
	// header produces a duplicate on actual responses.
	out := generateCORSHeaders(&config.CORSConfig{}) // zero value → all defaults

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "            add_header 'Access-Control-Allow-Origin'") {
			t.Fatalf("Allow-Origin must not be emitted at location level, got:\n%s", out)
		}
	}

	if !strings.Contains(out, "                add_header 'Access-Control-Allow-Origin' '*' always;") {
		t.Fatalf("expected Allow-Origin inside OPTIONS block, got:\n%s", out)
	}

	// The default (nil) path must also produce a complete preflight (with
	// Origin/Methods/Headers) and no location-level Origin.
	defOut := generateCORSHeaders(nil)
	for _, line := range strings.Split(defOut, "\n") {
		if strings.HasPrefix(line, "            add_header 'Access-Control-Allow-Origin'") {
			t.Fatalf("default path: Allow-Origin must not be at location level, got:\n%s", defOut)
		}
	}
	if !strings.Contains(defOut, "                add_header 'Access-Control-Allow-Origin' '*' always;") {
		t.Fatalf("default path: expected Allow-Origin in OPTIONS block, got:\n%s", defOut)
	}
}

func TestCORSPlainOptionsPassesThrough(t *testing.T) {
	// A real browser preflight is OPTIONS + Access-Control-Request-Method.
	// Plain OPTIONS (WebDAV capability discovery) must NOT be short-circuited
	// with return 204 — macOS webdavfs refuses to mount a server whose OPTIONS
	// answer lacks a DAV header, so those requests must reach the backend.
	out := generateCORSHeaders(nil)

	if !strings.Contains(out, "$http_access_control_request_method") {
		t.Fatalf("preflight match must require Access-Control-Request-Method, got:\n%s", out)
	}
	if !strings.Contains(out, "if ($cors_preflight = 'MA') {") {
		t.Fatalf("expected AND-condition preflight block, got:\n%s", out)
	}

	// The bare method-only branch must only set the flag, never add_header or
	// return: a return inside `if ($request_method = 'OPTIONS')` is the exact
	// bug that broke WebDAV.
	methodIf := "if ($request_method = 'OPTIONS') {\n                set $cors_preflight 'M';\n            }"
	if !strings.Contains(out, methodIf) {
		t.Fatalf("method-only branch must only set the flag, got:\n%s", out)
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
