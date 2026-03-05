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
		route   string
		wantErr bool
	}{
		{name: "Not static", key: "1234", ok: false},
		{name: "Not static bracket IPv6 upstream", key: "[::1]:9000", ok: false},
		{name: "Dot path", key: "./static", ok: true, dir: "./static"},
		{name: "Abs path", key: "/app/static", ok: true, dir: "/app/static"},
		// Colons are now part of the path, not port separators
		{name: "Path with colon (treated as path)", key: "./static:v2", ok: true, dir: "./static:v2"},
		{name: "Path with number colon (treated as path)", key: "./static:10000", ok: true, dir: "./static:10000"},
		{name: "Bracket dir with route", key: "[/app/static/site1]/home", ok: true, dir: "/app/static/site1", route: "/home"},
		// Bracket syntax: colons are part of the path
		{name: "Bracket dir with colon in path and route", key: "[./static:v2]/home", ok: true, dir: "./static:v2", route: "/home"},
		{name: "Bracket non-dir is not static", key: "[site1]/home", ok: false},
		{name: "Bracket invalid route", key: "[/app/static]home", ok: true, wantErr: true},
		{name: "Empty dir", key: ":10000", ok: false},
		// Protocol prefix is stripped
		{name: "Protocol prefix stripped", key: "<http>./static", ok: true, dir: "./static"},
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
