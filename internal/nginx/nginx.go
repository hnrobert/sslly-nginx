package nginx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

type Manager struct {
	cmd *exec.Cmd
}

// RouteConfig represents a routing configuration for a domain/path combination
type RouteConfig struct {
	Upstream   config.Upstream
	DomainPath string
	BaseDomain string
	Path       string
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Start() error {
	logger.Info("Starting nginx...")

	// Remove stale PID file if it exists
	os.Remove("/var/run/nginx.pid")

	cmd := exec.Command("nginx", "-g", "daemon off;")
	// Important: by default, os/exec discards child stdout/stderr.
	// Pipe nginx logs through our logger with [NGINX-PROCS] prefix.
	cmd.Stdout = logger.NewNginxStdoutWriter()
	cmd.Stderr = logger.NewNginxStderrWriter()

	// Start nginx in background
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start nginx: %w", err)
	}

	m.cmd = cmd

	// Wait a moment for nginx to start and write PID file
	time.Sleep(2 * time.Second)

	// Write PID file manually since daemon off doesn't do it properly
	if m.cmd.Process != nil {
		pidStr := fmt.Sprintf("%d\n", m.cmd.Process.Pid)
		if err := os.WriteFile("/var/run/nginx.pid", []byte(pidStr), 0644); err != nil {
			logger.Warn("Failed to write PID file: %v", err)
		}
	}

	return nil
}

func (m *Manager) Stop() {
	if m.cmd != nil && m.cmd.Process != nil {
		logger.Info("Stopping nginx...")
		m.cmd.Process.Kill()
	}
}

func (m *Manager) Reload() error {
	logger.Info("Reloading nginx...")

	// Test configuration first
	cmd := exec.Command("nginx", "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx configuration test failed: %s", string(output))
	}

	// Use kill -HUP to reload nginx gracefully instead of nginx -s reload
	// This is more reliable when nginx is running in non-daemon mode
	if m.cmd != nil && m.cmd.Process != nil {
		if err := m.cmd.Process.Signal(os.Signal(syscall.SIGHUP)); err != nil {
			return fmt.Errorf("failed to send SIGHUP to nginx: %w", err)
		}
	} else {
		return fmt.Errorf("nginx process not found")
	}

	return nil
}

func (m *Manager) CheckHealth() error {
	// Test nginx configuration
	cmd := exec.Command("nginx", "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx health check failed: %s", string(output))
	}

	return nil
}

// getCORSConfig returns the CORS configuration for a given domain
func getCORSConfig(cfg *config.Config, domain string) *config.CORSConfig {
	// Check for wildcard first
	if corsConfig, ok := cfg.CORS["*"]; ok {
		return &corsConfig
	}

	// Check for exact domain match
	if corsConfig, ok := cfg.CORS[domain]; ok {
		return &corsConfig
	}

	return nil
}

