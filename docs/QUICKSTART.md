# Quick Start Guide

## 🚀 One-Command Setup

Get started in seconds with default configuration:

```bash
export SSL_NGINX_HOME = ~/sslly-nginx
mkdir -p $SSL_NGINX_HOME && cd $SSL_NGINX_HOME
# Download Docker Compose configuration
curl -fsSL https://raw.githubusercontent.com/hnrobert/sslly-nginx/main/docker-compose.yml -o docker-compose.yml

# Start the service
docker-compose up -d
```

That's it! The service will start with default configuration and create necessary directories locally.

### What Happens

- **Default Configuration**: Maps incoming hostnames to backend addresses. By default:

  - `a.com` & `b.a.com` → `localhost:1234`
  - `b.com` → `localhost:5678`
  - `lan.example.com` → `192.168.31.6:1234`
  - `remote.example.com` → `remote-server:8080`

- **Local Directories**: Creates `configs/` and `ssl/` directories in your current directory
- **Hot Reload**: Automatically reloads when you modify configuration or add SSL certificates
- **Ports**: Listens on HTTP (80) and HTTPS (443) using host networking

### Customize Configuration

Edit `configs/config.yaml` to change or add routes and meeting your requirements. Format Options:

```yaml
# Format Options:
# 1. port: [domains]           - Proxies to localhost:port (127.0.0.1:port)
# 2. ip:port: [domains]        - Proxies to specified ip:port
# 3. hostname:port: [domains]  - Proxies to specified hostname:port
# 4. [ipv6]:port: [domains]    - Proxies to IPv6 address (add brackets)
```

### Add SSL Certificates

Drop certificate files into the `ssl/` directory:

```text
ssl/
├── example.com.crt
├── example.com.key
└── api.example.com_bundle.crt
```

**Note**: Certificate files are automatically detected and hot-reloaded. No restart required!

## Local Build & Setup Instructions

1. Clone the repository:

   ```bash
   git clone https://github.com/hnrobert/sslly-nginx.git
   cd sslly-nginx
   ```

2. Copy the example configuration:

   ```bash
   cp configs/config.example.yaml configs/config.yaml
   ```

3. Edit `configs/config.yaml` with your port-to-domain mappings

4. Build and Start the service:

   ```bash
   docker build -t ghcr.io/hnrobert/sslly-nginx:latest .
   docker-compose up -d
   ```

## ⚠️ Important Notes

- SSL certificates are optional - add them anytime for HTTPS support

## View Logs

```bash
docker-compose logs -f
```

## Stop Service

```bash
docker-compose down
```

## FRP Integration

For secure remote access to your local services, you can integrate `sslly-nginx` with FRP (Fast Reverse Proxy). This allows you to expose your applications through a remote server.

### Basic FRP Setup

1. **Change Ports** (to avoid conflicts): Edit `docker-compose.yml`:

   ```yaml
   environment:
     - SSL_NGINX_HTTP_PORT=9980
     - SSL_NGINX_HTTPS_PORT=9943
   ```

2. **Configure FRP Client**: Create `frpc.toml`:

   ```toml
   serverAddr = "your-frp-server.com"
   serverPort = 7000
   auth.method = "token"
   auth.token = "your-secure-token"

   # HTTPS proxy - handles SSL/TLS traffic
   [[proxies]]
   name = "sslly-nginx-https"
   type = "https"
   localIP = "127.0.0.1"
   localPort = 9943
   customDomains = ["*.yourdomain.com", "yourdomain.com"]

   # HTTP proxy - handles plain HTTP and auto-redirects
   [[proxies]]
   name = "sslly-nginx-http"
   type = "http"
   localIP = "127.0.0.1"
   localPort = 9980
   customDomains = ["*.yourdomain.com", "yourdomain.com"]
   ```

3. **Start FRP**: Run `frpc -c frpc.toml`

For detailed FRP integration guide, see [FRP.md](FRP.md).
