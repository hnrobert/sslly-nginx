package nginx

import (
	"strings"
	"testing"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

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
