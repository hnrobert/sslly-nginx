package nginx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// StaticRouteConfig represents a static site routing configuration
type StaticRouteConfig struct {
	StaticSite config.StaticSiteSpec
	DomainPath string
	BaseDomain string
	Path       string
	HasIndex   bool
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Start() error {
	logger.Info("Starting nginx...")

	// Remove stale PID file if it exists (we use /tmp for non-root compatibility)
	_ = os.Remove("/tmp/nginx.pid")

	// Ensure nginx temp directories are writable in non-root containers.
	_ = os.MkdirAll("/tmp/nginx/client_body", 0777)
	_ = os.MkdirAll("/tmp/nginx/proxy", 0777)
	_ = os.MkdirAll("/tmp/nginx/fastcgi", 0777)
	_ = os.MkdirAll("/tmp/nginx/uwsgi", 0777)
	_ = os.MkdirAll("/tmp/nginx/scgi", 0777)

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

	// Wait a moment for nginx to start.
	time.Sleep(2 * time.Second)

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
		sb.WriteString(generateStreamBlock(streamMappings, httpPort, httpsPort))
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

	// Collect domains with and without certificates
	var domainsWithCerts []string
	var domainsWithoutCerts []string
	allDomains := make(map[string]bool)
	for baseDomain := range domainRoutes {
		allDomains[baseDomain] = true
	}
	for baseDomain := range staticRoutes {
		allDomains[baseDomain] = true
	}
	for baseDomain := range allDomains {
		cert, hasCert := ssl.FindCertificate(certMap, baseDomain)
		if hasCert && cert.KeyPath != "" {
			domainsWithCerts = append(domainsWithCerts, baseDomain)
		} else {
			domainsWithoutCerts = append(domainsWithoutCerts, baseDomain)
		}
	}

	// Generate default server blocks to handle unconfigured domains
	sb.WriteString(`    # Default server for HTTP - reject unconfigured domains
    server {
        listen ` + httpPort + ` default_server;
        server_name _;
        return 444;
    }

    # Default server for HTTPS - reject unconfigured domains
    server {
        listen ` + httpsPort + ` ssl default_server;
        server_name _;

        # Use a dummy self-signed certificate
        ssl_certificate /etc/nginx/ssl/dummy.crt;
        ssl_certificate_key /etc/nginx/ssl/dummy.key;

        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers HIGH:!aNULL:!MD5;

        return 444;
    }

`)

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

	// Generate server blocks for each base domain (combining proxy routes and static routes)
	for baseDomain := range allDomains {
		routes := domainRoutes[baseDomain]
		staticSiteRoutes := staticRoutes[baseDomain]

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

			// Generate location blocks for static sites
			if len(staticSiteRoutes) > 0 {
				generateStaticSiteLocations(&sb, staticSiteRoutes, corsConfig)
			}

			// Generate location blocks for proxy routes
			if len(routes) > 0 {
				generateProxyLocations(&sb, routes, corsConfig)
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

		// Generate location blocks for static sites
		if len(staticSiteRoutes) > 0 {
			generateStaticSiteLocations(&sb, staticSiteRoutes, corsConfig)
		}

		// Generate location blocks for proxy routes
		if len(routes) > 0 {
			generateProxyLocations(&sb, routes, corsConfig)
		}

		sb.WriteString(`    }

`)
	}

	sb.WriteString("}\n")

	return sb.String()
}

// generateStaticSiteLocations generates nginx location blocks for static sites using root directive
func generateStaticSiteLocations(sb *strings.Builder, routes []StaticRouteConfig, corsConfig *config.CORSConfig) {
	corsHeaders := generateCORSHeaders(corsConfig)

	// Sort routes by path length (longest first)
	sortStaticRoutesByPathLength(routes)

	for _, route := range routes {
		locationPath := route.Path
		if locationPath == "" {
			locationPath = "/"
		}

		// For non-root paths, add redirect for path without trailing slash
		if locationPath != "/" {
			sb.WriteString(fmt.Sprintf(`        location = %s {
            return 301 $scheme://$host%s/;
        }

`, locationPath, locationPath))
			locationPath = locationPath + "/"
		}

		// Generate location block with root directive
		if route.HasIndex {
			// SPA support with try_files
			sb.WriteString(fmt.Sprintf(`        location %s {
            root %s;
            index index.html;
            try_files $uri $uri/ %sindex.html;

%s
        }

`, locationPath, route.StaticSite.Dir, locationPath, corsHeaders))
		} else {
			// Simple static file serving
			sb.WriteString(fmt.Sprintf(`        location %s {
            root %s;

%s
        }

`, locationPath, route.StaticSite.Dir, corsHeaders))
		}
	}
}

// generateProxyLocations generates nginx location blocks for proxy routes
func generateProxyLocations(sb *strings.Builder, routes []RouteConfig, corsConfig *config.CORSConfig) {
	corsHeaders := generateCORSHeaders(corsConfig)

	// Sort routes by path length (longest first)
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
			sb.WriteString(fmt.Sprintf(`        location = %s {
            return 301 $scheme://$host%s/;
        }

`, locationPath, locationPath))
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

// StreamMapping represents a TCP/UDP stream mapping
type StreamMapping struct {
	ListenConfig config.ListenConfig
	Upstream     config.Upstream
}

// generateStreamBlock generates the nginx stream block for TCP/UDP forwarding
func generateStreamBlock(mappings []StreamMapping, httpPort, httpsPort string) string {
	var sb strings.Builder
	sb.WriteString("stream {\n")

	// Check if we need ssl_preread (when TCP uses same port as HTTPS)
	needSSLPreread := false
	for _, m := range mappings {
		if m.ListenConfig.Protocol == config.ProtocolTCP && m.ListenConfig.Port == httpsPort {
			needSSLPreread = true
			break
		}
	}

	// Group mappings by listen port to detect conflicts
	portMappings := make(map[string][]StreamMapping)
	for _, m := range mappings {
		key := m.ListenConfig.Port
		if m.ListenConfig.Host != "" {
			key = m.ListenConfig.Host + ":" + m.ListenConfig.Port
		}
		portMappings[key] = append(portMappings[key], m)
	}

	// Generate upstreams and servers
	for _, m := range mappings {
		// Generate upstream
		upstreamName := fmt.Sprintf("stream_%s_%s", m.ListenConfig.Protocol, m.ListenConfig.Port)
		if m.ListenConfig.Host != "" {
			// Replace dots and colons for valid upstream name
			hostSafe := strings.ReplaceAll(m.ListenConfig.Host, ".", "_")
			hostSafe = strings.ReplaceAll(hostSafe, ":", "_")
			upstreamName = fmt.Sprintf("stream_%s_%s_%s", m.ListenConfig.Protocol, hostSafe, m.ListenConfig.Port)
		}

		upstreamAddr := formatUpstreamAddr(m.Upstream)

		sb.WriteString(fmt.Sprintf("    upstream %s {\n", upstreamName))
		sb.WriteString(fmt.Sprintf("        server %s;\n", upstreamAddr))
		sb.WriteString("    }\n\n")
	}

	// Handle ssl_preread for port 443 conflict
	if needSSLPreread {
		sb.WriteString("    # SSL preread for TLS/HTTPS detection on port 443\n")
		sb.WriteString("    map $ssl_preread_protocol $backend_443 {\n")
		sb.WriteString("        default stream_tcp_443;\n")
		sb.WriteString("        \"TLS\"  stream_https_443;\n")
		sb.WriteString("    }\n\n")

		// Generate the ssl_preread server
		sb.WriteString("    server {\n")
		sb.WriteString(fmt.Sprintf("        listen %s;\n", httpsPort))
		sb.WriteString("        ssl_preread on;\n")
		sb.WriteString("        proxy_pass $backend_443;\n")
		sb.WriteString("    }\n\n")
	}

	// Generate servers for each mapping
	for _, m := range mappings {
		// Skip the httpsPort if we're using ssl_preread
		if needSSLPreread && m.ListenConfig.Protocol == config.ProtocolTCP && m.ListenConfig.Port == httpsPort {
			continue
		}

		upstreamName := fmt.Sprintf("stream_%s_%s", m.ListenConfig.Protocol, m.ListenConfig.Port)
		if m.ListenConfig.Host != "" {
			hostSafe := strings.ReplaceAll(m.ListenConfig.Host, ".", "_")
			hostSafe = strings.ReplaceAll(hostSafe, ":", "_")
			upstreamName = fmt.Sprintf("stream_%s_%s_%s", m.ListenConfig.Protocol, hostSafe, m.ListenConfig.Port)
		}

		sb.WriteString("    server {\n")

		// Build listen directive
		listenAddr := m.ListenConfig.Port
		if m.ListenConfig.Host != "" {
			listenAddr = fmt.Sprintf("%s:%s", m.ListenConfig.Host, m.ListenConfig.Port)
		}

		if m.ListenConfig.Protocol == config.ProtocolUDP {
			sb.WriteString(fmt.Sprintf("        listen %s udp;\n", listenAddr))
		} else {
			sb.WriteString(fmt.Sprintf("        listen %s;\n", listenAddr))
		}

		sb.WriteString(fmt.Sprintf("        proxy_pass %s;\n", upstreamName))
		sb.WriteString("    }\n\n")
	}

	sb.WriteString("}\n\n")
	return sb.String()
}
