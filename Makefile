.PHONY: build generate generate-go test test-coverage clean run fmt lint proto-lint proto-breaking proto-push docker-build docker-up docker-down docker-logs deps deps-update

# Build output
BIN_DIR := ./bin
BIN := $(BIN_DIR)/sslly-nginx

# Regenerate code for EVERY consumer (Go server + TS/Python/C# reference
# SDKs). Remote plugins run on the Buf Schema Registry, so no local
# toolchains are needed. Outputs land in gen/ and sdks/ (both gitignored).
generate:
	rm -rf gen sdks
	cd proto && buf lint \
	  && buf generate --template buf.gen.go.yaml --include-imports \
	  && buf generate --template buf.gen.ts.yaml --include-imports \
	  && buf generate --template buf.gen.python.yaml --include-imports \
	  && buf generate --template buf.gen.csharp.yaml --include-imports

# Fast path: regenerate only the Go server code into gen/ (gitignored).
generate-go:
	rm -rf gen
	cd proto && buf lint && buf generate --template buf.gen.go.yaml --include-imports

# Build the binary into ./bin
build: generate-go $(BIN_DIR)
	go build -v -o $(BIN) ./cmd/sslly-nginx

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

# Run tests
test: generate-go
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

# Push the proto module to the Buf Schema Registry (creates the public
# repository on first push). CI attaches --label <tag> on v* tags.
proto-push:
	cd proto && buf push

# Build Docker image
docker-build: generate-go
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
