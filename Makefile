.PHONY: build run test clean docker-build docker-run help

# Variables
BINARY_NAME=aero-arc-relay
DOCKER_IMAGE=aero-arc-relay
CONFIG_PATH=configs/config.yaml

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) cmd/aero-arc-relay/main.go

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME) -config $(CONFIG_PATH)

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -cover ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	go clean

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .

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

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Generate documentation
docs:
	@echo "Generating documentation..."
	godoc -http=:6060

# Help
help:
	@echo "Available commands:"
	@echo "  build         - Build the application"
	@echo "  run           - Build and run the application"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  clean         - Clean build artifacts"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Start services with Docker Compose"
	@echo "  docker-stop   - Stop Docker Compose services"
	@echo "  logs          - View application logs"
	@echo "  fmt           - Format code"
	@echo "  lint          - Lint code"
	@echo "  deps          - Install dependencies"
	@echo "  docs          - Generate documentation"
	@echo "  help          - Show this help message"
