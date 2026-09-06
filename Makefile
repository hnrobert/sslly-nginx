.PHONY: build generate test test-coverage clean run fmt lint proto-lint proto-breaking docker-build docker-up docker-down docker-logs deps deps-update

# Build output
BIN_DIR := ./bin
BIN := $(BIN_DIR)/sslly-nginx

# Regenerate protobuf/gRPC/gateway code from proto/ into gen/ (gitignored).
# --include-imports also emits code for the googleapis annotations dependency.
generate:
	rm -rf gen
	cd proto && buf lint && buf generate --template buf.gen.go.yaml --include-imports

# Build the binary into ./bin
build: generate $(BIN_DIR)
	go build -v -o $(BIN) ./cmd/sslly-nginx

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

# Run tests
test: generate
	go test -v -race -coverprofile=coverage.out ./...

# Run tests with coverage report
test-coverage: test
	go tool cover -html=coverage.out

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)
	rm -f coverage.out

# Run the application locally
run: build
	$(BIN)

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	go vet ./...

# Lint / breaking-change check the proto sources
proto-lint:
	cd proto && buf lint

proto-breaking:
	cd proto && buf breaking --against '.git#branch=main'

# Build Docker image
docker-build: generate
	docker build -t sslly-nginx:latest .

# Start with Docker Compose
docker-up:
	docker-compose up -d

# Stop Docker Compose
docker-down:
	docker-compose down

# View Docker logs
docker-logs:
	docker-compose logs -f

# Install dependencies
deps:
	go mod download
	go mod verify

# Update dependencies
deps-update:
	go get -u ./...
	go mod tidy
