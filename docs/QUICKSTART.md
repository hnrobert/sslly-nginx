# Quick Start Guide

## 🚀 One-Command Setup

Get started in seconds with default configuration:

```bash
# Download Docker Compose configuration
curl -fsSL https://raw.githubusercontent.com/hnrobert/sslly-nginx/main/docker-compose.yml -o docker-compose.yml

# Start the service
docker-compose up -d
```

That's it! The service will start with default configuration and create necessary directories locally.

## What Happens

- **Default Configuration**: Maps incoming hostnames to backend addresses. By default:

  - `a.com` & `b.a.com` → `localhost:1234`
  - `b.com` → `localhost:5678`
  - `lan.example.com` → `192.168.31.6:1234`
  - `remote.example.com` → `remote-server:8080`

- **Local Directories**: Creates `configs/`, `ssl/`, and `nginx/` directories in your current directory
- **Hot Reload**: Automatically reloads when you modify configuration or add SSL certificates
- **Ports**: Listens on HTTP (80) and HTTPS (443) using host networking

## Customize Configuration

Edit `configs/config.yaml` to change or add routes and meeting your requirements. Format Options:

```yaml
# Format Options:
# 1. port: [domains]           - Proxies to localhost:port (127.0.0.1:port)
# 2. ip:port: [domains]        - Proxies to specified ip:port
# 3. hostname:port: [domains]  - Proxies to specified hostname:port
# 4. [ipv6]:port: [domains]    - Proxies to IPv6 address (add brackets)
```

## Add SSL Certificates

Drop certificate files into the `ssl/` directory:

```text
ssl/
├── example.com.crt
├── example.com.key
└── api.example.com_bundle.crt
```

**Note**: Certificate files are automatically detected and hot-reloaded. No restart required!

## ⚠️ Important Notes

- The `nginx/nginx.conf` file is **auto-generated** and will be **overwritten** on configuration changes. By default it is mounted for your reference.
- **Do not modify** `nginx/nginx.conf` directly unless you just want some temporary changes for testing
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
     - SSL_NGINX_HTTP_PORT=8080
     - SSL_NGINX_HTTPS_PORT=8443
   ```

2. **Configure FRP Client**: Create `frpc.toml`:

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

3. **Start FRP**: Run `frpc -c frpc.toml`

For detailed FRP integration guide, see [FRP.md](FRP.md).
