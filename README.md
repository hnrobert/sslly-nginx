# sslly-nginx

A smart Nginx SSL reverse proxy manager that automatically configures SSL certificates and proxies traffic to your local applications.

## Features

- 🔄 **Automatic Configuration**: Watches for configuration and SSL certificate changes and automatically reloads Nginx
- 🔒 **SSL Management**: Automatically scans and maps SSL certificates to domains
- 🔁 **Hot Reload**: Updates Nginx configuration without downtime when files change
- 🛡️ **Error Recovery**: Maintains the last working configuration and rolls back on failures
- 🐳 **Docker Ready**: Runs as a containerized service with Docker Compose
- 🚀 **CI/CD Pipeline**: Includes GitHub Actions workflows for testing, building, and releasing

## How It Works

`sslly-nginx` is a Go application that runs inside an Nginx Alpine container and manages the Nginx configuration dynamically:

1. **Configuration Monitoring**: Watches `./configs/config.yaml` for changes
2. **Certificate Scanning**: Recursively scans `./ssl` directory for certificate files
3. **Nginx Generation**: Generates Nginx configuration based on port-to-domain mappings
4. **Health Checks**: Verifies Nginx health after each reload
5. **Rollback Protection**: Maintains last working configuration for automatic recovery

## Quick Start

### Prerequisites

- Docker and Docker Compose
- SSL certificates for your domains
- Running applications on local ports

### Installation

1. Clone the repository:

   ```bash
   git clone https://github.com/hnrobert/sslly-nginx.git
   cd sslly-nginx
   ```

2. Create configuration file:

   ```bash
   cp configs/config.example.yaml configs/config.yaml
   ```

3. Edit `configs/config.yaml` with your port-to-domain mappings:

   ```yaml
   1234:
     - a.com
     - b.a.com
   5678:
     - b.com
   ```

