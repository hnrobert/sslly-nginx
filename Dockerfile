# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o sslly-nginx ./cmd/sslly-nginx

# Runtime stage
FROM nginx:alpine

# Install required tools
RUN apk add --no-cache ca-certificates openssl

# Create necessary directories
RUN mkdir -p /app/configs /app/ssl /etc/nginx/ssl /etc/sslly/configs /var/run \
    && chmod -R g+rwX,u+rwX,o+rX /app /etc/nginx /etc/sslly/configs /var/run || true

## Copy default configuration examples (used to auto-fill missing configs on first boot)
COPY configs/proxy.example.yaml /etc/sslly/configs/proxy.example.yaml
COPY configs/cors.example.yaml /etc/sslly/configs/cors.example.yaml
COPY configs/logs.example.yaml /etc/sslly/configs/logs.example.yaml

# Generate a dummy self-signed certificate for default HTTPS server
RUN openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
    -keyout /etc/nginx/ssl/dummy.key \
    -out /etc/nginx/ssl/dummy.crt \
    -subj "/C=US/ST=State/L=City/O=Organization/CN=dummy"

# Ensure non-root nginx can read the dummy certificate/key
RUN chmod 0755 /etc/nginx/ssl || true \
    && chmod 0644 /etc/nginx/ssl/dummy.crt /etc/nginx/ssl/dummy.key || true

# Forward nginx logs to Docker log collector
# This allows nginx access and error logs to be visible via 'docker logs'
RUN ln -sf /dev/stdout /var/log/nginx/access.log \
    && ln -sf /dev/stderr /var/log/nginx/error.log

# Ensure default configs are world-readable
RUN chmod 0644 /etc/sslly/configs/*.yaml || true

# Copy the binary from builder
COPY --from=builder /build/sslly-nginx /app/sslly-nginx

# Set working directory
WORKDIR /app

# Replace nginx entrypoint with our application
ENTRYPOINT ["/app/sslly-nginx"]
