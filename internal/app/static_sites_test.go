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
        {name: "Absolute path", key: "/app/static", ok: true, dir: "/app/static"},
        {name: "Path with colon (treated as path)", key: "/app/static:v2", ok: true, dir: "/app/static:v2"},
        {name: "Path with number colon (treated as path)", key: "/app/static:10000", ok: true, dir: "/app/static:10000"},
        {name: "Double slash separator for route", key: "/app/static//docs", ok: true, dir: "/app/static", route: "/docs"},
        {name: "Double slash empty route", key: "/app/static//", ok: true, dir: "/app/static", route: ""},
        {name: "Protocol prefix stripped", key: "<http>/app/static", ok: true, dir: "/app/static"},
        {name: "Relative path not supported", key: "./static", ok: false},
        {name: "Bracket syntax not supported", key: "[/app/static]/home", ok: false},
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
                t.Errorf("ParseStaticSiteKey(%q).Dir = %q, want %q", tt.key, spec.Dir, tt.dir)
            }
            if spec.RoutePath != tt.route {
                t.Errorf("ParseStaticSiteKey(%q).RoutePath = %q, want %q", tt.key, spec.RoutePath, tt.route)
            }
        })
    }
}
