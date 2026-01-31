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
		wantErr bool
	}{
		{name: "Not static", key: "1234", ok: false},
		{name: "Dot path no port", key: "./static", ok: true, dir: "./static", hasPort: false},
		{name: "Abs path no port", key: "/app/static", ok: true, dir: "/app/static", hasPort: false},
		{name: "Dot path with port", key: "./static:10000", ok: true, dir: "./static", hasPort: true, port: 10000},
		{name: "Invalid port", key: "./static:abc", ok: true, dir: "./static:abc", hasPort: false},
		{name: "Port out of range", key: "./static:70000", ok: true, wantErr: true},
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
		})
	}
}
