# Configuration Reference

This document describes the complete configuration format and rules for sslly-nginx.

## Overview

sslly-nginx uses YAML configuration files with a key-value mapping format:

```yaml
upstream_key:
  - listener_key_1
  - listener_key_2
```

## upstream_key Format

```md
<upstream_protocol>domain:port/routes
```

or for static sites:

```md
static_directory//url_route
```

### upstream_key Components

| Component | Required | Default | Description |
|-----------|----------|---------|-------------|
| `upstream_protocol` | No | `http` | Protocol prefix: `https`, `tcp`, `udp`. Omit for `http` |
| `domain` | No | `127.0.0.1` | IP address or hostname. IPv6 must use brackets `[::1]` |
| `port` | No | Protocol default | Port number |
| `routes` | No | - | URL path routing (e.g., `/api`) |
| `static_directory` | - | - | Absolute filesystem path starting with `/` |

### Protocol Default Ports

- **HTTP** (default): 80
- **HTTPS**: 443
- **TCP/UDP**: Required (error if missing)

### Static Route Rules

- Only absolute paths starting with `/` are recognized as static sites
- Use `//` to separate directory from URL route
- Colon (`:`) in static path is NOT a port separator

### upstream_key Separator Rules

- If only `domain` or `port` is specified (not both), the `:` can be omitted
- For static routes with URL paths, use `//` as separator

### Upstream Format Examples

| Type | Format | Parsed As |
|------|--------|-----------|
| Port only | `8080` | `http://127.0.0.1:8080` |
| IP:port | `192.168.1.1:3000` | `http://192.168.1.1:3000` |
| Domain only | `api.example.com` | `http://api.example.com:80` |
| HTTPS upstream | `<https>api.secure.com:8443` | `https://api.secure.com:8443` |
| HTTPS domain | `<https>api.secure.com` | `https://api.secure.com:443` |
| TCP stream | `<tcp>9122` | TCP listen on 9122 |
| UDP stream | `<udp>9123` | UDP listen on 9123 |
| Path routing | `192.168.1.1:8080/api` | `http://192.168.1.1:8080/api` |
| IPv6 | `[::1]:8080` | `http://[::1]:8080` |
| Static simple | `/app/static` | Serve `/app/static` |
| Static with route | `/app/static//docs` | Serve `/app/static` at `/docs` |
| Static with colon | `/app/static:v2` | Serve `/app/static:v2` |

## listener_key Format

```md
<listen_protocol>listened_server_name|listened_port
```

### listener_key Components

| Component | Required | Default | Description |
|-----------|----------|---------|-------------|
| `listen_protocol` | No | Smart mode | Listen protocol: `http`, `https`, `tcp`, `udp` |
| `listened_server_name` | No | All interfaces | Server name (domain) to listen on |
| `listened_port` | No | Env var ports | Listen port |

### Smart Mode Behavior

When `listen_protocol` is not specified:

- **TCP/UDP upstream** → automatically use same protocol for listen
- **HTTP/HTTPS upstream** → adaptive based on certificate:
  - Domain has SSL certificate → HTTPS
  - Upstream is `<https>` → HTTPS
  - Otherwise → HTTP

### listener_key Separator Rules

- Use `|` to separate `listened_server_name` and `listened_port`
- If only one is specified, the `|` can be omitted

### Listener Format Examples

| Format | Example | Description |
|--------|---------|-------------|
| Domain only | `example.com` | Listen on domain, default port |
| Port only | `8080` | Listen on port, all domains |
| Domain\|port | `example.com\|8080` | Specific domain and port |
| Explicit HTTP | `<http>example.com` | Force HTTP protocol |
| Explicit HTTPS | `<https>example.com` | Force HTTPS protocol |
| TCP stream | `<tcp>8122` | TCP listen on 8122 |

## Format Types

### HTTP/HTTPS Proxy

Basic reverse proxy configuration.

