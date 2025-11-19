<p align="center">
  <img src="assets/logo.png" alt="Aero Arc Relay logo" style="max-width: 50%; height: auto;">
</p>

# Aero Arc Relay

A Go-based telemetry relay that ingests MAVLink traffic and fans it out to cloud storage. v0.1 focuses on production-ready sinks for:

- **AWS S3** – cloud persistence with file rotation buffering
- **Google Cloud Storage** – mirrored flow for GCS buckets
- **Kafka** – streaming fan-out to downstream consumers
- **Local File** – JSON rotation for on-disk retention

## Highlights

- **MAVLink ingest** via gomavlib (UDP/TCP/Serial)
- **Buffered sinks** with async queues/backpressure controls
- **Prometheus metrics** at `/metrics`
- **Health/ready probes** at `/healthz` and `/readyz`
- **Structured logging** with configurable levels/outputs

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

Edit `configs/config.yaml` to enable only the sinks you plan to run.

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
    flush_interval: "30s"
    queue_size: 1000
    backpressure_policy: "drop"
```

#### Google Cloud Storage Configuration
```yaml
sinks:
  gcs:
    bucket: "your-gcs-telemetry-bucket"
    project_id: "your-gcp-project"
    credentials: "/path/to/service-account.json"  # Optional: uses ADC if not provided
    prefix: "telemetry"
    flush_interval: "30s"
    queue_size: 1000
    backpressure_policy: "drop"
```

#### Kafka Configuration
```yaml
sinks:
  kafka:
    brokers:
      - "localhost:9092"
    topic: "telemetry-data"
    queue_size: 1000
    backpressure_policy: "drop"
```

#### File Configuration
```yaml
sinks:
  file:
    path: "/var/log/telemetry"
    format: "json"  # json, csv, binary
    rotation: "daily"  # daily, hourly
    queue_size: 1000
    backpressure_policy: "drop"
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

- `/metrics`: Prometheus exposition format
- `/healthz`: liveness probe (always 200 if the process is running)
- `/readyz`: readiness probe (200 once sinks initialize)
- Key series:
  - `aero_relay_messages_total{source,message_type}`
  - `aero_relay_sink_errors_total{sink}`
  - `aero_sink_queue_length{sink}`
  - `aero_sink_enqueued_total{sink}`
  - `aero_sink_dropped_total{sink}`

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