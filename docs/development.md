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

# Run container-backed integration tests
go test -tags=integration -timeout=10m ./internal/integration ./internal/registryreporter ./internal/relay
```

### Container-backed telemetry integration test

The integration suite requires a working Docker daemon. Testcontainers starts
the pinned `influxdb:3.10.3-core` image with an in-memory object store, waits for
its `/health` endpoint, creates a unique database, and maps its port
dynamically. The test also starts an in-process Relay gRPC server on an
ephemeral loopback port. A narrow fake Agent uses the generated production
`AgentGateway` client to register, establish a session, and submit a
`GLOBAL_POSITION_INT` `TelemetryFrame`.

Run it with:

```bash
go test -tags=integration -timeout=10m -v ./internal/integration
```

The first run can take a few minutes while Docker downloads InfluxDB and the
Testcontainers resource reaper. Warm runs normally complete in under 30
seconds. Containers, mapped ports, the in-memory object store, the Relay, and
client connections are cleaned up automatically on success and failure. If
Docker is unavailable, the suite reports why and skips; it never substitutes a
fake database. Startup failures include InfluxDB logs, while query failures
report the database, expected frame identity, last SQL error, and last result.
Check `docker info`, Docker socket permissions, available disk space, and
registry access when startup fails.

The test proves that:

- the Agent-facing registration, session, and telemetry stream accept a valid
  generated-protobuf client;
- Relay validation and attribution preserve Agent, aircraft, Relay, session,
  timing, and WAL identity;
- raw MAVLink `GLOBAL_POSITION_INT` units are normalized correctly;
- the production asynchronous telemetry writer and InfluxDB 3 backend persist
  the record; and
- the record is SQL-queryable before and after clean Relay shutdown.

`internal/registryreporter` also contains a real-gRPC integration test that
starts an in-process registry server on an ephemeral port and verifies Relay
registration, periodic Relay heartbeat, and active-stream agent placement
publication.

`internal/relay` exercises the generated Agent gRPC client against a real
in-process gRPC server. It proves unauthenticated registration is rejected and
that an authenticated registration/session binding publishes liveness only
while its stream is active.

It does not exercise the real Agent process, Agent WAL or backlog recovery,
serial/UDP/TCP MAVLink ingestion, ArduPilot SITL, Relay restart durability,
complete duplicate semantics, commands, DSS, deconfliction, or conformance.

To update InfluxDB, change `InfluxDBImage` in
`internal/testsupport/influxdb.go` to a published patch-specific Core tag,
review its release notes and readiness endpoint, then run the command above.
Do not replace the pin with `latest`, `core`, or another moving tag.

### Agent stream lifecycle

See [Agent Telemetry Stream Lifecycle](agent-telemetry-stream-lifecycle.md) for
the registration, telemetry ACK, control-command, stream replacement, and cleanup
ownership rules.

### Code Quality

```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run

# Run static analysis
staticcheck ./...
```
