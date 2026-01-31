package app

import (
	"testing"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

func TestParseStaticSiteKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		ok      bool
		dir     string
		hasPort bool
		port    int
		route   string
		wantErr bool
	}{
		{name: "Not static", key: "1234", ok: false},
		{name: "Not static bracket https upstream", key: "[https]9143", ok: false},
		{name: "Not static bracket IPv6 upstream", key: "[::1]:9000", ok: false},
		{name: "Dot path no port", key: "./static", ok: true, dir: "./static", hasPort: false},
		{name: "Abs path no port", key: "/app/static", ok: true, dir: "/app/static", hasPort: false},
		{name: "Dot path with port", key: "./static:10000", ok: true, dir: "./static", hasPort: true, port: 10000},
		{name: "Invalid port", key: "./static:abc", ok: true, dir: "./static:abc", hasPort: false},
		{name: "Port out of range", key: "./static:70000", ok: true, wantErr: true},
		{name: "Bracket dir with route", key: "[/app/static/site1]/home", ok: true, dir: "/app/static/site1", hasPort: false, route: "/home"},
		{name: "Bracket dir with port and route", key: "[./static:10080]/home", ok: true, dir: "./static", hasPort: true, port: 10080, route: "/home"},
		{name: "Bracket non-dir is not static", key: "[site1]/home", ok: false},
		{name: "Bracket invalid route", key: "[/app/static]home", ok: true, wantErr: true},
		{name: "Empty dir", key: ":10000", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, ok, err := config.ParseStaticSiteKey(tt.key)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v (spec=%+v err=%v)", ok, tt.ok, spec, err)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !tt.ok {
				return
			}
			if spec.Dir != tt.dir {
				t.Fatalf("dir=%q want %q", spec.Dir, tt.dir)
			}
			if spec.HasPort != tt.hasPort {
				t.Fatalf("hasPort=%v want %v", spec.HasPort, tt.hasPort)
			}
			if spec.Port != tt.port {
				t.Fatalf("port=%d want %d", spec.Port, tt.port)
			}
			if spec.RoutePath != tt.route {
				t.Fatalf("route=%q want %q", spec.RoutePath, tt.route)
			}
		})
	}
}

func TestApplyStaticSiteRoute(t *testing.T) {
	out := applyStaticSiteRoute([]string{"example.com", "api.example.com/v1"}, "/home")
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d: %#v", len(out), out)
	}
	if out[0] != "example.com/home" {
		t.Fatalf("unexpected rewritten domain: %q", out[0])
	}
	if out[1] != "api.example.com/v1" {
		t.Fatalf("expected existing path to remain, got %q", out[1])
	}
}
