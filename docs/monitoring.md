## Monitoring

### Metrics Endpoint

Prometheus metrics are exposed at `http://localhost:2112/metrics`:

**Key Metrics:**
- `aero_relay_messages_total{source,msg_name}` - Total messages processed
- `aero_relay_sink_errors_total{sink}` - Sink write errors
- `aero_sink_queue_length{sink}` - Current queue depth
- `aero_sink_enqueued_total{sink}` - Messages enqueued
- `aero_sink_dropped_total{sink}` - Messages dropped (backpressure)

### Health Endpoints

- **`/healthz`** - Liveness probe (always 200 if process is running)
- **`/readyz`** - Readiness probe (200 once sinks are initialized)