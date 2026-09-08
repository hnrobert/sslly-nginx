package nginx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

func GenerateConfig(cfg *config.Config, certMap map[string]ssl.Certificate) string {
	// Build a set of paths that should not have trailing slash redirects.
	noTrailingSlash := make(map[string]bool, len(cfg.NoTrailingSlash))
	for _, p := range cfg.NoTrailingSlash {
		noTrailingSlash[p] = true
	}

	var sb strings.Builder

	// Read ports from environment with sensible defaults
	// New variable names take precedence, fallback to legacy names for backward compatibility
	httpPort := "80"
	httpsPort := "443"
	if p := os.Getenv("SSLLY_DEFAULT_HTTP_LISTEN_PORT"); p != "" {
		httpPort = p
	} else if p := os.Getenv("SSL_NGINX_HTTP_PORT"); p != "" {
		httpPort = p
	}
	if p := os.Getenv("SSLLY_DEFAULT_HTTPS_LISTEN_PORT"); p != "" {
		httpsPort = p
	} else if p := os.Getenv("SSL_NGINX_HTTPS_PORT"); p != "" {
		httpsPort = p
	}

	// Determine nginx error_log level based on configuration
	errorLogLevel := "error" // Default
	if cfg.Log.Nginx.StderrAs != "" {
		errorLogLevel = cfg.Log.Nginx.StderrAs
	}

	// Separate HTTP/HTTPS mappings from TCP/UDP mappings and static sites
	var streamMappings []StreamMapping
	httpPorts := make(map[string]bool)

	// First pass: identify stream mappings, HTTP ports, and static sites
	for portKey, domainPaths := range cfg.Ports {
		// Check if it's a static site key - they use HTTP/HTTPS ports
		if config.IsStaticSiteKey(portKey) {
			httpPorts[httpPort] = true
			httpPorts[httpsPort] = true
			continue
		}

		listenConfig := config.ParseListenKey(portKey)

		if listenConfig.Protocol.IsStream() {
			// TCP/UDP mapping - get the upstream target from the first domain path
			if len(domainPaths) > 0 {
				upstream := config.ParseUpstream(domainPaths[0])
				streamMappings = append(streamMappings, StreamMapping{
					ListenConfig: listenConfig,
					Upstream:     upstream,
				})
			}
		} else {
			// HTTP/HTTPS mapping
			upstream := config.ParseUpstream(portKey)
			httpPorts[upstream.Port] = true
		}
	}

	// Nginx base configuration
	// NOTE: We intentionally omit the "user" directive to avoid warnings in non-root containers.
	sb.WriteString(fmt.Sprintf(`
worker_processes auto;
error_log stderr %s;
pid /tmp/nginx.pid;

events {
    worker_connections 1024;
}

`, errorLogLevel))

	// Generate stream block for TCP/UDP if there are any stream mappings
	if len(streamMappings) > 0 {
		sb.WriteString(generateStreamBlock(streamMappings, httpsPort))
	}

	sb.WriteString(`http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    # Custom log format without timestamp (handled by logger)
    # Includes upstream response details
    log_format sslly '$remote_addr - $remote_user "$request" '
                     '$status $body_bytes_sent "$http_referer" '
                     '"$http_user_agent" "$http_x_forwarded_for" '
                     'upstream: $upstream_addr $upstream_status $upstream_response_time';

    access_log /dev/stdout sslly;

    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;

    # Enable HTTP/2
    http2 on;

    # Allow large file uploads
    client_max_body_size 100M;

	# Temp paths for non-root containers
	client_body_temp_path /tmp/nginx/client_body;
	proxy_temp_path /tmp/nginx/proxy;
	fastcgi_temp_path /tmp/nginx/fastcgi;
	uwsgi_temp_path /tmp/nginx/uwsgi;
	scgi_temp_path /tmp/nginx/scgi;

    # Proxy buffer settings
    proxy_buffering on;
    proxy_buffer_size 4k;
    proxy_buffers 8 4k;
    proxy_busy_buffers_size 8k;

`)

	// Map: baseDomain -> []RouteConfig (for proxy routes)
	domainRoutes := make(map[string][]RouteConfig)
	// Map: baseDomain -> []StaticRouteConfig (for static sites)
	staticRoutes := make(map[string][]StaticRouteConfig)

	// Parse all routes and group by base domain
	for portKey, domainPaths := range cfg.Ports {
		// Handle static sites
		if config.IsStaticSiteKey(portKey) {
			staticSpec, hasSpec := cfg.RuntimeStaticSites[portKey]
			if !hasSpec {
				// If not in RuntimeStaticSites, try to parse from key
				spec, ok, err := config.ParseStaticSiteKey(portKey)
				if err != nil || !ok {
					continue
				}
				staticSpec = spec
			}

			// Check if index.html exists
			hasIndex := false
			if st, err := os.Stat(filepath.Join(staticSpec.Dir, "index.html")); err == nil && !st.IsDir() {
				hasIndex = true
			}

			for _, domainPath := range domainPaths {
				baseDomain, path := splitDomainPath(domainPath)
				// If route path is specified in config and domain doesn't already have a path, use it
				if staticSpec.RoutePath != "" && path == "" {
					path = staticSpec.RoutePath
				}
				staticRoutes[baseDomain] = append(staticRoutes[baseDomain], StaticRouteConfig{
					StaticSite: staticSpec,
					DomainPath: domainPath,
					BaseDomain: baseDomain,
					Path:       path,
					HasIndex:   hasIndex,
				})
			}
			continue
		}

		listenConfig := config.ParseListenKey(portKey)

		// Skip TCP/UDP mappings - they're handled in stream block
		if listenConfig.Protocol.IsStream() {
			continue
		}

		upstream := config.ParseUpstream(portKey)

		for _, domainPath := range domainPaths {
			baseDomain, path := splitDomainPath(domainPath)

			domainRoutes[baseDomain] = append(domainRoutes[baseDomain], RouteConfig{
				Upstream:   upstream,
				DomainPath: domainPath,
				BaseDomain: baseDomain,
				Path:       path,
			})
		}
	}

	// Build an ordered, de-duplicated list of base domains following the
	// declaration order of proxy.yaml (cfg.OrderedPorts). This makes server
	// block emission and the redirect server_name lists deterministic.
	allDomains := make(map[string]bool)
	for baseDomain := range domainRoutes {
		allDomains[baseDomain] = true
	}
	for baseDomain := range staticRoutes {
		allDomains[baseDomain] = true
	}
	var orderedDomains []string
	seen := make(map[string]bool)
	for _, key := range cfg.OrderedPorts {
		for _, domainPath := range cfg.Ports[key] {
			baseDomain, _ := splitDomainPath(domainPath)
			if baseDomain == "" || seen[baseDomain] {
				continue
			}
			seen[baseDomain] = true
			orderedDomains = append(orderedDomains, baseDomain)
		}
	}
	// Defensive fallback: if order info is unavailable, use the map keys so
	// every configured domain is still emitted.
	if len(orderedDomains) == 0 {
		for baseDomain := range allDomains {
			orderedDomains = append(orderedDomains, baseDomain)
		}
	}

	// Collect domains with and without certificates, in declaration order.
	var domainsWithCerts []string
	var domainsWithoutCerts []string
	for _, baseDomain := range orderedDomains {
		cert, hasCert := ssl.FindCertificate(certMap, baseDomain)
		if hasCert && cert.KeyPath != "" {
			domainsWithCerts = append(domainsWithCerts, baseDomain)
		} else {
			domainsWithoutCerts = append(domainsWithoutCerts, baseDomain)
		}
	}

	// Generate default server blocks to handle unconfigured domains
	writeDefaultServers(&sb, httpPort, httpsPort)

	// Generate the 301 redirect servers (HTTP→HTTPS and HTTPS→HTTP)
	writeRedirectServers(&sb, domainsWithCerts, domainsWithoutCerts, httpPort, httpsPort)

	// Generate server blocks for each base domain (combining proxy routes and static routes),
	// in proxy.yaml declaration order (orderedDomains).
	for _, baseDomain := range orderedDomains {
		routes := domainRoutes[baseDomain]
		staticSiteRoutes := staticRoutes[baseDomain]

		cert, hasCert := ssl.FindCertificate(certMap, baseDomain)
		if hasCert && cert.KeyPath == "" {
			hasCert = false
		}
		corsConfig := getCORSConfig(cfg, baseDomain)

		if !hasCert {
			// gRPC-only domain: h2c (cleartext HTTP/2) server block.
			if grpcRoute, ok := grpcRouteFor(routes, staticSiteRoutes); ok {
				sb.WriteString(fmt.Sprintf(`    # HTTP server block for %s (no SSL, gRPC/h2c)
    server {
        listen %s;
        http2 on;
        server_name %s;

`, baseDomain, httpPort, baseDomain))
				generateGRPCLocation(&sb, grpcRoute)
				sb.WriteString(`    }

`)
				continue
			}

			// No certificate found - create HTTP-only server block
			sb.WriteString(fmt.Sprintf(`    # HTTP server block for %s (no SSL)
    server {
        listen %s;
        server_name %s;

`, baseDomain, httpPort, baseDomain))

			// Generate location blocks for static sites
			if len(staticSiteRoutes) > 0 {
				generateStaticSiteLocations(&sb, staticSiteRoutes, corsConfig, noTrailingSlash)
			}

			// Generate location blocks for proxy routes
			if len(routes) > 0 {
				generateProxyLocations(&sb, routes, corsConfig, noTrailingSlash)
			}

			sb.WriteString(`    }

`)
			continue
		}

		// Certificate found - create HTTPS server block
		header := `    # HTTPS server block for %s
    server {
        listen %s ssl;
        %s server_name %s;
        ssl_certificate %s;
        ssl_certificate_key %s;

        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers HIGH:!aNULL:!MD5;
        ssl_prefer_server_ciphers on;

`
		// gRPC-only domain over TLS: negotiate h2 via ALPN.
		http2Line := ""
		if _, ok := grpcRouteFor(routes, staticSiteRoutes); ok {
			http2Line = "http2 on;\n        "
		}
		fmt.Fprintf(&sb, header, baseDomain, httpsPort, http2Line, baseDomain, cert.CertPath, cert.KeyPath)

		// gRPC-only domain: one location / via grpc_pass.
		if grpcRoute, ok := grpcRouteFor(routes, staticSiteRoutes); ok {
			generateGRPCLocation(&sb, grpcRoute)
			sb.WriteString(`    }

`)
			continue
		}

		// Generate location blocks for static sites
		if len(staticSiteRoutes) > 0 {
			generateStaticSiteLocations(&sb, staticSiteRoutes, corsConfig, noTrailingSlash)
		}

		// Generate location blocks for proxy routes
		if len(routes) > 0 {
			generateProxyLocations(&sb, routes, corsConfig, noTrailingSlash)
		}

		sb.WriteString(`    }

`)
	}

	sb.WriteString("}\n")

	return sb.String()
}

