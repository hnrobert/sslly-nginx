package nginx

import (
	"fmt"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

// ALPN) is terminated by nginx when the domain has a certificate.
func generateGRPCLocation(sb *strings.Builder, route RouteConfig) {
	fmt.Fprintf(sb, `        # gRPC reverse proxy (upstream over cleartext h2c)
        location / {
            grpc_pass %s;
            grpc_read_timeout 3600s;
            grpc_send_timeout 3600s;
        }

`, formatGRPCAddr(route.Upstream))
}

// generateStaticSiteLocations generates nginx location blocks for static sites
// Uses root directive for "/" path, alias directive for non-root paths
func generateStaticSiteLocations(sb *strings.Builder, routes []StaticRouteConfig, corsConfig *config.CORSConfig, noTrailingSlash map[string]bool) {
	corsHeaders := generateCORSHeaders(corsConfig)

	// Sort routes by path length (longest first)
	sortStaticRoutesByPathLength(routes)

	for _, route := range routes {
		locationPath := route.Path
		if locationPath == "" {
			locationPath = "/"
		}

		isRootPath := locationPath == "/"

		if isRootPath {
			// Root path: use root directive
			if route.HasIndex {
				// SPA support with try_files
				sb.WriteString(fmt.Sprintf(`        location / {
            root %s;
            index index.html;
            try_files $uri $uri/ /index.html;

%s
        }

`, route.StaticSite.Dir, corsHeaders))
			} else {
				// Simple static file serving
				fmt.Fprintf(sb, `        location / {
            root %s;

%s
        }

`, route.StaticSite.Dir, corsHeaders)
			}
		} else {
			// Non-root path: use alias directive
			// Add redirect for path without trailing slash (unless disabled)
			if !noTrailingSlash[route.DomainPath] {
				sb.WriteString(fmt.Sprintf(`        location = %s {
            return 301 $scheme://$host%s/;
        }

`, locationPath, locationPath))
			}

			// Ensure alias path ends with /
			aliasPath := route.StaticSite.Dir
			if !strings.HasSuffix(aliasPath, "/") {
				aliasPath += "/"
			}

			if route.HasIndex {
				// SPA support with try_files (alias mode uses different path resolution)
				// For alias, the try_files fallback must be a URI (starting with /)
				// which triggers an internal redirect to the named location
				sb.WriteString(fmt.Sprintf(`        location %s/ {
            alias %s;
            index index.html;
            try_files $uri $uri/ %s/index.html;

%s
        }

`, locationPath, aliasPath, locationPath, corsHeaders))
			} else {
				// Simple static file serving
				fmt.Fprintf(sb, `        location %s/ {
            alias %s;

%s
        }

`, locationPath, aliasPath, corsHeaders)
			}
		}
	}
}

// generateProxyLocations generates nginx location blocks for proxy routes
func generateProxyLocations(sb *strings.Builder, routes []RouteConfig, corsConfig *config.CORSConfig, noTrailingSlash map[string]bool) {
	corsHeaders := generateCORSHeaders(corsConfig)

	// Sort routes by path length (longest first)
	sortRoutesByPathLength(routes)

	for _, route := range routes {
		// A gRPC route only fits a gRPC-ONLY domain (it owns location /).
		// Mixed domains drop it with a visible comment instead of emitting a
		// duplicate location / that would fail nginx -t.
		if route.Upstream.Protocol.IsGRPC() {
			fmt.Fprintf(sb, `        # WARNING: gRPC upstream for %s is SKIPPED — a gRPC domain must not mix with HTTP proxy/static routes on the same server name
`, route.DomainPath)
			continue
		}
		upstreamAddr := formatUpstreamAddr(route.Upstream)
		locationPath := route.Path
		if locationPath == "" {
			locationPath = "/"
		}

		proxyPass := fmt.Sprintf("%s://%s", route.Upstream.Scheme, upstreamAddr)
		if route.Upstream.Path != "" {
			proxyPass += route.Upstream.Path
		}

		// For non-root paths, optionally add redirect and use trailing slash
		if locationPath != "/" {
			if !noTrailingSlash[route.DomainPath] {
				sb.WriteString(fmt.Sprintf(`        location = %s {
            return 301 $scheme://$host%s/;
        }

`, locationPath, locationPath))
			}
			locationPath = locationPath + "/"
			if !strings.HasSuffix(proxyPass, "/") {
				proxyPass += "/"
			}
		}

		sb.WriteString(fmt.Sprintf(`        location %s {
            proxy_pass %s;
            proxy_http_version 1.1;

            # Standard proxy headers
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Host $http_host;
            proxy_set_header X-Forwarded-Proto $scheme;

            # WebSocket support
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";

            # Timeouts
            proxy_connect_timeout 60s;
            proxy_send_timeout 60s;
            proxy_read_timeout 60s;

%s
        }

`, locationPath, proxyPass, corsHeaders))
	}
}
