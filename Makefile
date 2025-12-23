.PHONY: all build build-server build-benchmark run run-json run-debug \
        benchmark benchmark-search benchmark-custom test test-coverage test-race \
        fmt vet lint tidy clean install-tools help verify

# Build output directory
BUILD_DIR := bin

# Binary names
SERVER_BIN := $(BUILD_DIR)/vex-server
BENCHMARK_BIN := $(BUILD_DIR)/vex-benchmark

# Go commands
GOCMD := go
GOBUILD := $(GOCMD) build
GORUN := $(GOCMD) run
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOFMT := $(GOCMD) fmt
GOVET := $(GOCMD) vet
GOMOD := $(GOCMD) mod

# Version info
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

# Default target
all: tidy fmt ci build

# Build both server and benchmark
build: build-server build-benchmark

# Build server binary
build-server:
	@echo "Building vex-server..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(SERVER_BIN) ./cmd/vex-server

# Build benchmark binary
build-benchmark:
	@echo "Building vex-benchmark..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BENCHMARK_BIN) ./cmd/vex-benchmark

# Run server directly
run:
	@echo "Starting Vex server..."
	$(GORUN) ./cmd/vex-server/main.go

# Run server with JSON logging
run-json:
	@echo "Starting Vex server with JSON logging..."
	$(GORUN) ./cmd/vex-server/main.go -log-format=json

# Run server with debug logging
run-debug:
	@echo "Starting Vex server with debug logging..."
	$(GORUN) ./cmd/vex-server/main.go -log-level=debug

# Run benchmark (insert mode)
benchmark:
	@echo "Running insert benchmark..."
	$(GORUN) ./cmd/vex-benchmark/main.go -mode=insert -concurrency=50 -n=100000

# Run benchmark (search mode)
benchmark-search:
	@echo "Running search benchmark..."
	$(GORUN) ./cmd/vex-benchmark/main.go -mode=search -concurrency=50 -n=50000

# Run custom benchmark
benchmark-custom:
	@echo "Running custom benchmark..."
	@echo "Usage: make benchmark-custom ARGS='-mode=insert -concurrency=100 -n=200000'"
	$(GORUN) ./cmd/vex-benchmark/main.go $(ARGS)

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	$(GOTEST) -v -race ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

# Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

# Run golangci-lint
lint:
	@echo "Running golangci-lint..."
	golangci-lint run ./...

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GOCMD) install golang.org/x/tools/cmd/goimports@latest
	$(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Help
help:
	@echo "Vex Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  all              - Format, tidy, and build everything (default)"
	@echo "  build            - Build vex-server and vex-benchmark binaries"
	@echo "  build-server     - Build only the vex-server binary"
	@echo "  build-benchmark  - Build only the vex-benchmark binary"
	@echo "  run              - Run the server directly"
	@echo "  run-json         - Run the server with JSON logging"
	@echo "  run-debug        - Run the server with debug logging"
	@echo "  benchmark        - Run insert benchmark"
	@echo "  benchmark-search - Run search benchmark"
	@echo "  test             - Run all tests"
	@echo "  test-coverage    - Run tests with coverage report"
	@echo "  fmt              - Format all Go code"
	@echo "  vet              - Run go vet"
	@echo "  tidy             - Tidy Go modules"
	@echo "  clean            - Clean build artifacts"
	@echo "  lint             - Run golangci-lint"
	@echo "  install-tools    - Install development tools"
	@echo "  verify           - Run all verification checks locally (test, lint, race)"
	@echo "  help             - Show this help message"

# Run all verify checks locally (mirrors GitHub Actions CI)
verify: tidy fmt vet
	@echo "=== Running CI checks locally ==="
	@echo ""
	@echo ">>> Running tests with race detector..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./internal/...
	@echo ""
	@echo ">>> Running golangci-lint..."
	golangci-lint run ./...
	@echo ""
	@echo ">>> Coverage Summary:"
	@$(GOCMD) tool cover -func=coverage.out | tail -n 1
	@echo ""
	@echo "=== All CI checks passed! ==="
