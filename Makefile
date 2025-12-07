.PHONY: build run clean test fmt vet deps tidy help web web-install web-dev

# Build the binary (includes web frontend)
build: web
	@mkdir -p dist
	@go build -v -o dist/idleclans ./cmd/idleclans-bot

# Build Go binary only (without rebuilding web)
build-go:
	@mkdir -p dist
	@go build -v -o dist/idleclans ./cmd/idleclans-bot

# Run the application
run:
	@go run ./cmd/idleclans-bot

# Clean build artifacts
clean:
	@rm -rf dist
	@rm -rf web/node_modules
	@rm -rf pkg/web/static/assets

# Run tests
test:
	@go test ./...

# Format code
fmt:
	@go fmt ./...

# Run go vet
vet:
	@go vet ./...

# Download dependencies
deps: web-install
	@go mod download

# Tidy go.mod
tidy:
	@go mod tidy

# Install web dependencies
web-install:
	@cd web && npm install

# Build web frontend
web: web-install
	@cd web && npm run build

# Run web frontend in development mode
web-dev:
	@cd web && npm run dev

# Show help
help:
	@echo "Available targets:"
	@echo "  build      - Build the binary with web frontend to ./dist/idleclans"
	@echo "  build-go   - Build Go binary only (skip web frontend)"
	@echo "  run        - Run the application"
	@echo "  clean      - Remove build artifacts"
	@echo "  test       - Run tests"
	@echo "  fmt        - Format code"
	@echo "  vet        - Run go vet"
	@echo "  deps       - Download all dependencies"
	@echo "  tidy       - Tidy go.mod"
	@echo "  web        - Build web frontend"
	@echo "  web-install - Install web dependencies"
	@echo "  web-dev    - Run web frontend in development mode"
	@echo "  help       - Show this help message"

