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

# Copy the binary from builder
COPY --from=builder /build/sslly-nginx /app/sslly-nginx

# Create necessary directories
RUN mkdir -p /app/configs /app/ssl /etc/nginx/ssl

# Generate a dummy self-signed certificate for default HTTPS server
RUN openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
    -keyout /etc/nginx/ssl/dummy.key \
    -out /etc/nginx/ssl/dummy.crt \
    -subj "/C=US/ST=State/L=City/O=Organization/CN=dummy"

# Forward nginx logs to Docker log collector
# This allows nginx access and error logs to be visible via 'docker logs'
RUN ln -sf /dev/stdout /var/log/nginx/access.log \
    && ln -sf /dev/stderr /var/log/nginx/error.log

# Set working directory
WORKDIR /app

# Replace nginx entrypoint with our application
ENTRYPOINT ["/app/sslly-nginx"]
