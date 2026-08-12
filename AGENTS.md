# Relay Development Context

## Service boundaries

- Relay owns authenticated Agent sessions, telemetry admission, normalization,
  and delivery to hot/archive outputs.
- InfluxDB measurement `aircraft_telemetry` stores independent normalized
  MAVLink observations; it is not a synchronized aircraft snapshot.
- Registry owns ephemeral Relay/Agent liveness and placement only. Never put
  position, battery, or historical telemetry in registry records.
- The API owns durable aircraft/operator identity and operation intent/flight
  context. Do not infer these identities from MAVLink values.

## Normalized telemetry contract

- `frame_id`, `agent_id`, `message_name`, and `schema_version` are tags.
- Assignment/session/operation metadata are fields because they can change
  across retries or sessions.
- Event time normally comes from the durable Agent capture timestamp.
- Supported normalized groups are documented in
  `docs/telemetry-normalization-fields-v1.md`.
- Preserve independent timestamps when adding fields or consumers.

## Registry reporting

- Treat the Registry reporter as an explicit control-plane lifecycle service,
  not a telemetry Router consumer. Relay/Agent registration and background
  heartbeats are driven by startup and stream lifecycle events.
- Registry-enabled startup must register the Relay before accepting Agents.
- The Agent registration handshake creates a pending local session; only an
  active telemetry stream is published as connected in the registry.
- Agent registry publication must succeed before its telemetry stream is
  accepted, and idle active streams receive background heartbeats.
- Every Agent heartbeat includes the configured Relay ID. Registry uses that
  owner identity to reject a stale Relay trying to renew another Relay's
  current Agent placement.
- Preserve rollout compatibility for heartbeat owner enforcement: publish
  Protos, deploy all Relays, then deploy strict Registry validation. Before
  rolling Relay back to a version without the owner field, roll Registry
  validation back first or perform a coordinated rollback.
- Authenticate the claimed Agent ID and bind the registration session before
  publishing it. Keep per-Agent credentials in environment-backed secrets.
- Schedule idle Agent heartbeats independently; one slow Registry RPC must not
  delay renewal for unrelated active streams.
- Heartbeats are throttled and retry after TTL expiry or registry restart.
- Registry failures must not make successfully admitted telemetry retry unless
  the official telemetry writer itself failed.

## Validation

- Run `gofmt` on changed Go files.
- Run `go test ./...` for unit coverage.
- Run `go test -race ./...` for concurrency changes.
- Run `go test -tags=integration -timeout=10m ./internal/integration ./internal/registryreporter`
  when Docker and local sockets are available.
