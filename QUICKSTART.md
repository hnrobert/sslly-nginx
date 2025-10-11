# Quick Start Guide

## Setup and Run in 5 Minutes

### 1. Prepare Configuration

Create your configuration file:

```bash
cp configs/config.example.yaml configs/config.yaml
```

Edit `configs/config.yaml`:

```yaml
1234:
  - example.com
5678:
  - api.example.com
```

### 2. Add SSL Certificates

Add your SSL certificates to the `ssl/` directory:

```bash
ssl/
├── example.com.crt
├── example.com.key
├── api.example.com_bundle.crt
└── api.example.com_bundle.key
```

### 3. Start the Service

```bash
docker-compose up -d
```

### 4. Check Logs

```bash
docker-compose logs -f
```

You should see:

```bash
Starting sslly-nginx...
Found certificate for domain: example.com
Found certificate for domain: api.example.com
Nginx configuration generated successfully
Application started successfully
```

## What It Does

1. **HTTP (Port 80)**: Redirects all traffic to HTTPS
2. **HTTPS (Port 443)**:
   - Listens for `example.com` → proxies to `localhost:1234`
   - Listens for `api.example.com` → proxies to `localhost:5678`
3. **Hot Reload**: Automatically updates when you change config or certificates

## Customizing Nginx Ports

By default the service listens on HTTP port `80` and HTTPS port `443`. You can override them via Docker Compose environment variables:

- `SSL_NGINX_HTTP_PORT` — default `80`
- `SSL_NGINX_HTTPS_PORT` — default `443`

Example (`docker-compose.yml` snippet):

```yaml
services:
  sslly-nginx:
    environment:
      - SSL_NGINX_HTTP_PORT=8080
      - SSL_NGINX_HTTPS_PORT=8443
```

## Common Commands

```bash
# Start
docker-compose up -d

# Stop
docker-compose down

# Restart
docker-compose restart

# View logs
docker-compose logs -f

# Rebuild
docker-compose up -d --build
```

## Testing Locally (Without Docker)

```bash
# Build
make build

# Run (requires nginx installed)
./sslly-nginx
```

## Troubleshooting

**Container stops immediately?**

- Check `docker-compose logs`
- Verify `configs/config.yaml` exists
- Ensure certificates exist for all domains

**Certificate not found?**

- Check filename matches pattern: `domain.crt/key` or `domain_bundle.crt/key`
- Both `.crt` and `.key` must exist

**Port already in use?**

- Service uses host network (ports 80 and 443)
- Stop other services using these ports

## Next Steps

- Read [README.md](README.md) for detailed documentation
- See [PROJECT_SUMMARY.md](PROJECT_SUMMARY.md) for architecture details
- Check [CONTRIBUTING.md](CONTRIBUTING.md) to contribute
