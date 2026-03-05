# Configuration Reference

This document describes the complete configuration format and rules for sslly-nginx.

## Configuration Format

sslly-nginx uses YAML configuration files with a key-value mapping format. Each mapping consists of a **upstream key** and a list of **listener keys**.

```yaml
upstream_key:
  - listener_key_1
  - listener_key_2
```

Each listener key in the list can be:

- **Domain**: `example.com`, `api.example.com`
- **Upstream**: `192.168.1.1:8080` (only if manually specified)
- **Listener**: `8080` (shorthand without `:`)

## Key Format Overview

There are three types of keys:

| Key Type | Format | Example | Description |
|----------|-------|---------|-------------|
| **Upstream Key** (left side) | `address` | `192.168.1.1:8080` | Defines where traffic goes |
| **Listener Key** (in list) | `domain[/path]` or `host\|port` | `example.com/api` or `192.168.1.1\|8080` | Defines where traffic is received |
| **Static Site Key** (left side) | `/directory` | `/app/static` | Defines static file serving |

### Upstream Key (Left Side)

The upstream key format follows this structure:

### Basic Format

```bash
[<protocol>][host]:[port][/path]
```

Each component is optional based on context. See the tables below for details.

## Component Tables

### 1. Protocol Prefix (Optional)

| Prefix | Description | Default Behavior |
|-------|-------------|------------------|
| `<http>` | Explicit HTTP protocol | Use HTTP for upstream connection |
| `<https>` | HTTPS protocol | Use HTTPS for upstream connection |
| `<tcp>` | TCP stream | TCP port forwarding (layer 4) |
| `<udp>` | UDP stream | UDP port forwarding (layer 4) |
| (none) | Smart mode | Default behavior based on context |

**Smart Mode Behavior:**

- For TCP/UDP upstream: automatically use TCP/UDP
- For HTTP/HTTPS upstream: **Adaptive mode**
  - If domain has SSL certificate → HTTPS
  - If upstream is `<https>` → HTTPS
  - Otherwise → HTTP
- Manual specification (`<http>` or `<https>`) disables auto-switching

### 2. Host Component

| Format | Description | Examples |
|-------|-------------|----------|
| IPv4 address | IP address without brackets | `192.168.1.1`, `10.0.0.1` |
| IPv6 address | **Must** be wrapped in brackets | `[2001:db8::1]`, `[::1]` |
| Hostname | Domain name or hostname | `localhost`, `example.com`, `server.local` |
| (none) | Default to localhost | Results in `127.0.0.1` |

**Important:** IPv6 addresses must always be wrapped in brackets: `[ipv6:address]`

### 3. Port Component

| Protocol | Required? | Default | Invalid Configuration |
|----------|-----------|--------|----------------------|
| HTTP | No | 80 | - |
| HTTPS | No | 443 | - |
| TCP | **Yes** | - | Config ignored if port missing |
| UDP | **Yes** | - | Config ignored if port missing |
| Static | **No** | - | **Error** if port specified |

**Examples:**

- `192.168.1.1:8080` → Port 8080
- `example.com` → Port 80 (HTTP default)
- `<https>example.com` → Port 443 (HTTPS default)
- `<tcp>9122` → Port 9122 (TCP, required)
- `/app/static:8080` → **ERROR** (static cannot have port)

### 4. Path Component (Optional)

Adds path-based routing to the upstream.

| Format | Description | Example |
|-------|-------------|---------|
| `/path` | URL path prefix | `/api`, `/v1`, `/app` |

**Behavior:**

- Requests to `domain.com/path` route to this upstream
- Different paths on same domain can route to different upstreams

## Domain List Format

The domain list specifies which domains route to the upstream.

### Domain List Basic Format

```yaml
- domain.com
- domain.com/path
```

### Domain with Path

You can specify a URL path directly in the domain:

```yaml
8080:
  - example.com/api
  - example.com/docs
```

This routes `/api` and `/docs` paths on `example.com` to port 8080.

## Static Site Configuration

Static sites are configured using directory paths (starting with `.` or `/`) instead of network addresses.

### Static Site Formats

#### Format 1: Directory with Domain Path

```yaml
/app/static:
  - example.com/home
  - example.com/docs
```