// generateCORSHeaders generates CORS header configuration from CORSConfig
func generateCORSHeaders(corsConfig *config.CORSConfig) string {
	if corsConfig == nil {
		// Default CORS configuration
		return `            # CORS configuration
            add_header 'Access-Control-Allow-Origin' '*' always;
            add_header 'Access-Control-Allow-Methods' 'GET, HEAD, POST, PUT, DELETE, CONNECT, OPTIONS, TRACE, PATCH' always;
            add_header 'Access-Control-Allow-Headers' 'DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization' always;
            add_header 'Access-Control-Expose-Headers' 'Content-Length,Content-Range' always;

            # Handle OPTIONS preflight requests
            if ($request_method = 'OPTIONS') {
                add_header 'Access-Control-Max-Age' 1728000;
                add_header 'Content-Type' 'text/plain; charset=utf-8';
                add_header 'Content-Length' 0;
                return 204;
            }`
	}

	// Apply defaults
	allowOrigin := corsConfig.AllowOrigin
	if allowOrigin == "" {
		allowOrigin = "*"
	}

	allowMethods := corsConfig.AllowMethods
	if len(allowMethods) == 0 {
		allowMethods = []string{"GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"}
	}
	methodsStr := strings.Join(allowMethods, ", ")

	allowHeaders := corsConfig.AllowHeaders
	if len(allowHeaders) == 0 {
		allowHeaders = []string{"DNT", "User-Agent", "X-Requested-With", "If-Modified-Since", "Cache-Control", "Content-Type", "Range", "Authorization"}
	}
	headersStr := strings.Join(allowHeaders, ",")

	exposeHeaders := corsConfig.ExposeHeaders
	if len(exposeHeaders) == 0 {
		exposeHeaders = []string{"Content-Length", "Content-Range"}
	}
	exposeHeadersStr := strings.Join(exposeHeaders, ",")

	maxAge := corsConfig.MaxAge
	if maxAge == 0 {
		maxAge = 1728000 // 20 days
	}

	var sb strings.Builder
	sb.WriteString("            # CORS configuration\n")
	sb.WriteString(fmt.Sprintf("            add_header 'Access-Control-Allow-Origin' '%s' always;\n", allowOrigin))
	sb.WriteString(fmt.Sprintf("            add_header 'Access-Control-Allow-Methods' '%s' always;\n", methodsStr))
	sb.WriteString(fmt.Sprintf("            add_header 'Access-Control-Allow-Headers' '%s' always;\n", headersStr))
	sb.WriteString(fmt.Sprintf("            add_header 'Access-Control-Expose-Headers' '%s' always;\n", exposeHeadersStr))

	if corsConfig.AllowCredentials {
		sb.WriteString("            add_header 'Access-Control-Allow-Credentials' 'true' always;\n")
	}

	sb.WriteString("\n            # Handle OPTIONS preflight requests\n")
	sb.WriteString("            if ($request_method = 'OPTIONS') {\n")
	sb.WriteString(fmt.Sprintf("                add_header 'Access-Control-Allow-Origin' '%s' always;\n", allowOrigin))
	sb.WriteString(fmt.Sprintf("                add_header 'Access-Control-Allow-Methods' '%s' always;\n", methodsStr))
	sb.WriteString(fmt.Sprintf("                add_header 'Access-Control-Allow-Headers' '%s' always;\n", headersStr))
	if corsConfig.AllowCredentials {
		sb.WriteString("                add_header 'Access-Control-Allow-Credentials' 'true' always;\n")
	}
	sb.WriteString(fmt.Sprintf("                add_header 'Access-Control-Max-Age' %d always;\n", maxAge))
	sb.WriteString("                add_header 'Content-Type' 'text/plain; charset=utf-8';\n")
	sb.WriteString("                add_header 'Content-Length' 0;\n")
	sb.WriteString("                return 204;\n")
	sb.WriteString("            }")

	return sb.String()
} // splitDomainPath splits domain/path into domain and path parts
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

