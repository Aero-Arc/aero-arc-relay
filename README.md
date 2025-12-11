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
It provides reliable ingest, structured envelopes, and **high-performance NATS JetStream plumbing** — without requiring teams to build brittle one-off pipelines.

Robotics teams today still hand-roll telemetry ingestion, buffering, and streaming logic.  
It results in silent data loss, blocked pipelines, fragile backpressure behavior, and no unified format across UAVs, research rigs, and SITL.

Aero Arc Relay solves that with **modern messaging architecture**.

It is a **high-confidence, async-buffered, fault-tolerant** telemetry relay written in Go, designed for:

- drone fleets & robotics platforms  
- research labs & autonomy teams  
- cloud-native infrastructure  
- real-time telemetry dashboards  
- edge-to-cloud streaming pipelines  

Relay handles MAVLink concurrency and message parsing, applies a unified envelope format, and delivers data to **NATS JetStream**, S3, GCS, or local storage with structured constellation logging, metrics, and health probes for orchestration.

Whether you're running a single SITL instance or a fleet of autonomous aircraft, Aero Arc Relay is the ingestion backbone you plug in first — before analytics, dashboards, autonomy, or ML-based insights.

## Highlights

- **MAVLink ingest** via gomavlib (UDP/TCP/Serial) with support for multiple dialects
- **NATS JetStream streaming** with entity-specific subjects (`constellation.telemetry.{entity_id}`)
- **Device State Tracking** via NATS Key-Value stores (latest values for GPS, battery, attitude, etc.)
- **Data sinks** with async queues and backpressure controls:
  - **NATS JetStream** - Modern streaming platform with persistence and replay
  - AWS S3 - Cloud object storage
  - Google Cloud Storage - GCS buckets  
  - Local file storage with rotation
- **Token authentication** - JWT and credentials file support for NATS
- **Constellation logging** - Structured logging with Zap integration
- **Prometheus metrics** at `/metrics` endpoint
- **Health/ready probes** at `/healthz` and `/readyz` for orchestration
- **Graceful shutdown** with context cancellation for clean container restarts
- **Environment variable support** for secure credential management
- **Pure Go** - No CGO dependencies, ARM64 compatible

## Quick Start

### Prerequisites

- Go 1.24.0 or later
- [Task](https://taskfile.dev/installation/) (Taskfile runner)
- NATS server with JetStream enabled (for streaming functionality)
- Docker and Docker Compose (for containerized deployment)

### Installation

1. Clone the repository:
```bash
git clone https://github.com/makinje/aero-arc-relay.git
cd aero-arc-relay
```

2. Install dependencies:
```bash
task deps
```

3. Configure the application:
```bash
cp configs/config.yaml.example configs/config.yaml
# Edit configs/config.yaml with your settings
```

4. Start NATS server with JetStream:
```bash
# Run NATS server locally with JetStream enabled
docker run -p 4222:4222 nats:latest -js
```

5. Run the application:
```bash
# Set environment variables and run
LOG_LEVEL=INFO task run
```

### Docker Deployment

1. Build the Docker image:
```bash
task docker-build
```

2. Start services with Docker Compose:
```bash
task docker-run
```

This will start the relay and necessary services as defined in `docker-compose.yml`.

3. View logs:
```bash
task logs
```

4. Access metrics:
```bash
curl http://localhost:2112/metrics
```

5. Stop services:
```bash
task docker-stop
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

Configure your data destinations. **NATS JetStream is the recommended sink for real-time streaming and replay capabilities.**

#### NATS JetStream (Recommended)

```yaml
sinks:
  nats:
    url: "nats://localhost:4222"
    subject: "constellation.telemetry.{entity_id}"  # Entity-specific routing
    token: "${NATS_TOKEN}"                          # JWT token for authentication
    # creds_file: "/path/to/nats.creds"            # Alternative: credentials file
    queue_size: 1000
    backpressure_policy: "drop"                     # drop or block
    stream:
      name: "MAVLINK_TELEMETRY"
      subjects: 
        - "constellation.telemetry.>"               # Captures all entity traffic
      storage: "file"                               # "memory" or "file"
      max_age: "24h"                                # Message retention
      max_msgs: 1000000                             # Max messages to retain
      compression: true                             # Enable S2 compression
```

**Subject Patterns:**
- **1:1 mode**: `constellation.telemetry.{entity_id}` → `constellation.telemetry.drone-alpha`
- **Multi mode**: `constellation.telemetry.{org_id}` → `constellation.telemetry.fleet-001`

#### NATS JetStream KV (Device State)

Configure a Key-Value bucket to track the *latest* aggregated state of each device. This is ideal for dashboards needing "current status" without replaying streams.

```yaml
sinks:
  nats:
    # ... standard connection config ...
    kv:
      bucket: "mavlink_state"
      key_pattern: "{drone_id}"        # Key template (supported: {drone_id}, {entity_id}, {source})
      description: "Current state of drone fleet"
      ttl: "24h"                       # Time-to-live for stale keys
      storage: "file"                  # "memory" or "file"
      max_bytes: 104857600             # Max bucket size (100MB)
      replicas: 1
```

#### Cloud Storage

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
nats:
  url: "${NATS_URL:-nats://localhost:4222}"
  token: "${NATS_TOKEN}"
s3:
  access_key: "${AWS_ACCESS_KEY_ID}"
  secret_key: "${AWS_SECRET_ACCESS_KEY}"
```

Set environment variables before running:
```bash
export NATS_TOKEN="eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9..."
export AWS_ACCESS_KEY_ID="your-key"
export AWS_SECRET_ACCESS_KEY="your-secret"
export LOG_LEVEL="INFO"  # DEBUG, INFO, WARN, ERROR
```

## NATS JetStream Streaming

Aero Arc Relay provides first-class support for NATS JetStream, offering persistent streaming with replay capabilities for MAVLink telemetry.

### Key Benefits

- **Entity Isolation**: Each drone/vehicle gets its own subject namespace
- **Replay Capability**: Historical telemetry available for analysis and debugging
- **High Throughput**: Optimized for real-time telemetry ingestion
- **Persistence**: Configurable storage (memory/file) with retention policies
- **Authentication**: JWT token and credentials file support
- **Compression**: S2 compression for bandwidth optimization

### Subject Architecture

The relay uses a hierarchical subject structure for optimal routing and filtering:

```
constellation.telemetry.{entity_id}
```

**Examples:**
- Drone Alpha: `constellation.telemetry.drone-alpha`
- Vehicle Beta: `constellation.telemetry.vehicle-beta` 
- Fleet Operations: `constellation.telemetry.fleet-001`

### Stream Configuration

Streams are automatically created and managed based on your configuration:

```yaml
stream:
  name: "MAVLINK_TELEMETRY"
  subjects: ["constellation.telemetry.>"]  # Capture all constellation telemetry
  storage: "file"                          # Persistent storage
  max_age: "24h"                           # Retain for 24 hours
  max_msgs: 1000000                        # Maximum message count
  compression: true                        # Enable S2 compression
```

### Consuming Messages

Subscribe to entity-specific or wildcard subjects:

```bash
# Subscribe to specific entity
nats sub "constellation.telemetry.drone-alpha"

# Subscribe to all telemetry
nats sub "constellation.telemetry.>"

# Subscribe to all drones
nats sub "constellation.telemetry.drone-*"
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

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass (`task test`)
6. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues and questions:
- Create an issue on GitHub
- Check the documentation in `internal/sinks/README.md` for sink development
- Review the configuration examples in `configs/config.yaml.example`
