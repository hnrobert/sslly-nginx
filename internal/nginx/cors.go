package nginx

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

// getCORSConfig returns the merged CORS configuration for a given domain.
//
// Matching layers are applied from lowest to highest priority, so a more
// specific layer overrides (or clears) fields set by a more general one, while
// fields it leaves unset are inherited:
//
//  1. "*" catch-all (lowest priority)
//  2. "*.suffix" wildcards, shortest suffix first; "*.suffix" matches subdomains
//     only — "*.example.com" matches "api.example.com" but not "example.com".
//  3. Exact domain key (highest priority)
//
// A field explicitly set to an empty value ("", null, or []) in a higher layer
// clears (omits) that header in the result; a field left unset is inherited.
// This mirrors ssl.FindCertificate's "*.suffix" handling.
func getCORSConfig(cfg *config.Config, domain string) *config.CORSConfig {
	if cfg.CORS == nil {
		return nil
	}
	domain = strings.ToLower(strings.TrimSpace(domain))

	// Collect applicable layers in priority order (lowest first).
	var layers []config.CORSConfig

	// 1. Catch-all.
	if c, ok := cfg.CORS["*"]; ok {
		layers = append(layers, c)
	}

	// 2. Matching "*.suffix" wildcards, shortest suffix first.
	type wildcard struct {
		suffix string
		cfg    config.CORSConfig
	}
	var wildcards []wildcard
	for pat, c := range cfg.CORS {
		if !strings.HasPrefix(pat, "*.") {
			continue
		}
		suffix := pat[1:] // ".example.com"
		if domain != pat[2:] && strings.HasSuffix(domain, suffix) {
			wildcards = append(wildcards, wildcard{suffix, c})
		}
	}
	sort.SliceStable(wildcards, func(i, j int) bool {
		return len(wildcards[i].suffix) < len(wildcards[j].suffix)
	})
	for _, w := range wildcards {
		layers = append(layers, w.cfg)
	}

	// 3. Exact match (highest priority).
	if c, ok := cfg.CORS[domain]; ok {
		layers = append(layers, c)
	}

	if len(layers) == 0 {
		return nil
	}

	// Merge: each layer's explicitly-present fields override the accumulator.
	merged := config.CORSConfig{}
	presence := config.CORSFieldPresence{}
	for _, l := range layers {
		lp := l.Presence()
		if lp.AllowOrigin {
			merged.AllowOrigin = l.AllowOrigin
			presence.AllowOrigin = true
		}
		if lp.AllowMethods {
			merged.AllowMethods = l.AllowMethods
			presence.AllowMethods = true
		}
		if lp.AllowHeaders {
			merged.AllowHeaders = l.AllowHeaders
			presence.AllowHeaders = true
		}
		if lp.ExposeHeaders {
			merged.ExposeHeaders = l.ExposeHeaders
			presence.ExposeHeaders = true
		}
		if lp.MaxAge {
			merged.MaxAge = l.MaxAge
			presence.MaxAge = true
		}
		if lp.AllowCredentials {
			merged.AllowCredentials = l.AllowCredentials
			presence.AllowCredentials = true
		}
	}
	merged.SetPresence(presence)
	return &merged
}

