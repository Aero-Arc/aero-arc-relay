.PHONY: build run test clean docker-build docker-run help lint fmt vet race coverage bench install-tools ci

# Variables
BINARY_NAME=aero-arc-relay
DOCKER_IMAGE=aero-arc-relay
CONFIG_PATH=configs/config.yaml
COVERAGE_DIR=coverage
COVERAGE_FILE=$(COVERAGE_DIR)/coverage.out
COVERAGE_HTML=$(COVERAGE_DIR)/coverage.html
BENCH_DIR=benchmarks

# Default target
.DEFAULT_GOAL := help

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p bin
	go build -o bin/$(BINARY_NAME) cmd/aero-arc-relay/main.go
	@echo "Build complete: bin/$(BINARY_NAME)"

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY_NAME)-linux-amd64 cmd/aero-arc-relay/main.go
	GOOS=darwin GOARCH=amd64 go build -o bin/$(BINARY_NAME)-darwin-amd64 cmd/aero-arc-relay/main.go
	GOOS=windows GOARCH=amd64 go build -o bin/$(BINARY_NAME)-windows-amd64.exe cmd/aero-arc-relay/main.go
	@echo "Multi-platform builds complete"

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./bin/$(BINARY_NAME) -config $(CONFIG_PATH)

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage: $(COVERAGE_DIR)
	@echo "Running tests with coverage..."
	go test -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	go tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

# Run tests with race detection
test-race:
	@echo "Running tests with race detection..."
	go test -race -v ./...

# Run benchmarks
bench: $(BENCH_DIR)
	@echo "Running benchmarks..."
	go test -bench=. -benchmem -run=^$$ ./... > $(BENCH_DIR)/benchmark.txt
	@echo "Benchmark results saved to $(BENCH_DIR)/benchmark.txt"

# Run all tests (unit, integration, race, coverage)
test-all: test test-race test-coverage
	@echo "All tests completed"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/
	rm -rf $(COVERAGE_DIR)/
	rm -rf $(BENCH_DIR)/
	go clean
	@echo "Clean complete"

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Code formatting complete"

# Lint code with golangci-lint
lint:
	@echo "Linting code..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	golangci-lint run --timeout=5m
	@echo "Linting complete"

# Lint with specific configuration
lint-config:
	@echo "Linting with custom config..."
	golangci-lint run --config .golangci.yml --timeout=5m

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...
	@echo "Vet complete"

# Run static analysis
staticcheck:
	@echo "Running static analysis..."
	@if ! command -v staticcheck >/dev/null 2>&1; then \
		echo "Installing staticcheck..."; \
		go install honnef.co/go/tools/cmd/staticcheck@latest; \
	fi
	staticcheck ./...
	@echo "Static analysis complete"

# Run all code quality checks
quality: fmt vet lint staticcheck
	@echo "All code quality checks complete"

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	@echo "Development tools installed"

# Security scan
security:
	@echo "Running security scan..."
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "Installing gosec..."; \
		go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest; \
	fi
	gosec ./...
	@echo "Security scan complete"

# Generate dependency graph
deps-graph:
	@echo "Generating dependency graph..."
	@if ! command -v go-callvis >/dev/null 2>&1; then \
		echo "Installing go-callvis..."; \
		go install github.com/OfekLev/go-callvis@latest; \
	fi
	go-callvis -group pkg,type -ignore github.com/bluenviron/gomavlib/v2 ./cmd/aero-arc-relay
	@echo "Dependency graph generated"

# Run CI pipeline
ci: install-tools quality test-all security
	@echo "CI pipeline complete"

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .
	@echo "Docker image built: $(DOCKER_IMAGE)"

# Run with Docker Compose
docker-run:
	@echo "Starting services with Docker Compose..."
	docker-compose up -d

# Stop Docker Compose services
docker-stop:
	@echo "Stopping Docker Compose services..."
	docker-compose down

# View logs
logs:
	@echo "Viewing logs..."
	docker-compose logs -f aero-arc-relay

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy
	go mod verify
	@echo "Dependencies installed"

# Generate documentation
docs:
	@echo "Generating documentation..."
	@if ! command -v godoc >/dev/null 2>&1; then \
		echo "Installing godoc..."; \
		go install golang.org/x/tools/cmd/godoc@latest; \
	fi
	godoc -http=:6060 &
	@echo "Documentation server started at http://localhost:6060"

# Create necessary directories
$(COVERAGE_DIR):
	@mkdir -p $(COVERAGE_DIR)

$(BENCH_DIR):
	@mkdir -p $(BENCH_DIR)

# Development workflow
dev: deps fmt vet test
	@echo "Development checks complete"

# Pre-commit checks
pre-commit: fmt vet lint test-race
	@echo "Pre-commit checks complete"

# Release preparation
release: clean build-all test-all quality security
	@echo "Release preparation complete"

# Help
help:
	@echo "Available commands:"
	@echo ""
	@echo "  Build Commands:"
	@echo "    build         - Build the application"
	@echo "    build-all     - Build for multiple platforms"
	@echo "    run           - Build and run the application"
	@echo ""
	@echo "  Testing:"
	@echo "    test          - Run tests"
	@echo "    test-coverage - Run tests with coverage report"
	@echo "    test-race     - Run tests with race detection"
	@echo "    test-all      - Run all tests (unit, race, coverage)"
	@echo "    bench         - Run benchmarks"
	@echo ""
	@echo "  Code Quality:"
	@echo "    fmt           - Format code"
	@echo "    vet           - Run go vet"
	@echo "    lint          - Lint code with golangci-lint"
	@echo "    staticcheck   - Run static analysis"
	@echo "    quality       - Run all code quality checks"
	@echo "    security      - Run security scan"
	@echo ""
	@echo "  Development:"
	@echo "    deps          - Install dependencies"
	@echo "    install-tools - Install development tools"
	@echo "    dev           - Run development checks"
	@echo "    pre-commit    - Run pre-commit checks"
	@echo "    ci            - Run CI pipeline"
	@echo ""
	@echo "  Docker:"
	@echo "    docker-build  - Build Docker image"
	@echo "    docker-run    - Start services with Docker Compose"
	@echo "    docker-stop   - Stop Docker Compose services"
	@echo "    logs          - View application logs"
	@echo ""
	@echo "  Utilities:"
	@echo "    clean         - Clean build artifacts"
	@echo "    docs          - Generate documentation"
	@echo "    release       - Prepare for release"
	@echo "    help          - Show this help message"