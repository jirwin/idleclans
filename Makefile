.PHONY: build run clean test fmt vet deps tidy help

# Build the binary
build:
	@mkdir -p dist
	@go build -v -o dist/idleclans ./cmd/idleclans-bot

# Run the application
run:
	@go run ./cmd/idleclans-bot

# Clean build artifacts
clean:
	@rm -rf dist

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
deps:
	@go mod download

# Tidy go.mod
tidy:
	@go mod tidy

# Show help
help:
	@echo "Available targets:"
	@echo "  build   - Build the binary to ./dist/idleclans"
	@echo "  run     - Run the application"
	@echo "  clean   - Remove build artifacts"
	@echo "  test    - Run tests"
	@echo "  fmt     - Format code"
	@echo "  vet     - Run go vet"
	@echo "  deps    - Download dependencies"
	@echo "  tidy    - Tidy go.mod"
	@echo "  help    - Show this help message"

