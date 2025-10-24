# FRP (Fast Reverse Proxy) Integration

`sslly-nginx` can be easily integrated with [FRP](https://github.com/fatedier/frp) to expose your local services through a remote server, enabling secure remote access to your applications.

## Overview

FRP allows you to expose local services behind a NAT or firewall to the internet. By combining `sslly-nginx` with FRP, you can:

- **Secure Remote Access**: Access your local applications from anywhere via HTTPS
- **Custom Domains**: Use your own domain names instead of IP addresses
- **SSL Termination**: Handle SSL certificates centrally while proxying to local services
- **Load Balancing**: Distribute traffic across multiple local instances

## Configuration

### 1. Configure sslly-nginx Ports

Edit your `docker-compose.yml` to use non-standard ports (avoid conflicts with FRP):

```yaml
services:
  sslly-nginx:
    environment:
      - SSL_NGINX_HTTP_PORT=8080 # Change from 80
      - SSL_NGINX_HTTPS_PORT=8443 # Change from 443
```

### 2. FRP Client Configuration

Create `frpc.toml` on your local machine:

```toml
serverAddr = "your-frp-server.com"
serverPort = 7000

[[proxies]]
name = "sslly-nginx-http"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8080
remotePort = 80

[[proxies]]
name = "sslly-nginx-https"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8443
remotePort = 443
```

### 3. FRP Server Configuration

On your FRP server, ensure these ports are available for forwarding.

## Usage Examples

### Remote Development Access

Expose your local development environment to clients or team members:

```yaml
# configs/config.yaml
3000:
  - dev.myapp.com
  - api.dev.myapp.com
8080:
  - staging.myapp.com
```

### Home Server Access

Make your home services accessible from anywhere:

```yaml
# configs/config.yaml
8123:
  - homeassistant.mydomain.com
32400:
  - plex.mydomain.com
```

### Multi-Environment Setup

Run different environments on different ports:

```yaml
# configs/config.yaml
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

When using FRP, SSL certificates should be configured for the domain pointing to your FRP server, not the local machine.

## Advanced Configuration

### Custom FRP Settings

For production deployments, consider:

```toml
# Advanced frpc.toml
serverAddr = "frp.example.com"
serverPort = 7000
auth.token = "your-secure-token"

[[proxies]]
name = "web-service"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8080
remotePort = 80
# Health check
healthCheck.type = "tcp"
healthCheck.intervalS = 30
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
