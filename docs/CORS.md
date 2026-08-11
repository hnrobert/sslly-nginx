# CORS Configuration Guide

## Overview

sslly-nginx supports comprehensive CORS (Cross-Origin Resource Sharing) configuration through the `cors.yaml` file. You can customize all CORS headers and behaviors for your domains.

## Configuration Options

### Complete CORS Configuration Example

```yaml
'*': # Wildcard applies to all domains
  # Origin to allow (use "*" for all origins, or specific origin like "https://example.com")
  allow_origin: '*'

  # HTTP methods allowed for CORS requests
  # Default (if not specified): All methods (GET, HEAD, POST, PUT, DELETE, CONNECT, OPTIONS, TRACE, PATCH)
  allow_methods:
    - GET
    - HEAD
    - POST
    - PUT
    - DELETE
    - CONNECT
    - OPTIONS
    - TRACE
    - PATCH

  # Headers that can be used in the actual request
  allow_headers:
    - DNT
    - User-Agent
    - X-Requested-With
    - If-Modified-Since
    - Cache-Control
    - Content-Type
    - Range
    - Authorization

  # Headers exposed to the browser
  expose_headers:
    - Content-Length
    - Content-Range

  # How long (in seconds) the preflight response can be cached
  max_age: 1728000 # 20 days

  # Whether to allow credentials (cookies, authorization headers, etc.)
  allow_credentials: false
```

### Configuration Fields

| Field               | Type    | Default                                                                            | Description                                                                               |
| ------------------- | ------- | ---------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `allow_origin`      | string  | `"*"`                                                                              | Allowed origin. Use `"*"` for all origins or specific origin like `"https://example.com"` |
| `allow_methods`     | array   | `["GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"]` | HTTP methods allowed for CORS requests                                                    |
| `allow_headers`     | array   | Common headers                                                                     | Request headers allowed in CORS requests                                                  |
| `expose_headers`    | array   | `["Content-Length", "Content-Range"]`                                              | Response headers exposed to the browser                                                   |
| `max_age`           | integer | `1728000`                                                                          | Preflight cache duration in seconds (20 days default)                                     |
| `allow_credentials` | boolean | `false`                                                                            | Whether to allow credentials (cookies, auth headers)                                      |

### Per-Domain CORS Configuration

You can also configure CORS for specific domains:

```yaml
# Global default for all domains
'*':
  allow_origin: '*'
  allow_methods: [GET, POST, OPTIONS]

# Specific configuration for api.example.com
'api.example.com':
  allow_origin: 'https://app.example.com'
  allow_methods: [GET, POST, PUT, DELETE, OPTIONS]
  allow_headers: [Content-Type, Authorization]
  allow_credentials: true
  max_age: 86400 # 1 day
```

### Wildcard Matching & Precedence

CORS keys support three forms:

| Form             | Example              | Matches                                                                                        |
| ---------------- | -------------------- | ---------------------------------------------------------------------------------------------- |
| Exact domain     | `api.example.com`    | Only `api.example.com`                                                                         |
| Suffix wildcard  | `*.example.com`      | Any subdomain (`api.example.com`, `app.api.example.com`) — **not** the bare apex `example.com` |
| Catch-all        | `*`                  | Every domain                                                                                   |

When multiple keys match a domain, the **most specific** wins, in this order:

1. Exact domain match
2. **Longest** matching `*.suffix` (e.g. `*.api.example.com` wins over `*.example.com`)
3. The `*` catch-all

```yaml
# Resolution for api.cpu.ibuduan.com (most specific wins):
'api.cpu.ibuduan.com': { allow_origin: 'https://app.ibuduan.com' }   # 1. exact — wins
'*.cpu.ibuduan.com':   { allow_origin: 'https://cpu.ibuduan.com' }   # 2. longer suffix
'*.ibuduan.com':       { allow_origin: '*' }                          # 3. shorter suffix
'*':                   { allow_origin: '*' }                          # 4. catch-all
```

> **Note:** A `*.suffix` key matches subdomains only. `*.example.com` will **not** match the bare apex `example.com` — add an exact `example.com` key for that.

This mirrors how TLS certificates are matched (see `FindCertificate`).

### Layer Inheritance & Clearing

Matching layers are merged field-by-field from the least to the most specific
(`*` → `*.suffix` shortest → longest → exact). A field you **don't** mention in
a more specific key is **inherited** from the lower-priority layers, so a
per-domain entry only needs to state what differs:

```yaml
'*':
  allow_origin: '*'
  allow_methods: [GET, POST]
  allow_headers: [Content-Type]

'api.example.com':
  allow_credentials: true   # only this field overrides; the rest is inherited
```