4. Add your SSL certificates to the `ssl/` directory (see [SSL Certificate Structure](#ssl-certificate-structure))

5. Start the service:

   ```bash
   docker-compose up -d
   ```

## Configuration

### Application Configuration

The configuration file (`configs/config.yaml`) maps local ports to domain names:

```yaml
# Format: port: [list of domains]
1234:
  - example.com
  - www.example.com
5678:
  - api.example.com
```

- **Key**: Local port number where your application is running
- **Value**: List of domains that should be proxied to this port

### SSL Certificate Structure

Place SSL certificates in the `ssl/` directory. The application supports the following naming patterns:

1. **Bundle format**: `domain_bundle.crt` and `domain_bundle.key`

   - Example: `example.com_bundle.crt` and `example.com_bundle.key`

2. **Standard format**: `domain.crt` and `domain.key`
   - Example: `example.com.crt` and `example.com.key`

You can organize certificates in subdirectories:

```tree
ssl/
├── production/
│   ├── example.com_bundle.crt
│   └── example.com_bundle.key
├── staging/
│   ├── staging.example.com.crt
│   └── staging.example.com.key
└── api.example.com.crt
    └── api.example.com.key
```

**Important Notes**:

- Each domain must have exactly one certificate (no duplicates)
- Both `.crt` and `.key` files must exist
- Certificates are matched by domain name automatically

## Features in Detail

### Automatic HTTPS Redirect

All HTTP (port 80) traffic is automatically redirected to HTTPS (port 443).

### Hot Reload

The application watches for changes in:

- Configuration files (`./configs/config.yaml` or `./configs/config.yml`)
- SSL certificates (`./ssl/**/*`)

When changes are detected:

1. New configuration is generated
2. Nginx configuration is tested
3. If valid, Nginx is reloaded
4. If invalid, the previous working configuration is restored

### Error Handling

- **Initial Startup**: If configuration or certificates are invalid, the container stops
- **Runtime Errors**: If reload fails, the application:
  - Logs detailed error messages
  - Restores the last working configuration
  - Continues running with previous settings

### WebSocket Support

The generated Nginx configuration includes WebSocket support for all proxied applications.

## Development

### Build Locally

```bash
# Build the binary
make build

# Run tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Run linter
make lint
```

### Build Docker Image

```bash
# Build image
make docker-build

# Or use Docker directly
docker build -t sslly-nginx:latest .
```

### Run Locally (without Docker)

```bash
# Note: Requires Nginx installed on your system
make run
```

## CI/CD Workflows

The project includes three GitHub Actions workflows:

### 1. CI Workflow (`ci.yml`)

- **Triggers**: All branch pushes and pull requests
- **Actions**:
  - Build the application
  - Run tests
  - Run linter and format checks
- **No Docker image is built**

### 2. Docker Build Workflow (`docker-build.yml`)

- **Triggers**: Pushes to `main` and `develop` branches
- **Actions**:
  - Run tests
  - Build Docker image
  - Push to `ghcr.io`
- **Tags**:
  - `main` branch → `latest` tag
  - `develop` branch → `develop` tag

### 3. Release Workflow (`release.yml`)

- **Triggers**:
  - Git tag push (e.g., `v1.0.0`)
  - Manual workflow dispatch
- **Actions**:
  - Create tag (if workflow_dispatch)
  - Run tests
  - Build and push Docker image with version tag
  - Create GitHub release

## Docker Compose Configuration

The `docker-compose.yml` is configured with:

- **Network Mode**: `host` - Uses host networking for direct port access
- **Restart Policy**: `on-failure` - Stops on errors, auto-starts on system boot
- **Volumes**:
  - `./configs:/app/configs:ro` - Configuration (read-only)
  - `./ssl:/app/ssl:ro` - SSL certificates (read-only)

## Project Structure

```tree
sslly-nginx/
├── cmd/
│   └── sslly-nginx/
│       └── main.go              # Application entry point
├── internal/
│   ├── app/
│   │   └── app.go               # Application logic
│   ├── config/
│   │   ├── config.go            # Configuration loader
│   │   └── config_test.go
│   ├── nginx/
│   │   └── nginx.go             # Nginx management
│   ├── ssl/
│   │   ├── ssl.go               # Certificate scanner
│   │   └── ssl_test.go
│   └── watcher/
│       └── watcher.go           # File system watcher
├── .github/
│   └── workflows/
│       ├── ci.yml               # CI pipeline
│       ├── docker-build.yml     # Docker build pipeline
│       └── release.yml          # Release pipeline
├── configs/
│   └── config.example.yaml      # Example configuration
├── ssl/
│   └── README.md                # SSL certificate guide
├── Dockerfile                   # Docker image definition
├── docker-compose.yml           # Docker Compose configuration
├── Makefile                     # Build automation
├── go.mod                       # Go module definition
└── README.md                    # This file
```

## Logging

The application logs important events:

- Configuration changes detected
- Certificate scanning results
- Nginx reload success/failure
- Error details with recovery actions

Logs can be viewed with:

```bash
docker-compose logs -f
```

## Troubleshooting

### Container Stops Immediately

**Cause**: Invalid configuration or missing certificates

**Solution**:

1. Check logs: `docker-compose logs`
2. Verify `configs/config.yaml` exists and is valid YAML
3. Ensure all domains have matching certificates in `ssl/`

### Certificate Not Found

**Cause**: Certificate file naming doesn't match expected patterns

**Solution**:

1. Check certificate files follow naming pattern: `domain.crt/key` or `domain_bundle.crt/key`
2. Ensure both `.crt` and `.key` files exist
3. Check logs for certificate scanning results

### Nginx Fails to Reload

**Cause**: Configuration error or certificate issues

**Solution**:

1. Application automatically rolls back to last working configuration
2. Check logs for specific error messages
3. Fix the configuration or certificate issue
4. Changes will be automatically detected and reloaded

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

See [LICENSE](LICENSE) file for details.

## Support

For issues and questions, please use the [GitHub Issues](https://github.com/hnrobert/sslly-nginx/issues) page.
A nginx container with automatic ssl handling