This serves the **same directory** at **different URL paths**:

- `example.com/home` → serves files from `/app/static`
- `example.com/docs` → serves files from `/app/static`

**Use case:** One directory, multiple entry points.

#### Format 2: Directory with Route Path (Key-based)

```yaml
"[/app/static]/home":
  - example.com
```

This serves files at `example.com/home` from `/app/static`.

**Difference from Format 1:**

- Route path (`/home`) is bound to the **directory key**
- Cannot serve same directory at multiple paths without duplicating the key
- More explicit: the route is clearly associated with the directory

**Use case:** Directory dedicated to a specific route.

### Static Site Rules

1. **Key format**: Must start with `.` or `/`
2. **Bracket syntax**: `"[directory]/route"` defines route path
3. **No ports allowed**: Static sites cannot specify ports (error if attempted)
4. **SPA support**: If `index.html` exists, Nginx enables SPA routing with `try_files`
5. **Direct serving**: Files served directly by Nginx (no proxy layer)

## Complete Examples

### HTTP Proxy

```yaml
# Proxy to localhost:8080 (HTTP default)
8080:
  - example.com

# Proxy to specific IP with custom port
192.168.1.1:3000:
  - api.example.com
```

### HTTPS Upstream

```yaml
# Proxy to HTTPS backend
<https>api.secure.com:
  - example.com

# Proxy to HTTPS backend with custom port
<https>192.168.1.1:8443:
  - secure.example.com
```

### IPv6 Configuration

```yaml
# IPv6 upstream (brackets required for address)
'[2001:db8::1]:8080':
  - ipv6.example.com

# HTTPS to IPv6 upstream
'<https>[2001:db8::1]:8443':
  - secure-ipv6.example.com
```

### Path-Based Routing

```yaml
# Main site
9012:
  - example.com

# API on different server
192.168.1.1:8080/api:
  - example.com/api

# Docs on yet another server
192.168.1.2:8080/docs:
  - example.com/docs
```

### TCP/UDP Forwarding

```yaml
# TCP forwarding (port required)
<tcp>9122:
  - 8122

# UDP forwarding (port required)
<udp>9123:
  - 8123

# TCP to specific host (use | to separate host and port)
<tcp>192.168.1.1|22:
  - 5022
```

### Static Sites

```yaml
# Basic static site
/app/static:
  - static.example.com

# Multiple paths from same directory
/app/static:
  - docs.example.com/api
  - docs.example.com/guide

# Directory with route path (key-based)
"[/app/static]/home":
  - example.com

# Directory with colon in path (allowed)
"/app/static:v2":
  - v2.example.com
```

## Configuration Processing Order

Configuration is processed from **specific to general** (child to parent):

1. **Path-specific routes** (e.g., `/api`) processed first
2. **Root routes** (`/`) processed last
3. **Longest path matches first** in generated Nginx config

This ensures that specific paths take precedence over general ones.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SSLLY_DEFAULT_HTTP_LISTEN_PORT` | 80 | HTTP listen port |
| `SSLLY_DEFAULT_HTTPS_LISTEN_PORT` | 443 | HTTPS listen port |
| `SSLLY_EXAMPLE_DIR` | `/etc/sslly/configs/` | Example config directory |

**Note:** Legacy `SSL_NGINX_HTTP_PORT` and `SSL_NGINX_HTTPS_PORT` are still supported for backward compatibility.

## Error Handling

| Error Type | Behavior |
|------------|----------|
| Missing port for TCP/UDP | Config entry ignored, error logged |
| Port specified for static site | Config entry fails with error |
| Invalid directory for static site | Config entry fails with error |
| Parse error in key | Config entry fails with error |

Non-fatal errors allow other valid configurations to continue working.

## Best Practices

1. **Use explicit protocols** for clarity when needed
2. **Use bracket syntax** for IPv6 addresses
3. **Use path-based routing** for microservices on same domain
4. **Use Format 1 for static sites** when serving multiple paths from same directory
5. **Use Format 2 for static sites** when directory is dedicated to a single route
6. **Organize certificates** in subdirectories under `ssl/`
7. **Use HTTPS upstream prefix** to prevent "plain HTTP to HTTPS port" errors
