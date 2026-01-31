# FRP (Fast Reverse Proxy) Integration

`sslly-nginx` can be easily integrated with [FRP](https://github.com/fatedier/frp) to expose your local services through a remote server, enabling secure remote access to your applications.

## Overview

FRP allows you to expose local services behind a NAT or firewall to the internet. By combining `sslly-nginx` with FRP, you can:

- **Secure Remote Access**: Access your local applications from anywhere via HTTPS
- **Custom Domains**: Use your own domain names instead of IP addresses
- **SSL Management**: SSL certificates are configured locally in sslly-nginx for domain-based routing
- **Load Balancing**: Distribute traffic across multiple local instances

## Configuration

### 1. Configure sslly-nginx Ports

Edit your `docker-compose.yml` to use non-standard ports (avoid conflicts with FRP):

```yaml
services:
  sslly-nginx:
    environment:
      - SSL_NGINX_HTTP_PORT=9980 # HTTP traffic port
      - SSL_NGINX_HTTPS_PORT=9943 # HTTPS traffic port
```

### 2. FRP Client Configuration

Create `frpc.toml` on your local machine. Use HTTP and HTTPS proxy types for automatic protocol-based routing:

```toml
serverAddr = "your-frp-server.com"
serverPort = 7000

# FRP server authentication
auth.method = "token"
auth.token = "your-secure-token"

# HTTPS proxy - handles SSL/TLS traffic
[[proxies]]
name = "sslly-nginx-https"
type = "https"
localIP = "127.0.0.1"
localPort = 9943
customDomains = ["*.yourdomain.com", "yourdomain.com"]

# HTTP proxy - handles plain HTTP traffic and redirects
[[proxies]]
name = "sslly-nginx-http"
type = "http"
localIP = "127.0.0.1"
localPort = 9980
customDomains = ["*.yourdomain.com", "yourdomain.com"]
```

**Key Benefits of this setup:**

- **Automatic HTTPS Redirect**: FRP handles HTTP to HTTPS redirection at the server level
- **Protocol-based Routing**: HTTP requests go to port 9980, HTTPS to port 9943
- **SSL Management**: SSL certificates configured locally in sslly-nginx for domain routing
- **Domain-based Routing**: All subdomains and the main domain are handled

### 3. FRP Server Configuration

On your FRP server, ensure ports 80 and 443 are available for HTTP/HTTPS forwarding. FRP will handle the encrypted transport between the server and your local machine.

## Usage Examples

### Remote Development Access

Expose your local development environment to clients or team members:

```yaml
# configs/proxy.yaml - sslly-nginx proxy configuration
3000:
  - dev.myapp.com
  - api.dev.myapp.com
8080:
  - staging.myapp.com
```

With FRP configuration:

```toml
[[proxies]]
name = "dev-https"
type = "https"
localIP = "127.0.0.1"
localPort = 9943
customDomains = ["*.myapp.com", "myapp.com"]

[[proxies]]
name = "dev-http"
type = "http"
localIP = "127.0.0.1"
localPort = 9980
customDomains = ["*.myapp.com", "myapp.com"]
```

### Home Server Access

Make your home services accessible from anywhere:

```yaml
# configs/proxy.yaml
8123:
  - homeassistant.mydomain.com
32400:
  - plex.mydomain.com
```

### Multi-Environment Setup

Run different environments on different ports:

```yaml
# configs/proxy.yaml
3000:
  - dev.example.com
3001:
  - staging.example.com
3002:
  - prod.example.com
```

## Security Considerations

- **Firewall**: Configure your FRP server firewall to only allow necessary traffic
- **Authentication**: Use FRP's authentication features
- **SSL**: Always use HTTPS for production deployments
- **Rate Limiting**: Consider implementing rate limiting on your FRP server

## Troubleshooting

### Port Conflicts

If you encounter port conflicts, change the `SSL_NGINX_HTTP_PORT` and `SSL_NGINX_HTTPS_PORT` values in docker-compose.yml.

### FRP Connection Issues

1. Verify FRP server is running and accessible
2. Check FRP client logs: `frpc -c frpc.toml`
3. Ensure local ports are not blocked by firewall
4. Confirm FRP server configuration allows the remote ports

### SSL Certificate Issues

When using FRP with HTTP/HTTPS proxy types, SSL certificates must be configured locally in sslly-nginx:

1. **Local SSL Required**: SSL certificates must be placed in the `ssl/` directory for proper domain-based routing
2. **FRP Transport**: FRP handles encrypted transport between server and client
3. **Domain Matching**: sslly-nginx uses local certificates to match and route requests to appropriate backends
4. **Certificate Management**: Manage certificates locally using your preferred method (Let's Encrypt, self-signed, etc.)

## Advanced Configuration

### Custom FRP Settings

For production deployments, consider:

```toml
# Advanced frpc.toml
serverAddr = "frp.example.com"
serverPort = 7000
auth.method = "token"
auth.token = "your-secure-token"

# Web server dashboard (optional)
webServer.addr = "0.0.0.0"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "secure-password"

# HTTPS proxy with custom settings
[[proxies]]
name = "web-service-https"
type = "https"
localIP = "127.0.0.1"
localPort = 9943
customDomains = ["*.example.com", "example.com"]
# Additional security
transport.useEncryption = true
transport.useCompression = true

# HTTP proxy with health check
[[proxies]]
name = "web-service-http"
type = "http"
localIP = "127.0.0.1"
localPort = 9980
customDomains = ["*.example.com", "example.com"]
# Health check for HTTP
healthCheck.type = "http"
healthCheck.intervalS = 30
healthCheck.path = "/health"
```

### Load Balancing

FRP supports load balancing across multiple local instances:

```toml
[[proxies]]
name = "load-balanced-app"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8080
remotePort = 80
loadBalancer.group = "web-group"
loadBalancer.groupKey = "web-group-key"
```

## Related Documentation

- [FRP Official Documentation](https://github.com/fatedier/frp)
- [sslly-nginx Quick Start](quickstart.md)
- [sslly-nginx Configuration](https://github.com/hnrobert/sslly-nginx#configuration)
