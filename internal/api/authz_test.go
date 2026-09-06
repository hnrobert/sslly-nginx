package api

import (
	"testing"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func perm(surface, mode string, domains, upstreams []string) config.Permission {
	return config.Permission{Surface: surface, Mode: mode, Domains: domains, Upstreams: upstreams}
}

func TestAuthorizeSemantics(t *testing.T) {
	denyByDefault := &config.User{Name: "a", Permissions: nil}

	rwAll := &config.User{Name: "rw", Permissions: []config.Permission{
		perm(config.SurfaceProxy, config.ModeReadWrite, nil, nil),
	}}

	readOnly := &config.User{Name: "ro", Permissions: []config.Permission{
		perm(config.SurfaceProxy, config.ModeRead, nil, nil),
	}}

	domainScoped := &config.User{Name: "dom", Permissions: []config.Permission{
		perm(config.SurfaceCORS, config.ModeReadWrite, []string{"*.ibuduan.com"}, nil),
	}}

	domainScopedRWProxy := &config.User{Name: "mix", Permissions: []config.Permission{
		perm(config.SurfaceProxy, config.ModeReadWrite, []string{"*.ibuduan.com"}, nil),
		perm(config.SurfaceProxy, config.ModeReadWrite, nil, []string{"8080"}),
	}}

	upstreamScoped := &config.User{Name: "up", Permissions: []config.Permission{
		perm(config.SurfaceProxy, config.ModeReadWrite, nil, []string{"8080", "192.168.50.2:1234"}),
	}}

	starDomain := &config.User{Name: "star", Permissions: []config.Permission{
		perm(config.SurfaceCORS, config.ModeReadWrite, []string{"*"}, nil),
	}}

	res := func(domain, upstream string) Resource {
		return Resource{Domain: domain, UpstreamKey: upstream}
	}

	cases := []struct {
		name      string
		user      *config.User
		surface   string
		mode      string
		resources []Resource
		wantCode  codes.Code // OK to expect success
	}{
		{"deny by default", denyByDefault, config.SurfaceProxy, config.ModeRead, nil, codes.PermissionDenied},
		{"nil user", nil, config.SurfaceProxy, config.ModeRead, nil, codes.Unauthenticated},
		{"rw all proxy", rwAll, config.SurfaceProxy, config.ModeReadWrite,
			[]Resource{res("a.example.com", "1234"), res("b.example.com", "9099")}, codes.OK},
		{"read granted by rw rule", rwAll, config.SurfaceProxy, config.ModeRead, nil, codes.OK},
		{"read-only cannot write", readOnly, config.SurfaceProxy, config.ModeReadWrite, nil, codes.PermissionDenied},
		{"read-only can read", readOnly, config.SurfaceProxy, config.ModeRead, nil, codes.OK},
		{"surface isolation", readOnly, config.SurfaceCORS, config.ModeRead, nil, codes.PermissionDenied},
		{"domain wildcard matches subdomain", domainScoped, config.SurfaceCORS, config.ModeReadWrite,
			[]Resource{res("api.cpu.ibuduan.com", "")}, codes.OK},
		{"domain wildcard excludes bare apex", domainScoped, config.SurfaceCORS, config.ModeReadWrite,
			[]Resource{res("ibuduan.com", "")}, codes.PermissionDenied},
		{"domain wildcard excludes others", domainScoped, config.SurfaceCORS, config.ModeReadWrite,
			[]Resource{res("example.com", "")}, codes.PermissionDenied},
		{"cors catch-all key needs unrestricted selector", domainScoped, config.SurfaceCORS, config.ModeReadWrite,
			[]Resource{res("*", "")}, codes.PermissionDenied},
		{"star selector covers catch-all key", starDomain, config.SurfaceCORS, config.ModeReadWrite,
			[]Resource{res("*", "")}, codes.OK},
		{"upstream selector exact match", upstreamScoped, config.SurfaceProxy, config.ModeReadWrite,
			[]Resource{res("x.example.com", "8080")}, codes.OK},
		{"upstream selector verbatim only", upstreamScoped, config.SurfaceProxy, config.ModeReadWrite,
			[]Resource{res("x.example.com", "8081")}, codes.PermissionDenied},
		{"upstream selector exact string form", upstreamScoped, config.SurfaceProxy, config.ModeReadWrite,
			[]Resource{res("x.example.com", "192.168.50.2:1234")}, codes.OK},
		// Rules do not union: one domain-scoped + one port-scoped rule must not
		// combine to allow a request outside either single scope.
		{"no rule union", domainScopedRWProxy, config.SurfaceProxy, config.ModeReadWrite,
			[]Resource{res("a.ibuduan.com", "8080"), res("x.example.com", "9090")}, codes.PermissionDenied},
		{"single rule covers all", domainScopedRWProxy, config.SurfaceProxy, config.ModeReadWrite,
			[]Resource{res("a.ibuduan.com", "9090"), res("b.ibuduan.com", "9091")}, codes.OK},
		{"domain-restricted rule fails domain-less resource", domainScopedRWProxy, config.SurfaceProxy, config.ModeReadWrite,
			[]Resource{res("", "9122")}, codes.PermissionDenied},
		{"unrestricted rule covers domain-less resource", rwAll, config.SurfaceProxy, config.ModeReadWrite,
			[]Resource{res("", "9122")}, codes.OK},
		{"empty resources pass for scoped surface", domainScoped, config.SurfaceCORS, config.ModeReadWrite, nil, codes.OK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Authorize(tc.user, tc.surface, tc.mode, tc.resources)
			if status.Code(err) != tc.wantCode {
				t.Fatalf("Authorize code = %v (%v), want %v", status.Code(err), err, tc.wantCode)
			}
		})
	}
}

func TestDomainOfListener(t *testing.T) {
	cases := map[string]string{
		"example.com":           "example.com",
		"example.com/api":       "example.com",
		"example.com|8443":      "example.com",
		"<https>example.com":    "example.com",
		"<http>example.com/api": "example.com",
		"EXAMPLE.com":           "example.com",
		"8122":                  "8122",
		"192.168.50.1|22":       "192.168.50.1",
	}
	for in, want := range cases {
		if got := domainOfListener(in); got != want {
			t.Errorf("domainOfListener(%q) = %q, want %q", in, got, want)
		}
	}
}
