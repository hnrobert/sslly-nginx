package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_NoTrailingSlash(t *testing.T) {
	tmpDir := t.TempDir()

	proxyYAML := `8080:
  - example.com/api
  - example.com/admin

no_trailing_slash:
  - example.com/api
`
	if err := os.WriteFile(filepath.Join(tmpDir, "proxy.yaml"), []byte(proxyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.NoTrailingSlash) != 1 || cfg.NoTrailingSlash[0] != "example.com/api" {
		t.Errorf("unexpected NoTrailingSlash: %v", cfg.NoTrailingSlash)
	}

	if _, ok := cfg.Ports["no_trailing_slash"]; ok {
		t.Error("no_trailing_slash should not appear in Ports map")
	}
}

func TestLoad_OrderedPorts(t *testing.T) {
	tmpDir := t.TempDir()

	// Intentionally non-sorted: 9090, 8080, 7070, with special keys interspersed.
	proxyYAML := `9090:
  - d.example.com

cors:
  api.example.com:
    allow_origin: "*"

8080:
  - b.example.com
  - a.example.com

log:
  format: text

7070:
  - c.example.com

no_trailing_slash:
  - b.example.com
`
	if err := os.WriteFile(filepath.Join(tmpDir, "proxy.yaml"), []byte(proxyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	want := []string{"9090", "8080", "7070"}
	if len(cfg.OrderedPorts) != len(want) {
		t.Fatalf("OrderedPorts = %v, want %v", cfg.OrderedPorts, want)
	}
	for i, k := range want {
		if cfg.OrderedPorts[i] != k {
			t.Fatalf("OrderedPorts = %v, want %v at index %d", cfg.OrderedPorts, want, i)
		}
	}

	// Special keys must not survive into OrderedPorts (nor Ports).
	for _, special := range []string{"cors", "log", "no_trailing_slash"} {
		if _, ok := cfg.Ports[special]; ok {
			t.Errorf("%q should not be in Ports", special)
		}
		for _, k := range cfg.OrderedPorts {
			if k == special {
				t.Errorf("%q should not be in OrderedPorts", special)
			}
		}
	}
}

func TestParseStaticSiteKey(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantDir   string
		wantRoute string
		wantOK    bool
		wantErr   bool
	}{
		{
			name:      "Simple directory path",
			input:     "/app/static",
			wantDir:   "/app/static",
			wantRoute: "",
			wantOK:    true,
			wantErr:   false,
		},
		{
			name:      "Directory with colon in path (treated as path)",
			input:     "/app/static:v2",
			wantDir:   "/app/static:v2",
			wantRoute: "",
			wantOK:    true,
			wantErr:   false,
		},
		{
			name:      "Non-static key (plain port)",
			input:     "1234",
			wantDir:   "",
			wantRoute: "",
			wantOK:    false,
			wantErr:   false,
		},
		{
			name:      "Non-static key (ip:port)",
			input:     "192.168.50.1:22",
			wantDir:   "",
			wantRoute: "",
			wantOK:    false,
			wantErr:   false,
		},
		{
			name:      "Non-static key (relative path)",
			input:     "./static",
			wantDir:   "",
			wantRoute: "",
			wantOK:    false,
			wantErr:   false,
		},
		{
			name:      "Double slash separator for route",
			input:     "/app/static//docs",
			wantDir:   "/app/static",
			wantRoute: "/docs",
			wantOK:    true,
			wantErr:   false,
		},
		{
			name:      "Protocol prefix stripped",
			input:     "<http>/app/static",
			wantDir:   "/app/static",
			wantRoute: "",
			wantOK:    true,
			wantErr:   false,
		},
		{
			name:      "Bracket syntax is NOT supported",
			input:     "[/app/static]/home",
			wantDir:   "",
			wantRoute: "",
			wantOK:    false,
			wantErr:   false,
		},
		{
			name:      "Bracket with colon NOT supported",
			input:     "[/app/static:v2]/docs",
			wantDir:   "",
			wantRoute: "",
			wantOK:    false,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, ok, err := ParseStaticSiteKey(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStaticSiteKey(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if ok != tt.wantOK {
				t.Errorf("ParseStaticSiteKey(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				return
			}
			if ok {
				if spec.Dir != tt.wantDir {
					t.Errorf("ParseStaticSiteKey(%q).Dir = %q, want %q", tt.input, spec.Dir, tt.wantDir)
				}
				if spec.RoutePath != tt.wantRoute {
					t.Errorf("ParseStaticSiteKey(%q).RoutePath = %q, want %q", tt.input, spec.RoutePath, tt.wantRoute)
				}
			}
		})
	}
}

func TestParseUpstreamGRPC(t *testing.T) {
	cases := []struct {
		key      string
		host     string
		port     string
		protocol Protocol
	}{
		{"<grpc>9081", "127.0.0.1", "9081", ProtocolGRPC},
		{"<grpc>192.168.50.2:50051", "192.168.50.2", "50051", ProtocolGRPC},
		{"<grpc>grpc.example.com", "grpc.example.com", "50051", ProtocolGRPC}, // conventional default
	}
	for _, tc := range cases {
		u := ParseUpstream(tc.key)
		if u.Protocol != tc.protocol || u.Host != tc.host || u.Port != tc.port {
			t.Errorf("ParseUpstream(%q) = %+v, want host=%s port=%s proto=%s", tc.key, u, tc.host, tc.port, tc.protocol)
		}
	}
	// The prefix parses through the generic protocol branch; grpc upstreams
	// must not be mistaken for stream (L4) mappings.
	if ParseUpstream("<grpc>9081").Protocol.IsStream() {
		t.Fatal("grpc must not count as a stream protocol")
	}
}

func TestValidateMappingGRPCRules(t *testing.T) {
	// Path-based listener on a gRPC upstream is rejected.
	_, errs, _ := ValidateMapping("<grpc>9081", "example.com/api", false)
	if len(errs) == 0 {
		t.Fatal("path listener must be rejected for gRPC upstreams")
	}
	// Path suffix on the upstream itself is rejected too.
	_, errs, _ = ValidateMapping("<grpc>9081/x", "example.com", false)
	if len(errs) == 0 {
		t.Fatal("upstream path suffix must be rejected for gRPC upstreams")
	}
	// Explicit stream listener is rejected.
	_, errs, _ = ValidateMapping("<grpc>9081", "<tcp>example.com", false)
	if len(errs) == 0 {
		t.Fatal("tcp listener must be rejected for gRPC upstreams")
	}
	// Bare domain smart mode: no certificate -> HTTP (h2c), with -> HTTPS.
	lc, errs, _ := ValidateMapping("<grpc>9081", "example.com", false)
	if len(errs) != 0 || lc.Protocol != ProtocolHTTP {
		t.Fatalf("smart mode without cert = %+v errs=%v, want http", lc, errs)
	}
	lc, errs, _ = ValidateMapping("<grpc>9081", "example.com", true)
	if len(errs) != 0 || lc.Protocol != ProtocolHTTPS {
		t.Fatalf("smart mode with cert = %+v errs=%v, want https", lc, errs)
	}
}