// writeDefaultServers emits the catch-all 444 servers for unconfigured domains.
func writeDefaultServers(sb *strings.Builder, httpPort, httpsPort string) {
	sb.WriteString(`    # Default server for HTTP - reject unconfigured domains
    server {
        listen `)
	sb.WriteString(httpPort)
	sb.WriteString(` default_server;
        server_name _;
        return 444;
    }

    # Default server for HTTPS - reject unconfigured domains
    server {
        listen `)
	sb.WriteString(httpsPort)
	sb.WriteString(` ssl default_server;
        server_name _;

        # Use a dummy self-signed certificate
        ssl_certificate /etc/nginx/ssl/dummy.crt;
        ssl_certificate_key /etc/nginx/ssl/dummy.key;

        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers HIGH:!aNULL:!MD5;

        return 444;
    }

`)
}

// writeRedirectServers emits the 301 redirect servers: HTTP→HTTPS for domains
// with certificates and HTTPS→HTTP for those without.
func writeRedirectServers(sb *strings.Builder, domainsWithCerts, domainsWithoutCerts []string, httpPort, httpsPort string) {
	if len(domainsWithCerts) > 0 {
		sb.WriteString(`    # HTTP to HTTPS redirect for domains with certificates
    server {
        listen `)
		sb.WriteString(httpPort)
		sb.WriteString(`;
        server_name `)
		sb.WriteString(strings.Join(domainsWithCerts, " "))
		sb.WriteString(`;

        location / {
            return 301 https://$host$request_uri;
        }
    }

`)
	}

	if len(domainsWithoutCerts) > 0 {
		sb.WriteString(`    # HTTPS to HTTP redirect for domains without certificates
    server {
        listen `)
		sb.WriteString(httpsPort)
		sb.WriteString(` ssl;
        server_name `)
		sb.WriteString(strings.Join(domainsWithoutCerts, " "))
		sb.WriteString(`;

        # Use a dummy self-signed certificate
        ssl_certificate /etc/nginx/ssl/dummy.crt;
        ssl_certificate_key /etc/nginx/ssl/dummy.key;

        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers HIGH:!aNULL:!MD5;

        location / {
            return 301 http://$host$request_uri;
        }
    }

`)
	}
}
