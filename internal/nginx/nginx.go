package nginx

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

type Manager struct {
	cmd *exec.Cmd
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Start() error {
	log.Println("Starting nginx...")

	// Remove stale PID file if it exists
	os.Remove("/var/run/nginx.pid")

	cmd := exec.Command("nginx", "-g", "daemon off;")

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
			log.Printf("WARNING: Failed to write PID file: %v", err)
		}
	}

	return nil
}

func (m *Manager) Stop() {
	if m.cmd != nil && m.cmd.Process != nil {
		log.Println("Stopping nginx...")
		m.cmd.Process.Kill()
	}
}

func (m *Manager) Reload() error {
	log.Println("Reloading nginx...")

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

	// Check if any configured domains have certificates
	hasAnyCerts := false
	for _, domains := range cfg.Ports {
		for _, domain := range domains {
			if _, ok := certMap[domain]; ok {
				hasAnyCerts = true
				break
			}
		}
		if hasAnyCerts {
			break
		}
	}

	// Nginx base configuration
	sb.WriteString(`user nginx;
worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for"';

    access_log /var/log/nginx/access.log main;

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

	if hasAnyCerts {
		// If we have certificates, redirect HTTP to HTTPS
		sb.WriteString(`    # HTTP to HTTPS redirect for all domains
    server {
        listen ` + httpPort + ` default_server;
        server_name _;

        location / {
            return 301 https://$host$request_uri;
        }
    }

`)
	}

	// Add default HTTPS server that redirects to HTTP for domains without valid certificates
	sb.WriteString(`    # Default HTTPS server - redirect to HTTP for invalid/missing certificates
    server {
        listen ` + httpsPort + ` ssl default_server;
        server_name _;

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

	// Generate server blocks for each port and domain
	for port, domains := range cfg.Ports {
		for _, domain := range domains {
			cert, ok := certMap[domain]
			if !ok {
				// No certificate found - create HTTP-only server block
				log.Printf("WARNING: No certificate found for domain: %s, serving over HTTP only", domain)
				sb.WriteString(fmt.Sprintf(`    # HTTP server block for %s -> localhost:%s (no SSL)
    server {
        listen %s;
        server_name %s;

        location / {
            proxy_pass http://127.0.0.1:%s;
            proxy_http_version 1.1;

            # Standard proxy headers
            proxy_set_header Host $proxy_host;
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
        }
    }

`, domain, port, httpPort, domain, port))
				continue
			}

			// Certificate found - create HTTPS server block
			sb.WriteString(fmt.Sprintf(`    # HTTPS server block for %s -> localhost:%s
    server {
        listen %s ssl;
        server_name %s;
        ssl_certificate %s;
        ssl_certificate_key %s;

        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers HIGH:!aNULL:!MD5;
        ssl_prefer_server_ciphers on;

        location / {
            proxy_pass http://127.0.0.1:%s;
            proxy_http_version 1.1;

            # Standard proxy headers
            proxy_set_header Host $proxy_host;
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
        }
    }

`, domain, port, httpsPort, domain, cert.CertPath, cert.KeyPath, port))
		}
	}

	sb.WriteString("}\n")

	return sb.String()
}
