# CORS Configuration Guide

## Overview

sslly-nginx now supports comprehensive CORS (Cross-Origin Resource Sharing) configuration through the `config.yaml` file. You can customize all CORS headers and behaviors for your domains.

## Configuration Options

### Complete CORS Configuration Example

```yaml
cors:
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
cors:
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

## Generated Nginx Configuration

The CORS configuration generates appropriate Nginx headers. Example output:

```nginx
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
cors:
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
cors:
  '*':
    allow_origin: '*'
    allow_methods: [GET, POST, PUT, DELETE, OPTIONS]
    allow_headers: [Content-Type, Authorization]

1234:
  - api.example.com
```

### Secure API with Credentials

```yaml
cors:
  'api.example.com':
    allow_origin: 'https://app.example.com'
    allow_methods: [GET, POST, OPTIONS]
    allow_headers: [Content-Type, Authorization, X-CSRF-Token]
    allow_credentials: true
    max_age: 86400

5678:
  - api.example.com
```

### Multiple Domains with Different CORS

```yaml
cors:
  'public-api.example.com':
    allow_origin: '*'
    allow_methods: [GET, OPTIONS]

  'private-api.example.com':
    allow_origin: 'https://admin.example.com'
    allow_methods: [GET, POST, PUT, DELETE, OPTIONS]
    allow_credentials: true

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