To **suppress** a header that a lower layer would otherwise emit, set it to an
empty value (`""` or `null`). An explicitly-empty field is *cleared* — the
corresponding header is omitted entirely from that domain's server block:

```yaml
'*':
  allow_origin: '*'
  allow_methods: [GET, POST]
  allow_headers: [Content-Type]

'api.example.com':
  allow_origin: ''   # ← Access-Control-Allow-Origin is OMITTED for this domain
                     #   methods (GET, POST) and headers (Content-Type) are inherited
```

| Field state in the winning layer(s) | Resulting header                         |
| ----------------------------------- | ---------------------------------------- |
| Set to a value                      | Emitted with that value                  |
| Not set in any matching layer       | Inherited, or the built-in default       |
| Explicitly `""` or `null`           | **Omitted** (cleared)                    |

This applies to `allow_origin`, `allow_methods`, `allow_headers`, and
`expose_headers`. `max_age` and `allow_credentials` are not clearable (set them
to the value you want, or omit to inherit/default).

## Generated Nginx Configuration

> **Ordering:** Nginx server blocks are emitted in the declaration order of `proxy.yaml` (top-level key order, then each key's domain list in order). The output is deterministic across reloads.

The CORS configuration generates appropriate Nginx headers. Example output:

```conf
# CORS configuration
add_header 'Access-Control-Allow-Origin' '*' always;
add_header 'Access-Control-Allow-Methods' 'GET, POST, OPTIONS, PUT, DELETE' always;
add_header 'Access-Control-Allow-Headers' 'DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization' always;
add_header 'Access-Control-Expose-Headers' 'Content-Length,Content-Range' always;

# Handle OPTIONS preflight requests

if ($request_method = 'OPTIONS') {
    add_header 'Access-Control-Allow-Origin' '*' always;
    add_header 'Access-Control-Allow-Methods' 'GET, POST, OPTIONS, PUT, DELETE' always;
    add_header 'Access-Control-Allow-Headers' 'DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization' always;
    add_header 'Access-Control-Max-Age' 1728000 always;
    add_header 'Content-Type' 'text/plain; charset=utf-8';
    add_header 'Content-Length' 0;
    return 204;
}

```

## Important Notes

### Credentials and Origins

When `allow_credentials: true`, you **cannot** use `allow_origin: "*"`. You must specify an exact origin:

```yaml
'api.example.com':
  allow_origin: 'https://app.example.com' # Must be specific
  allow_credentials: true
```

### Default Behaviour

If you omit the CORS configuration, sslly-nginx will use sensible defaults:

- `allow_origin`: `"*"`
- `allow_methods`: `["GET", "POST", "OPTIONS", "PUT", "DELETE"]`
- Common security headers
- 20-day preflight cache

### Best Practices

1. **Development**: Use wildcard `"*"` for `allow_origin`
2. **Production**: Specify exact origins for security
3. **Credentials**: Only enable when necessary and with specific origins
4. **Methods**: Only allow methods your API actually uses
5. **Headers**: Include all headers your frontend needs

## Examples

### Basic API with CORS

```yaml
# cors.yaml
'*':
  allow_origin: '*'
  allow_methods: [GET, POST, PUT, DELETE, OPTIONS]
  allow_headers: [Content-Type, Authorization]

# proxy.yaml
1234:
  - api.example.com
```

### Secure API with Credentials

```yaml
# cors.yaml
'api.example.com':
  allow_origin: 'https://app.example.com'
  allow_methods: [GET, POST, OPTIONS]
  allow_headers: [Content-Type, Authorization, X-CSRF-Token]
  allow_credentials: true
  max_age: 86400

# proxy.yaml
5678:
  - api.example.com
```

### Multiple Domains with Different CORS

```yaml
# cors.yaml
'public-api.example.com':
  allow_origin: '*'
  allow_methods: [GET, OPTIONS]
 
'private-api.example.com':
  allow_origin: 'https://admin.example.com'
  allow_methods: [GET, POST, PUT, DELETE, OPTIONS]
  allow_credentials: true

# proxy.yaml
8080:
  - public-api.example.com

9090:
  - private-api.example.com
```

## Testing CORS

You can test CORS headers with curl:

```bash
# Test preflight request
curl -X OPTIONS http://api.example.com/endpoint \
  -H "Origin: https://example.com" \
  -H "Access-Control-Request-Method: POST" \
  -H "Access-Control-Request-Headers: Content-Type" \
  -v

# Test actual request
curl -X GET http://api.example.com/endpoint \
  -H "Origin: https://example.com" \
  -v
```

Check for these headers in the response:

- `Access-Control-Allow-Origin`
- `Access-Control-Allow-Methods`
- `Access-Control-Allow-Headers`
- `Access-Control-Max-Age`
