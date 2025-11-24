## Development

### Project Structure

```
aero-arc-relay/
├── cmd/aero-arc-relay/     # Main application entry point
├── internal/
│   ├── config/             # Configuration management
│   ├── relay/              # Core relay logic
│   └── sinks/              # Data sink implementations
├── pkg/
│   └── telemetry/          # Telemetry envelope structures
├── configs/                # Configuration files
├── assets/                 # Logo and assets
└── Dockerfile              # Container build definition
```

### Building

```bash
# Build binary
go build -o bin/aero-arc-relay cmd/aero-arc-relay/main.go

# Build for multiple platforms
make build-all

# Build Docker image
docker build -t aeroarc/relay:latest .
```

### Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run with race detection
go test -race ./...

# Run integration tests
go test ./internal/relay -tags=integration
```

### Code Quality

```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run

# Run static analysis
staticcheck ./...
```