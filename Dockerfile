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
RUN apk add --no-cache ca-certificates

# Copy the binary from builder
COPY --from=builder /build/sslly-nginx /app/sslly-nginx

# Create necessary directories
RUN mkdir -p /app/configs /app/ssl

# Set working directory
WORKDIR /app

# Replace nginx entrypoint with our application
ENTRYPOINT ["/app/sslly-nginx"]
