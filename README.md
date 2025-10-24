# Aero Arc Relay

A high-performance Go-based telemetry relay system that connects to edge agents, drones, and other MAVLink-enabled devices to forward telemetry data to various data sinks including S3, Kafka, and file-based storage.

## Features

- **MAVLink Integration**: Uses gomavlib for robust MAVLink protocol support
- **Multiple Connection Types**: Supports UDP, TCP, and Serial connections
- **Flexible Data Sinks**: 
  - AWS S3 for cloud storage
  - Google Cloud Storage for cloud storage
  - Apache Kafka for real-time streaming
  - File-based storage with rotation
- **High Performance**: Concurrent processing with configurable workers
- **Structured Logging**: JSON-formatted logs with configurable levels
- **Docker Support**: Containerized deployment with docker-compose

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Drone/UAV     │    │   Ground Station│    │   Edge Agent    │
│   (MAVLink)     │    │   (MAVLink)     │    │   (MAVLink)     │
└─────────┬───────┘    └─────────┬───────┘    └─────────┬───────┘
          │                      │                      │
          └──────────────────────┼──────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │    Aero Arc Relay       │
                    │  ┌─────────────────────┐│
                    │  │   MAVLink Handler   ││
                    │  │   (gomavlib)        ││
                    │  └─────────────────────┘│
                    │  ┌─────────────────────┐│
                    │  │   Telemetry Parser  ││
                    │  └─────────────────────┘│
                    │  ┌─────────────────────┐│
                    │  │   Data Sinks        ││
                    │  │   (S3/Kafka/File)   ││
                    │  └─────────────────────┘│
                    └────────────┬────────────┘
                                 │
          ┌──────────────────────┼──────────────────────┐
          │                      │                      │
    ┌─────▼─────┐        ┌───────▼───────┐      ┌───────▼───────┐
    │   AWS S3  │        │   Apache Kafka│      │  File Storage │
    │  (Cloud)  │        │  (Streaming)  │      │   (Local)     │
    └───────────┘        └───────────────┘      └───────────────┘
```

## Quick Start

### Prerequisites

- Go 1.21 or later
- Docker and Docker Compose (for containerized deployment)

### Installation

1. Clone the repository:
```bash
git clone https://github.com/makinje/aero-arc-relay.git
cd aero-arc-relay
```

2. Install dependencies:
```bash
go mod download
```

3. Configure the application:
```bash
cp configs/config.yaml.example configs/config.yaml
# Edit configs/config.yaml with your settings
```

4. Run the application:
```bash
go run cmd/aero-arc-relay/main.go -config configs/config.yaml
```

### Docker Deployment

1. Start the services:
```bash
docker-compose up -d
```

2. View logs:
```bash
docker-compose logs -f aero-arc-relay
```

3. Access Kafka UI:
Open http://localhost:8080 in your browser

## Configuration

The application is configured via YAML files. See `configs/config.yaml` for a complete example.

### MAVLink Endpoints

Configure connections to your MAVLink-enabled devices:

```yaml
mavlink:
  endpoints:
    - name: "drone-1"
      protocol: "udp"
      address: "192.168.1.100"
      port: 14550
    - name: "ground-station"
      protocol: "serial"
      address: "/dev/ttyUSB0"
      baud_rate: 57600
```

### Data Sinks

#### S3 Configuration
```yaml
sinks:
  s3:
    bucket: "your-telemetry-bucket"
    region: "us-west-2"
    access_key: "${AWS_ACCESS_KEY_ID}"
    secret_key: "${AWS_SECRET_ACCESS_KEY}"
    prefix: "telemetry"
```

#### Google Cloud Storage Configuration
```yaml
sinks:
  gcs:
    bucket: "your-gcs-telemetry-bucket"
    project_id: "your-gcp-project"
    credentials: "/path/to/service-account.json"  # Optional: uses ADC if not provided
    prefix: "telemetry"
```

#### Kafka Configuration
```yaml
sinks:
  kafka:
    brokers:
      - "localhost:9092"
    topic: "telemetry-data"
```

#### File Configuration
```yaml
sinks:
  file:
    path: "/var/log/telemetry"
    format: "json"  # json, csv, binary
    rotation: "daily"  # daily, hourly
```

## Telemetry Data Format

The system captures and forwards the following telemetry data:

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "source": "drone-1",
  "latitude": 37.7749,
  "longitude": -122.4194,
  "altitude": 100.5,
  "speed": 15.2,
  "heading": 180.0,
  "battery": 85.5,
  "signal_strength": 95,
  "flight_mode": "AUTO",
  "status": "connected"
}
```

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
│   ├── mavlink/            # MAVLink connection handling
│   └── telemetry/          # Telemetry data structures
├── configs/                # Configuration files
└── docker-compose.yml      # Docker deployment
```

### Building

```bash
# Build binary
go build -o aero-arc-relay cmd/aero-arc-relay/main.go

# Build Docker image
docker build -t aero-arc-relay .
```

### Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...
```

## Monitoring

### Health Checks

The application supports health checks via HTTP endpoint (future enhancement).

### Logging

Structured JSON logging with configurable levels:

```yaml
logging:
  level: "info"      # debug, info, warn, error
  format: "json"     # json, text
  output: "stdout"   # stdout, file
  file: "/var/log/aero-arc-relay/app.log"
```

### Metrics

Telemetry data includes performance metrics:
- Connection status
- Message processing rates
- Sink write success/failure rates

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues and questions:
- Create an issue on GitHub
- Check the documentation
- Review the configuration examples