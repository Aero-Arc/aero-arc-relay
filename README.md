<p align="center">
  <img src="assets/logo.png" alt="Aero Arc Relay logo" style="max-width: 50%; height: auto;">
</p>

# Aero Arc Relay
[![Go Version](https://img.shields.io/github/go-mod/go-version/Aero-Arc/aero-arc-relay?filename=go.mod)](go.mod)
[![License](https://img.shields.io/github/license/Aero-Arc/aero-arc-relay)](LICENSE)
[![GitHub Release](https://img.shields.io/github/v/release/Aero-Arc/aero-arc-relay?include_prereleases)](https://github.com/Aero-Arc/aero-arc-relay/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/Aero-Arc/aero-arc-relay)](https://goreportcard.com/report/github.com/Aero-Arc/aero-arc-relay)
[![codecov](https://codecov.io/gh/Aero-Arc/aero-arc-relay/branch/main/graph/badge.svg)](https://codecov.io/gh/Aero-Arc/aero-arc-relay)


Aero Arc Relay is a production-grade telemetry ingestion pipeline for MAVLink-enabled drones and autonomous systems.  
It provides reliable ingest, structured envelopes, multi-cloud fan-out, and operational visibility — without requiring teams to build brittle one-off pipelines.

Robotics teams today still hand-roll telemetry ingestion, buffering, and cloud storage logic.  
It results in silent data loss, blocked pipelines, fragile backpressure behavior, and no unified format across UAVs, research rigs, and SITL.

Aero Arc Relay solves that.

It is a **high-confidence, async-buffered, fault-tolerant** telemetry relay written in Go, designed for:

- drone fleets & robotics platforms  
- research labs & autonomy teams  
- cloud robotics infrastructure  
- real-time telemetry dashboards  
- edge-to-cloud ingest pipelines  

Relay handles MAVLink concurrency and message parsing, applies a unified envelope format, and delivers data to S3, GCS, Kafka, or local storage with structured logs, metrics, and health probes for orchestration.

Whether you're running a single SITL instance or a fleet of autonomous aircraft, Aero Arc Relay is the ingestion backbone you plug in first — before analytics, dashboards, autonomy, or ML-based insights.

## Highlights

- **MAVLink ingest** via gomavlib (UDP/TCP/Serial) with support for multiple dialects
- **Data sinks** (v0.1) with async queues and backpressure controls:
  - AWS S3 - Cloud object storage
  - Google Cloud Storage - GCS buckets
  - Apache Kafka - Streaming platform
  - Local file storage with rotation
- **Prometheus metrics** at `/metrics` endpoint
- **Health/ready probes** at `/healthz` and `/readyz` for orchestration
- **Graceful shutdown** with context cancellation for clean container restarts
- **Environment variable support** for secure credential management
- **Structured logging** with configurable levels and outputs

## Quick Start

### Prerequisites

- Go 1.24.0 or later
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

1. Build the Docker image:
```bash
docker build -t aeroarc/relay:latest .
```

2. Run the container:
```bash
docker run -d \
  -p 14550:14550/udp \
  -p 2112:2112 \
  -v $(pwd)/configs/config.yaml:/etc/aero-arc-relay/config.yaml \
  -e AWS_ACCESS_KEY_ID=your-key \
  -e AWS_SECRET_ACCESS_KEY=your-secret \
  aeroarc/relay:latest
```

3. View logs:
```bash
docker logs -f <container-id>
```

4. Access metrics:
```bash
curl http://localhost:2112/metrics
```

### Testing with SITL

We intentionally do not containerize SITL (Software In The Loop).

SITL is a GUI-heavy simulator that varies by distro, rendering stack, and MAVLink tooling. Aero Arc Relay expects you to bring your own SITL or real drone and point it at the relay.

**Example with ArduPilot SITL:**
```bash
sim_vehicle.py --out=udp:<relay-ip>:14550
```

This keeps the relay lightweight, portable, and cloud-ready while letting you use any simulator or real hardware that suits your development and testing needs.

## Configuration

Edit `configs/config.yaml` to configure your MAVLink endpoints and data sinks.

### MAVLink Endpoints

Configure connections to your MAVLink-enabled devices:

```yaml
mavlink:
  dialect: "common"  # common, ardupilot, px4, minimal, standard, etc.
  endpoints:
    - name: "drone-1"
      protocol: "udp"      # udp, tcp, or serial
      drone_id: "drone-alpha"  # Optional: unique identifier for the drone
      mode: "1:1"           # 1:1 or multi
      port: 14550           # Required for UDP/TCP
      # address: "0.0.0.0"  # Optional: defaults to 0.0.0.0 for server mode
```

**Endpoint Modes:**
- `1:1`: One-to-one connection mode
- `multi`: Multi-connection mode for handling multiple clients

> **Note:** v0.1 supports the following endpoint modes: 1:1

**Protocols:**
- `udp`: UDP server/client mode
- `tcp`: TCP server/client mode  
- `serial`: Serial port connection

### Data Sinks

> **Note:** v0.1 supports the following sinks: AWS S3, Google Cloud Storage, Apache Kafka, and Local File. Additional sinks may be available in future versions.

```yaml
sinks:
  s3:
    bucket: "your-telemetry-bucket"
    region: "us-west-2"
    access_key: "${AWS_ACCESS_KEY_ID}"      # Environment variable expansion
    secret_key: "${AWS_SECRET_ACCESS_KEY}"  # Leave empty to use IAM role
    prefix: "telemetry"
    flush_interval: "1m"
    queue_size: 1000
    backpressure_policy: "drop"  # drop or block
```

### Environment Variables

The configuration file supports environment variable expansion using `${VAR_NAME}` syntax:

```yaml
s3:
  access_key: "${AWS_ACCESS_KEY_ID}"
  secret_key: "${AWS_SECRET_ACCESS_KEY}"
```

Set environment variables before running:
```bash
export AWS_ACCESS_KEY_ID="your-key"
export AWS_SECRET_ACCESS_KEY="your-secret"
```

## Telemetry Data Format

The relay uses a unified `TelemetryEnvelope` format for all messages:

```json
{
  "drone_id": "drone-alpha",
  "source": "drone-1",
  "timestamp_relay": "2024-01-15T10:30:00Z",
  "timestamp_device": 1705315800.123,
  "msg_id": 0,
  "msg_name": "Heartbeat",
  "system_id": 1,
  "component_id": 1,
  "sequence": 42,
  "fields": {
    "type": "MAV_TYPE_QUADROTOR",
    "autopilot": "MAV_AUTOPILOT_ARDUPILOTMEGA",
    "base_mode": 89,
    "custom_mode": 4,
    "system_status": "MAV_STATE_ACTIVE"
  },
  "raw": "base64-encoded-raw-bytes"
}
```

## Monitoring

### Metrics Endpoint

Prometheus metrics are exposed at `http://localhost:2112/metrics`:

### Health Endpoints

- **`/healthz`** - Liveness probe (always 200 if process is running)
- **`/readyz`** - Readiness probe (200 once sinks are initialized)

## Control-Plane API Contract

Building `aeroarc-api` (or any other control-plane client) against the relay?

Start here:

- `docs/control_plane_contract.md`

It documents what the relay **actually guarantees today** (supported RPCs, who “owns” session state, failure semantics, and Redis routing behavior), so you don’t have to infer behavior from code.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass (`go test ./...`)
6. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues and questions:
- Create an issue on GitHub
- Check the documentation in `internal/sinks/README.md` for sink development
- Review the configuration examples in `configs/config.yaml.example`
