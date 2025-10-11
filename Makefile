.PHONY: build test clean run docker-build docker-up docker-down

# Build output
BIN_DIR := ./bin
BIN := $(BIN_DIR)/sslly-nginx

# Build the binary into ./bin
build: $(BIN_DIR)
	go build -v -o $(BIN) ./cmd/sslly-nginx

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

# Run tests
test:
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

# Build Docker image
docker-build:
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
