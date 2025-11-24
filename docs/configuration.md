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