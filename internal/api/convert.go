package api

import (
	"fmt"
	"sort"
	"strings"

	v1 "github.com/hnrobert/sslly-nginx/gen/hnrobert/sslly/v1"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// --- FieldMask helpers ---------------------------------------------------

// maskPaths returns the mask paths; nil/empty mask means "every field".
func maskPaths(m *fieldmaskpb.FieldMask) []string {
	if m == nil {
		return nil
	}
	return m.GetPaths()
}

var corsFieldNames = map[string]struct{}{
	"allow_origin":      {},
	"allow_methods":     {},
	"allow_headers":     {},
	"expose_headers":    {},
	"max_age":           {},
	"allow_credentials": {},
}

func corsPresenceFromMask(paths []string) (config.CORSFieldPresence, error) {
	var p config.CORSFieldPresence
	if len(paths) == 0 {
		return config.CORSFieldPresence{
			AllowOrigin:      true,
			AllowMethods:     true,
			AllowHeaders:     true,
			ExposeHeaders:    true,
			MaxAge:           true,
			AllowCredentials: true,
		}, nil
	}
	for _, path := range paths {
		if _, ok := corsFieldNames[path]; !ok {
			return p, fmt.Errorf("unknown cors field %q", path)
		}
		switch path {
		case "allow_origin":
			p.AllowOrigin = true
		case "allow_methods":
			p.AllowMethods = true
		case "allow_headers":
			p.AllowHeaders = true
		case "expose_headers":
			p.ExposeHeaders = true
		case "max_age":
			p.MaxAge = true
		case "allow_credentials":
			p.AllowCredentials = true
		}
	}
	return p, nil
}

func maskFromCORSPresence(p config.CORSFieldPresence) *fieldmaskpb.FieldMask {
	var paths []string
	if p.AllowOrigin {
		paths = append(paths, "allow_origin")
	}
	if p.AllowMethods {
		paths = append(paths, "allow_methods")
	}
	if p.AllowHeaders {
		paths = append(paths, "allow_headers")
	}
	if p.ExposeHeaders {
		paths = append(paths, "expose_headers")
	}
	if p.MaxAge {
		paths = append(paths, "max_age")
	}
	if p.AllowCredentials {
		paths = append(paths, "allow_credentials")
	}
	if len(paths) == 0 {
		return nil
	}
	return &fieldmaskpb.FieldMask{Paths: paths}
}

func logsPresenceFromMask(paths []string) (config.LogFieldPresence, error) {
	var p config.LogFieldPresence
	if len(paths) == 0 {
		return config.LogFieldPresence{
			SSLLYLevel:      true,
			NginxLevel:      true,
			NginxStderrAs:   true,
			NginxStderrShow: true,
		}, nil
	}
	for _, path := range paths {
		switch path {
		case "sslly.level":
			p.SSLLYLevel = true
		case "nginx.level":
			p.NginxLevel = true
		case "nginx.stderr_as":
			p.NginxStderrAs = true
		case "nginx.stderr_show":
			p.NginxStderrShow = true
		default:
			return p, fmt.Errorf("unknown logs field %q", path)
		}
	}
	return p, nil
}

// --- CORS ----------------------------------------------------------------

func corsRuleFromConfig(key string, c config.CORSConfig) *v1.CMsgCorsRule {
	return &v1.CMsgCorsRule{
		Key:              key,
		AllowOrigin:      c.AllowOrigin,
		AllowMethods:     c.AllowMethods,
		AllowHeaders:     c.AllowHeaders,
		ExposeHeaders:    c.ExposeHeaders,
		MaxAge:           int32(c.MaxAge),
		AllowCredentials: c.AllowCredentials,
		ExplicitFields:   maskFromCORSPresence(c.Presence()),
	}
}

// sortedCorsKeys lists rule keys deterministically.
func sortedCorsKeys(m map[string]config.CORSConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- Logs ----------------------------------------------------------------

func logsFromConfig(lc config.LogConfig) *v1.CMsgLogsConfig {
	return &v1.CMsgLogsConfig{
		SsllyLevel:      lc.SSLLY.Level,
		NginxLevel:      lc.Nginx.Level,
		NginxStderrAs:   lc.Nginx.StderrAs,
		NginxStderrShow: lc.Nginx.StderrShow,
	}
}

func validateLevels(lc *v1.CMsgLogsConfig, mask config.LogFieldPresence) error {
	ok := func(v string, allowed ...string) bool {
		for _, a := range allowed {
			if strings.EqualFold(v, a) {
				return true
			}
		}
		return false
	}
	if mask.SSLLYLevel && !ok(lc.GetSsllyLevel(), "debug", "info", "warn", "error") {
		return fmt.Errorf("sslly_level must be debug|info|warn|error, got %q", lc.GetSsllyLevel())
	}
	if mask.NginxLevel && !ok(lc.GetNginxLevel(), "debug", "info", "warn", "error") {
		return fmt.Errorf("nginx_level must be debug|info|warn|error, got %q", lc.GetNginxLevel())
	}
	if mask.NginxStderrAs && !ok(lc.GetNginxStderrAs(), "warn", "error") {
		return fmt.Errorf("nginx_stderr_as must be warn|error, got %q", lc.GetNginxStderrAs())
	}
	if mask.NginxStderrShow && !ok(lc.GetNginxStderrShow(), "warn", "error") {
		return fmt.Errorf("nginx_stderr_show must be warn|error, got %q", lc.GetNginxStderrShow())
	}
	return nil
}

// --- Users ---------------------------------------------------------------

func userFromConfig(u config.User) *v1.CMsgUser {
	out := &v1.CMsgUser{Name: u.Name}
	for _, p := range u.Permissions {
		out.Permissions = append(out.Permissions, &v1.CMsgPermission{
			Surface:   surfaceToProto(p.Surface),
			Mode:      modeToProto(p.Mode),
			Domains:   p.Domains,
			Upstreams: p.Upstreams,
		})
	}
	return out
}

func userToProtoUser(u *v1.CMsgUser) (config.User, error) {
	if u == nil {
		return config.User{}, fmt.Errorf("user must not be empty")
	}
	if u.GetName() == "" {
		return config.User{}, fmt.Errorf("user name must not be empty")
	}
	out := config.User{Name: u.GetName()}
	for _, p := range u.GetPermissions() {
		if p == nil {
			return config.User{}, fmt.Errorf("permission must not be empty")
		}
		cp := config.Permission{Domains: p.GetDomains(), Upstreams: p.GetUpstreams()}
		var err error
		if cp.Surface, err = surfaceFromProto(p.GetSurface()); err != nil {
			return config.User{}, err
		}
		if cp.Mode, err = modeFromProto(p.GetMode()); err != nil {
			return config.User{}, err
		}
		out.Permissions = append(out.Permissions, cp)
	}
	if len(out.Permissions) == 0 {
		return config.User{}, fmt.Errorf("user %q needs at least one permission", u.GetName())
	}
	return out, nil
}

func surfaceToProto(s string) v1.EPermissionSurface {
	switch s {
	case config.SurfaceProxy:
		return v1.EPermissionSurface_E_PERMISSION_SURFACE_PROXY
	case config.SurfaceCORS:
		return v1.EPermissionSurface_E_PERMISSION_SURFACE_CORS
	case config.SurfaceLogs:
		return v1.EPermissionSurface_E_PERMISSION_SURFACE_LOGS
	case config.SurfaceUsers:
		return v1.EPermissionSurface_E_PERMISSION_SURFACE_USERS
	}
	return v1.EPermissionSurface_E_PERMISSION_SURFACE_UNSPECIFIED
}

func surfaceFromProto(e v1.EPermissionSurface) (string, error) {
	switch e {
	case v1.EPermissionSurface_E_PERMISSION_SURFACE_PROXY:
		return config.SurfaceProxy, nil
	case v1.EPermissionSurface_E_PERMISSION_SURFACE_CORS:
		return config.SurfaceCORS, nil
	case v1.EPermissionSurface_E_PERMISSION_SURFACE_LOGS:
		return config.SurfaceLogs, nil
	case v1.EPermissionSurface_E_PERMISSION_SURFACE_USERS:
		return config.SurfaceUsers, nil
	}
	return "", fmt.Errorf("unknown permission surface %v", e)
}

func modeToProto(m string) v1.EPermissionMode {
	switch m {
	case config.ModeRead:
		return v1.EPermissionMode_E_PERMISSION_MODE_READ
	case config.ModeReadWrite:
		return v1.EPermissionMode_E_PERMISSION_MODE_READ_WRITE
	}
	return v1.EPermissionMode_E_PERMISSION_MODE_UNSPECIFIED
}

func modeFromProto(e v1.EPermissionMode) (string, error) {
	switch e {
	case v1.EPermissionMode_E_PERMISSION_MODE_READ:
		return config.ModeRead, nil
	case v1.EPermissionMode_E_PERMISSION_MODE_READ_WRITE:
		return config.ModeReadWrite, nil
	}
	return "", fmt.Errorf("unknown permission mode %v", e)
}
