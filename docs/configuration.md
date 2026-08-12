### Registry reporting

The registry is the current-state control plane. When enabled, Relay registers
its routable address during startup, renews relay liveness periodically, and
registers agents when their telemetry stream becomes active. An independent
background heartbeat keeps every active stream live; Registry RPC latency is
never placed on the telemetry admission/ACK path. Telemetry payloads are not
stored in the registry.

```yaml
registry:
  enabled: true
  address: "registry.internal:50051"
  relay_id: "relay-us-central-1"
  advertise_address: "relay-us-central-1.internal"
  heartbeat_interval: "10s"
  request_timeout: "5s"
  tls:
    enabled: true
    ca_file: "/run/secrets/registry-ca.pem"
    server_name: "registry.internal"
```

`relay_id` may be omitted only when `telemetry.relay_id` supplies the same
identity. `advertise_address` must be reachable by trusted control-plane
clients; the Relay gRPC port comes from the process `--grpc-port` flag. TLS is
recommended outside isolated local development. If registry reporting cannot
initialize, Relay startup fails. If an agent cannot be registered, its Relay
telemetry stream is rejected so dashboards and routing never silently omit an
accepted connection. The initial registration handshake alone does not make an
agent appear connected. Registry heartbeat failures do not reject
already-admitted telemetry and are retried.

Agent registrations and heartbeats both carry the configured `relay_id`.
Registry accepts a heartbeat only from the Relay that currently owns the Agent
placement, preventing an older Relay connection from extending a reassigned
Agent's liveness.

Registry publication also requires the Agent-facing session to be
authenticated. Configure a distinct high-entropy bearer token for every Agent:

```yaml
agent_auth:
  tokens:
    "agent-id": "${AGENT_ID_TOKEN}"
```

The Agent sends `authorization: Bearer <token>` metadata on both `Register` and
`TelemetryStream`. The stream additionally sends the `aero-arc-session-id`
returned by `Register`; Relay verifies the credential and session binding
before publishing liveness. Supply tokens through environment-backed secrets,
never literal values committed to a configuration file. Registry-enabled
startup rejects an empty credential map. Registry-disabled local/demo flows may
omit it.

### Normalized telemetry

The official Aero Arc telemetry path uses InfluxDB 3 Core and is configured
separately from generic sinks:

```yaml
telemetry:
  enabled: true
  backend: "influxdb3"
  relay_id: "relay-us-central-1"
  queue_capacity: 10000
  workers: 2
  batch_size: 500
  flush_interval: "1s"
  enqueue_timeout: "100ms"
  write_timeout: "5s"
  max_retries: 3
  retry_backoff: "200ms"
  influxdb:
    host: "http://localhost:8181"
    token: "${INFLUXDB3_TOKEN}"
    database: "aero_arc"
  agent_mappings:
    "agent-id":
      operator_id: "operator-id"
      aircraft_id: "aircraft-id"
```

The mapping is a bootstrap mechanism until the relay consumes the API-owned
aircraft assignment view. All normalized points use the stable
`aircraft_telemetry` measurement. Authenticated but unmapped agent points omit
the `aircraft_id` and `operator_id` fields and can be isolated in queries with
an `aircraft_id IS NULL` predicate.

`relay_id` is required when the `influxdb3` backend is enabled. The relay fails
startup rather than writing normalized records without deployment identity.
Omitting `max_retries` defaults backend writes to three retries; setting it to
`0` explicitly disables retries.

Set `backend: "noop"` only when normalized telemetry is deliberately disabled
for tests or local routing demonstrations.

The normalized path already accepts `GLOBAL_POSITION_INT`, `BATTERY_STATUS`,
`HEARTBEAT`, `SYS_STATUS`, `VFR_HUD`, `EXTENDED_SYS_STATE`, `GPS_RAW_INT`, and
`SYSTEM_TIME`. Each message is stored as an independent observation with its
own event time. API consumers must query the latest row per message group and
must not pretend that position, battery, and vehicle state were sampled
together.

### Data Sinks

> **Note:** v0.1 supports the following sinks: AWS S3, Google Cloud Storage, Apache Kafka, and Local File. Additional sinks may be available in future versions.

#### S3 Configuration

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

**Note:** If `access_key` and `secret_key` are empty, the sink will use the default AWS credential chain (IAM roles, environment variables, `~/.aws/credentials`).

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
      - "localhost:9093"
    topic: "telemetry-data"
    queue_size: 1000
    backpressure_policy: "drop"
```

#### File Configuration

```yaml
sinks:
  file:
    path: "/var/log/aero-arc-relay"
    prefix: "telemetry"
    format: "json"  # json, csv, binary
    rotation_interval: "24h"  # 24h, 1h, 30m, etc.
    queue_size: 1000
    backpressure_policy: "drop"
```

See `configs/config.yaml.example` for complete configuration examples.

### Logging

Structured logging with configurable levels:

```yaml
logging:
  level: "info"      # debug, info, warn, error
  format: "json"     # json, text
  output: "stdout"   # stdout, file
  file: "/var/log/aero-arc-relay/app.log"  # Optional: for file output
```
