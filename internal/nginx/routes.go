package nginx

import (
	"fmt"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

// RouteConfig represents a routing configuration for a domain/path combination
type RouteConfig struct {
	Upstream   config.Upstream
	DomainPath string
	BaseDomain string
	Path       string
}

// StaticRouteConfig represents a static site routing configuration
type StaticRouteConfig struct {
	StaticSite config.StaticSiteSpec
	DomainPath string
	BaseDomain string
	Path       string
	HasIndex   bool
}

// splitDomainPath splits domain/path into domain and path parts
func splitDomainPath(domainPath string) (string, string) {
	if idx := strings.Index(domainPath, "/"); idx > 0 {
		return domainPath[:idx], domainPath[idx:]
	}
	return domainPath, ""
}

// formatUpstreamAddr formats upstream address properly for nginx
// IPv6 addresses need to be wrapped in brackets
func formatUpstreamAddr(upstream config.Upstream) string {
	host := upstream.Host

	// Check if host is IPv6 (contains colons but not already bracketed)
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		// It's IPv6, wrap in brackets
		host = "[" + host + "]"
	}

	return fmt.Sprintf("%s:%s", host, upstream.Port)
}

// formatGRPCAddr renders the grpc_pass target (plain h2c to the upstream).
func formatGRPCAddr(upstream config.Upstream) string {
	return "grpc://" + formatUpstreamAddr(upstream)
}

// grpcRouteFor returns the gRPC route of a domain when ALL its proxy routes
// are gRPC and it has no static routes (a gRPC domain serves location / via
// grpc_pass, so nothing else may share the server block). ok=false when the
// domain has no gRPC routes or mixes gRPC with other kinds.
func grpcRouteFor(routes []RouteConfig, staticSiteRoutes []StaticRouteConfig) (RouteConfig, bool) {
	if len(routes) == 0 || len(staticSiteRoutes) > 0 {
		return RouteConfig{}, false
	}
	for _, r := range routes {
		if !r.Upstream.Protocol.IsGRPC() {
			return RouteConfig{}, false
		}
	}
	return routes[0], true
}

// sortRoutesByPathLength sorts routes by path length (longest first) for proper nginx matching
func sortRoutesByPathLength(routes []RouteConfig) {
	for i := 0; i < len(routes)-1; i++ {
		for j := 0; j < len(routes)-i-1; j++ {
			if len(routes[j].Path) < len(routes[j+1].Path) {
				routes[j], routes[j+1] = routes[j+1], routes[j]
			}
		}
	}
}

// sortStaticRoutesByPathLength sorts static routes by path length (longest first)
func sortStaticRoutesByPathLength(routes []StaticRouteConfig) {
	for i := 0; i < len(routes)-1; i++ {
		for j := 0; j < len(routes)-i-1; j++ {
			if len(routes[j].Path) < len(routes[j+1].Path) {
				routes[j], routes[j+1] = routes[j+1], routes[j]
			}
		}
	}
}