```yaml
# Port only (proxies to 127.0.0.1:8080)
8080:
  - example.com
  - www.example.com

# IP:port (proxies to specific IP)
192.168.50.2:1234:
  - lan.example.com

# Hostname:port
example-server.local:8080:
  - remote.example.com

# IPv6 with brackets
"[2001:db8::1]:3000":
  - ipv6.example.com

# HTTPS upstream (prevents "plain HTTP to HTTPS port" errors)
"<https>192.168.50.2:8443":
  - secure-backend.example.com

# Path-based routing (multiple backends on same domain)
9012:
  - shared.example.com
192.168.50.2:5678/api:
  - shared.example.com/api
```

### TCP/UDP Stream Forwarding

Layer 4 port forwarding. The format is

```yaml
<protocol>listen_port:
  - target_port
```

```yaml
# TCP forwarding - listen on 9122, forward to localhost:8122
<tcp>9122:
  - 8122

# TCP forwarding to specific host
<tcp>9122:
  - 192.168.50.1|22

# UDP forwarding
<udp>9123:
  - 8123

# TCP on port 443 (ssl_preread enabled automatically)
<tcp>443:
  - 192.168.50.1|22
```

**Important:**

- Port is **required** for TCP/UDP upstream
- For TCP on port 443, `ssl_preread` is automatically enabled
- Use `|` to specify target host for stream forwarding

### Static Site Serving

Serve local directories as static websites.

```yaml
# Simple directory (serves files at root)
/app/static:
  - static.example.com

# Directory with multiple URL paths
/app/static/docs:
  - example.com/api
  - example.com/guide
  - example.com/docs

# Directory with explicit route path (using // separator)
/app/static//home:
  - yourdomain.com

# Directory with colon in path (colon is NOT port separator)
"/app/static:v2":
  - v2.example.com
```

**Rules:**

- Key must start with `/` for static detection
- Use `//` to separate directory from URL route
- Colon (`:`) in path is NOT a port separator
- SPA support enabled if `index.html` exists

## Validation Rules

### Protocol Compatibility

| Upstream | Listener | Behavior |
|---------|----------|----------|
| HTTP/HTTPS | HTTP/HTTPS | Allowed |
| HTTP/HTTPS | TCP/UDP | Error - config ignored |
| HTTP/HTTPS | Static | Error - config ignored |
| TCP/UDP | Same protocol | Warning - redundant |
| TCP/UDP | Different protocol | Error - config ignored |
| Static | HTTP/HTTPS | Allowed (smart mode) |
| Static | TCP/UDP | Error - config ignored |

### Error Handling

| Error Type | Behavior |
|------------|----------|
| YAML format error | Entire configuration fails |
| Invalid upstream key | Individual entry ignored |
| Invalid listener key | Individual entry ignored |
| Protocol mismatch | Individual entry ignored |
| Missing TCP/UDP port | Individual entry ignored |

**Note:** Non-fatal errors allow other valid configurations to continue working.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SSLLY_DEFAULT_HTTP_LISTEN_PORT` | 80 | Default HTTP listen port |
| `SSLLY_DEFAULT_HTTPS_LISTEN_PORT` | 443 | Default HTTPS listen port |

**Legacy Variables (deprecated):**

- `SSL_NGINX_HTTP_PORT`
- `SSL_NGINX_HTTPS_PORT`

## Complete Examples

### Multi-Service Configuration

```yaml
# Main application
8080:
  - example.com
  - www.example.com

# API service with path routing
192.168.1.1:3000/api:
  - example.com/api

# Admin panel on different backend
192.168.1.2:8080/admin:
  - example.com/admin

# HTTPS backend
<https>secure-api.internal:8443:
  - secure.example.com

# Static documentation
/app/docs:
  - docs.example.com

# Static with explicit route
/app/static//assets:
  - cdn.example.com

# TCP database forwarding
<tcp>5432:
  - 5432
```

### IPv6 Configuration

```yaml
# IPv6 upstream
'[2001:db8::1]:8080':
  - ipv6.example.com

# HTTPS to IPv6
'<https>[2001:db8::1]:8443':
  - secure-ipv6.example.com
```

### Static Sites with Routes

```yaml
# Simple static site
/app/static:
  - static.example.com

# Multiple routes from same directory
/app/static//docs:
  - example.com/docs
/app/static//api-docs:
  - example.com/api-docs
```
