package config

import (
	"testing"
)

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
