# Durable flight completion notifications

Completion admission requires all four prerequisites:

1. `completion_outbox_path` on persistent local disk.
2. `agent_auth.tokens` containing the registering Agent's ID and secret token.
3. Enabled control authentication: `control_auth.enabled: true`, a readable
   client CA, an allow-listed API identity, and the Relay listener's TLS
   certificate/key (`--tls-cert-path`, `--tls-key-path`).
4. A nonempty `telemetry.agent_mappings[agent-id].aircraft_id` for that Agent.
   This identity mapping is required even when telemetry sinks are disabled.

The checked-in `configs/config.yaml.example` deliberately disables completion;
it is a minimal telemetry example, not a completion-ready configuration.
A completion-enabled configuration includes the following values, with the
same real Agent ID in both maps and environment-provided credentials:

```yaml
completion_outbox_path: "./data/flight-completions.db"
agent_auth:
  tokens:
    "replace-with-agent-id": "${AERO_ARC_AGENT_TOKEN}"
control_auth:
  enabled: true
  client_ca_file: "${AERO_ARC_CONTROL_CLIENT_CA_FILE}"
  allowed_identities:
    - "spiffe://aero-arc/api"
telemetry:
  agent_mappings:
    "replace-with-agent-id":
      operator_id: "replace-with-operator-id"
      aircraft_id: "replace-with-aircraft-id"
```

Merge these values into the deployment's complete configuration and supply TLS
files at startup. A database file alone does not enable the feature. Verify
`RegisterResponse.durable_flight_completion` is true for the authenticated Agent;
otherwise upgraded Agents retain completion events for later delivery.

An authenticated, current Agent stream can submit historical completion evidence
for its configured aircraft. Flight context may already have changed during an
offline interval, so the API validates the event against immutable command and
flight records. Replaced streams cannot obtain new admission authority.

Relay commits the exact event bytes and digest to SQLite FULL/WAL storage before
returning a receipt. The API uses authenticated `ListFlightCompletions` and
`AckFlightCompletions` RPCs. Matching receipts retire pending delivery, while rows
remain as immutable deduplication tombstones. Restart and duplicate delivery do
not create new flight identities. Preserve this database during upgrades.

This queue is separate from telemetry admission and dispatch. It is not replicated
storage: recovery of a lost Relay disk is outside its durability guarantee.
Unacknowledged API obligations remain on that Relay until it rejoins discovery.