func GenerateConfig(cfg *config.Config, certMap map[string]ssl.Certificate) string {
	var sb strings.Builder

	// Read ports from environment with sensible defaults
	httpPort := "80"
	httpsPort := "443"
	if p := os.Getenv("SSL_NGINX_HTTP_PORT"); p != "" {
		httpPort = p
	}
	if p := os.Getenv("SSL_NGINX_HTTPS_PORT"); p != "" {
		httpsPort = p
	}

	// Nginx base configuration
	sb.WriteString(`user nginx;
worker_processes auto;
error_log stderr warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
}

http {
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

    # Proxy buffer settings
    proxy_buffering on;
    proxy_buffer_size 4k;
    proxy_buffers 8 4k;
    proxy_busy_buffers_size 8k;

`)

	// Map: baseDomain -> []RouteConfig
	domainRoutes := make(map[string][]RouteConfig)

	// Parse all routes and group by base domain
	for portKey, domainPaths := range cfg.Ports {
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

	// Collect domains with and without certificates
	var domainsWithCerts []string
	var domainsWithoutCerts []string
	for baseDomain := range domainRoutes {
		cert, hasCert := ssl.FindCertificate(certMap, baseDomain)
		if hasCert && cert.KeyPath != "" {
			domainsWithCerts = append(domainsWithCerts, baseDomain)
		} else {
			domainsWithoutCerts = append(domainsWithoutCerts, baseDomain)
		}
	}

	// Generate HTTP → HTTPS redirect for domains with certificates
	if len(domainsWithCerts) > 0 {
		sb.WriteString(`    # HTTP to HTTPS redirect for domains with certificates
    server {
        listen ` + httpPort + `;
        server_name ` + strings.Join(domainsWithCerts, " ") + `;

        location / {
            return 301 https://$host$request_uri;
        }
    }

`)
	}

	// Generate HTTPS → HTTP redirect for domains without certificates
	if len(domainsWithoutCerts) > 0 {
		sb.WriteString(`    # HTTPS to HTTP redirect for domains without certificates
    server {
        listen ` + httpsPort + ` ssl;
        server_name ` + strings.Join(domainsWithoutCerts, " ") + `;

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

	// Generate server blocks for each base domain
	for baseDomain, routes := range domainRoutes {
		cert, hasCert := ssl.FindCertificate(certMap, baseDomain)
		if hasCert && cert.KeyPath == "" {
			hasCert = false
		}
		corsConfig := getCORSConfig(cfg, baseDomain)

		if !hasCert {
			// No certificate found - create HTTP-only server block
			sb.WriteString(fmt.Sprintf(`    # HTTP server block for %s (no SSL)
    server {
        listen %s;
        server_name %s;

`, baseDomain, httpPort, baseDomain))

			// Generate location blocks for each route (sorted by path length, longest first)
			sortRoutesByPathLength(routes)
			for _, route := range routes {
				upstreamAddr := formatUpstreamAddr(route.Upstream)
				locationPath := route.Path
				if locationPath == "" {
					locationPath = "/"
				}

				proxyPass := fmt.Sprintf("%s://%s", route.Upstream.Scheme, upstreamAddr)
				if route.Upstream.Path != "" {
					proxyPass += route.Upstream.Path
				}

				// For non-root paths, add redirect and use trailing slash
				if locationPath != "/" {
					// Add exact match redirect for path without trailing slash
					sb.WriteString(fmt.Sprintf(`        location = %s {
            return 301 $scheme://$host%s/;
        }

`, locationPath, locationPath))
					// Use path with trailing slash for the actual location
					locationPath = locationPath + "/"
					// Add trailing slash to proxy_pass to strip the location path
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

`, locationPath, proxyPass, generateCORSHeaders(corsConfig)))
			}

			sb.WriteString(`    }

`)
			continue
		}

		// Certificate found - create HTTPS server block
		sb.WriteString(fmt.Sprintf(`    # HTTPS server block for %s
    server {
        listen %s ssl;
        server_name %s;
        ssl_certificate %s;
        ssl_certificate_key %s;

        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers HIGH:!aNULL:!MD5;
        ssl_prefer_server_ciphers on;

`, baseDomain, httpsPort, baseDomain, cert.CertPath, cert.KeyPath))

		// Generate location blocks for each route
		sortRoutesByPathLength(routes)
		for _, route := range routes {
			upstreamAddr := formatUpstreamAddr(route.Upstream)
			locationPath := route.Path
			if locationPath == "" {
				locationPath = "/"
			}

			proxyPass := fmt.Sprintf("%s://%s", route.Upstream.Scheme, upstreamAddr)
			if route.Upstream.Path != "" {
				proxyPass += route.Upstream.Path
			}

			// For non-root paths, add redirect and use trailing slash
			if locationPath != "/" {
				// Add exact match redirect for path without trailing slash
				sb.WriteString(fmt.Sprintf(`        location = %s {
            return 301 $scheme://$host%s/;
        }

`, locationPath, locationPath))
				// Use path with trailing slash for the actual location
				locationPath = locationPath + "/"
				// Add trailing slash to proxy_pass to strip the location path
				if !strings.HasSuffix(proxyPass, "/") {
					proxyPass += "/"
				}
			}

			// logger.Info("  Route: %s -> %s://%s (path: %s)", route.DomainPath, route.Upstream.Scheme, upstreamAddr, locationPath)

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

            # Set Secure flag for cookies when using HTTPS
            proxy_cookie_path / "/; Secure";

            # Timeouts
            proxy_connect_timeout 60s;
            proxy_send_timeout 60s;
            proxy_read_timeout 60s;

%s
        }

`, locationPath, proxyPass, generateCORSHeaders(corsConfig)))
		}

		sb.WriteString(`    }

`)
	}

	sb.WriteString("}\n")

	return sb.String()
}

// sortRoutesByPathLength sorts routes by path length (longest first) for proper nginx matching
func sortRoutesByPathLength(routes []RouteConfig) {
	// Simple bubble sort - good enough for small number of routes
	for i := 0; i < len(routes)-1; i++ {
		for j := 0; j < len(routes)-i-1; j++ {
			if len(routes[j].Path) < len(routes[j+1].Path) {
				routes[j], routes[j+1] = routes[j+1], routes[j]
			}
		}
	}
}