// generateCORSHeaders generates CORS header configuration from CORSConfig.
//
// Access-Control-Allow-Origin is emitted ONLY inside the OPTIONS preflight
// block, never at the location level. Many proxied backends already send this
// header on actual responses; emitting it at the location level would produce a
// duplicate ("Access-Control-Allow-Origin cannot contain more than one origin"
// in the browser). For the preflight the `return 204` short-circuits before
// proxy_pass, so the backend is never reached and exactly one Origin is sent.
//
// The preflight block only matches OPTIONS requests that carry an
// Access-Control-Request-Method header — the signature of a browser CORS
// preflight. Plain OPTIONS requests (e.g. WebDAV capability discovery by
// macOS webdavfs) must fall through to proxy_pass so the backend can answer
// with its own DAV headers; intercepting them breaks WebDAV mounting.
func generateCORSHeaders(corsConfig *config.CORSConfig) string {
	// No cors.yaml entry: use built-in defaults via the resolved path below (a
	// zero-value CORSConfig with no presence falls back to every default).
	if corsConfig == nil {
		corsConfig = &config.CORSConfig{}
	}

	// Resolve each header field. A field that was explicitly set to an empty
	// value ("", null, or []) is cleared (omitted); a field left unset by every
	// matching layer inherits its default.
	p := corsConfig.Presence()

	const (
		defaultMethods = "GET, HEAD, POST, PUT, DELETE, CONNECT, OPTIONS, TRACE, PATCH"
		defaultHeaders = "DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization"
		defaultExpose  = "Content-Length,Content-Range"
	)

	// resolveSlice: present=false → default; present=true+empty → cleared; else join.
	resolveSlice := func(present bool, parts []string, sep, def string) (string, bool) {
		if !present {
			return def, true
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, sep), true
	}
	// resolveStr: present=false → default; present=true+"" → cleared; else value.
	resolveStr := func(present bool, val, def string) (string, bool) {
		if !present {
			return def, true
		}
		if val == "" {
			return "", false
		}
		return val, true
	}

	originVal, emitOrigin := resolveStr(p.AllowOrigin, corsConfig.AllowOrigin, "*")
	methodsVal, emitMethods := resolveSlice(p.AllowMethods, corsConfig.AllowMethods, ", ", defaultMethods)
	headersVal, emitHeaders := resolveSlice(p.AllowHeaders, corsConfig.AllowHeaders, ",", defaultHeaders)
	exposeVal, emitExpose := resolveSlice(p.ExposeHeaders, corsConfig.ExposeHeaders, ",", defaultExpose)

	maxAge := corsConfig.MaxAge
	if !p.MaxAge {
		maxAge = 1728000 // 20 days
	}

	var sb strings.Builder
	sb.WriteString("            # CORS configuration\n")
	// NOTE: Access-Control-Allow-Origin is intentionally NOT emitted at the
	// location level. Many proxied backends already send this header on actual
	// responses; emitting it here would duplicate it and trigger
	// "Access-Control-Allow-Origin cannot contain more than one origin" in the
	// browser. Origin is only added inside the OPTIONS preflight block below,
	// where `return 204` short-circuits before proxy_pass (so the backend is
	// never reached and there is exactly one Origin). Methods/Headers/Expose
	// are still emitted here because backends typically don't set those.
	if emitMethods {
		fmt.Fprintf(&sb, "            add_header 'Access-Control-Allow-Methods' '%s' always;\n", methodsVal)
	}
	if emitHeaders {
		fmt.Fprintf(&sb, "            add_header 'Access-Control-Allow-Headers' '%s' always;\n", headersVal)
	}
	if emitExpose {
		fmt.Fprintf(&sb, "            add_header 'Access-Control-Expose-Headers' '%s' always;\n", exposeVal)
	}

	if corsConfig.AllowCredentials {
		sb.WriteString("            add_header 'Access-Control-Allow-Credentials' 'true' always;\n")
	}

	sb.WriteString("\n            # Handle CORS preflight requests only: an OPTIONS request carrying\n")
	sb.WriteString("            # an Access-Control-Request-Method header (browser preflight).\n")
	sb.WriteString("            # Plain OPTIONS (e.g. WebDAV capability discovery) is NOT matched\n")
	sb.WriteString("            # here and falls through to proxy_pass so the backend can answer\n")
	sb.WriteString("            # with its own DAV/Allow headers. nginx has no nested if, so the\n")
	sb.WriteString("            # AND of both conditions is built by string concatenation.\n")
	sb.WriteString("            set $cors_preflight '';\n")
	sb.WriteString("            if ($request_method = 'OPTIONS') {\n")
	sb.WriteString("                set $cors_preflight 'M';\n")
	sb.WriteString("            }\n")
	sb.WriteString("            if ($http_access_control_request_method != '') {\n")
	sb.WriteString("                set $cors_preflight '${cors_preflight}A';\n")
	sb.WriteString("            }\n")
	sb.WriteString("            if ($cors_preflight = 'MA') {\n")
	if emitOrigin {
		fmt.Fprintf(&sb, "                add_header 'Access-Control-Allow-Origin' '%s' always;\n", originVal)
	}
	if emitMethods {
		fmt.Fprintf(&sb, "                add_header 'Access-Control-Allow-Methods' '%s' always;\n", methodsVal)
	}
	if emitHeaders {
		fmt.Fprintf(&sb, "                add_header 'Access-Control-Allow-Headers' '%s' always;\n", headersVal)
	}
	if corsConfig.AllowCredentials {
		sb.WriteString("                add_header 'Access-Control-Allow-Credentials' 'true' always;\n")
	}
	fmt.Fprintf(&sb, "                add_header 'Access-Control-Max-Age' %d always;\n", maxAge)
	sb.WriteString("                add_header 'Content-Type' 'text/plain; charset=utf-8';\n")
	sb.WriteString("                add_header 'Content-Length' 0;\n")
	sb.WriteString("                return 204;\n")
	sb.WriteString("            }")

	return sb.String()
}
