package api

import (
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Resource is what a request touches: an optional base domain (lowercased)
// and/or a proxy.yaml upstream key. Empty string means "not applicable" and
// fails closed against a rule restricted on that dimension.
type Resource struct {
	Domain      string
	UpstreamKey string
}

// domainOfListener extracts the base domain from a listener key:
// "<https>example.com/api" -> "example.com", "api.example.com|8443" ->
// "api.example.com", "8122" (stream target) -> "8122".
func domainOfListener(key string) string {
	k := key
	if strings.HasPrefix(k, "<") {
		if i := strings.Index(k, ">"); i > 0 {
			k = k[i+1:]
		}
	}
	if i := strings.Index(k, "/"); i >= 0 {
		k = k[:i]
	}
	if i := strings.Index(k, "|"); i >= 0 {
		k = k[:i]
	}
	return strings.ToLower(strings.TrimSpace(k))
}

// entryResources builds the resource set of an upstream entry: the upstream
// key paired with each listener's domain.
func entryResources(upstreamKey string, listenerKeys []string) []Resource {
	res := make([]Resource, 0, len(listenerKeys))
	for _, lk := range listenerKeys {
		res = append(res, Resource{Domain: domainOfListener(lk), UpstreamKey: upstreamKey})
	}
	if len(res) == 0 {
		res = append(res, Resource{UpstreamKey: upstreamKey})
	}
	return res
}

// listenerResources builds domain-only resources for a list of listener keys
// (used by no_trailing_slash).
func listenerResources(listenerKeys []string) []Resource {
	res := make([]Resource, 0, len(listenerKeys))
	for _, lk := range listenerKeys {
		res = append(res, Resource{Domain: domainOfListener(lk)})
	}
	return res
}

// Authorize reports whether a single permission rule of the user grants
// `mode` on `surface` covering ALL of `resources` (deny-by-default; rules do
// not union — one rule must cover the whole request, which is what keeps the
// semantics explainable).
//
// Coverage per resource, both dimensions must pass:
//   - upstream: rule.Upstreams empty, contains "*", or contains the exact
//     upstream key verbatim;
//   - domain: rule.Domains empty, contains "*", an exact (case-insensitive)
//     match, or a "*.suffix" wildcard matching subdomains only (identical to
//     the CORS key semantics in nginx.getCORSConfig: "*.example.com" matches
//     api.example.com but NOT the bare example.com).
//
// A resource with an empty dimension never matches a rule restricted on that
// dimension (e.g. a domain-restricted rule cannot manage domain-less stream
// targets; the cors catch-all key "*" is only covered by an unrestricted or
// "*" domain selector).
func Authorize(u *config.User, surface, mode string, resources []Resource) error {
	if u == nil {
		return status.Error(codes.Unauthenticated, "not authenticated")
	}
	for i := range u.Permissions {
		p := &u.Permissions[i]
		if p.Surface != surface || !modeSatisfied(p.Mode, mode) {
			continue
		}
		if allCovered(p, resources) {
			return nil
		}
	}
	return status.Errorf(codes.PermissionDenied,
		"user %q lacks %s access to %s for this request", u.Name, mode, surface)
}

// CanSee is Authorize for read filtering: true when the caller may see the
// given resources.
func CanSee(u *config.User, surface string, resources []Resource) bool {
	return Authorize(u, surface, config.ModeRead, resources) == nil
}

func modeSatisfied(ruleMode, want string) bool {
	if want == config.ModeReadWrite {
		return ruleMode == config.ModeReadWrite
	}
	return ruleMode == config.ModeRead || ruleMode == config.ModeReadWrite
}

func allCovered(p *config.Permission, resources []Resource) bool {
	for _, r := range resources {
		if !upstreamCovered(p.Upstreams, r.UpstreamKey) || !domainCovered(p.Domains, r.Domain) {
			return false
		}
	}
	return true
}

func upstreamCovered(selectors []string, key string) bool {
	if len(selectors) == 0 || contains(selectors, "*") {
		return true
	}
	return contains(selectors, key)
}

func domainCovered(patterns []string, domain string) bool {
	if len(patterns) == 0 || contains(patterns, "*") {
		return true
	}
	for _, pat := range patterns {
		if pat == "*" {
			return true
		}
		if strings.HasPrefix(pat, "*.") {
			// Subdomains only, not the bare apex — mirrors getCORSConfig.
			suffix := pat[1:] // ".example.com"
			if domain != pat[2:] && strings.HasSuffix(domain, suffix) {
				return true
			}
			continue
		}
		if strings.EqualFold(pat, domain) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// hasUsersAdmin reports whether the user holds users-surface read-write.
func hasUsersAdmin(u config.User) bool {
	for _, p := range u.Permissions {
		if p.Surface == config.SurfaceUsers && p.Mode == config.ModeReadWrite {
			return true
		}
	}
	return false
}
