package nginx

import (
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
