## Production Deployment

### Graceful Shutdown

The relay supports graceful shutdown with context cancellation:
- Sinks have a 30-second timeout for cleanup
- HTTP server has a 10-second timeout for in-flight requests
- MAVLink connections are closed cleanly

### Container Considerations

- **Ports**: Expose UDP port 14550 for MAVLink and TCP port 2112 for metrics
- **Volumes**: Mount config file or use environment variables
- **Resources**: Minimal resource requirements; adjust based on message volume
- **Health Checks**: Use `/healthz` and `/readyz` endpoints

### gRPC Security Boundary

The current gRPC listener uses server TLS, which encrypts traffic and
authenticates the relay to clients. When Registry reporting is enabled, Agent
gateway registration and stream attachment additionally require a configured
per-Agent bearer credential, and the stream is bound to the random registration
session before liveness is published. Protect those credentials as deployment
secrets and rotate them per Agent.

The relay control service still has no workload authentication or authorization.
The agent gateway and relay control service also share one listener, so an
IP/port firewall cannot authorize individual control methods.

Operation-context mutation RPCs are disabled until the control service is moved
to a private listener restricted to the trusted API workload and protected by
mutual TLS or equivalent workload identity plus agent-level authorization. See
`kubernetes.md` for the intended deployment boundary.

### Agent Heartbeat Owner Rollout

Agent heartbeats now carry the Relay ID that owns the Agent placement. Roll out
this additive protobuf contract in this order:

1. Merge and publish the Protos revision containing
   `HeartbeatAgentRequest.relay_id`.
2. Deploy Relays built against that revision, and verify the entire Relay fleet
   is sending successful Agent heartbeats.
3. Deploy the Registry revision that requires `relay_id` and rejects a
   heartbeat when its Relay does not own the current Agent placement.

This order supports a mixed-version rollout because an older Registry ignores
the new protobuf field sent by an updated Relay. The reverse order is not
compatible: a strict Registry rejects the missing `relay_id` sent by an older
Relay, so those Agent placements eventually expire.

After the strict Registry is deployed, do not roll back Relay alone to a build
that omits the owner field. Roll back Registry owner enforcement first, or use a
coordinated rollback, then roll back Relay. Rolling back Registry while Relays
remain updated is wire-compatible because the older Registry ignores the
unknown field.

### WAL Generation Identity Rollout

Telemetry frames now carry the Agent WAL database's generation UUID. Roll out
this additive contract in this order:

1. Merge and publish the Protos revision containing `TelemetryFrame.wal_id`.
2. Deploy Agents that persist and send the WAL generation UUID, while the Relay
   fleet still accepts the additive field without depending on it.
3. After every Agent that may connect sends `wal_id`, deploy Relays that
   validate and persist it with normalized telemetry.

An upgraded Relay validates the WAL UUID at gateway admission before message
filtering or dispatch to normalized, no-op, or generic outputs. Missing or
malformed identities receive a permanent-error ACK; only a valid cursor can
receive `STATUS_OK` and be discarded from the Agent WAL.

Schema version 1 deliberately retains its deployed capture-time-based
`frame_id` formula throughout this rollout. If an upgraded Agent loses an ACK
from an old Relay and retries after connecting to an upgraded Relay, both
Relays therefore select the same InfluxDB point identity. Upgraded Relays store
`wal_id` as a field for cursor inspection and replay ordering; it does not alter
the version 1 point identity. Existing version 1 points and points written by
old Relays do not contain this field and remain valid. Record consumers must
treat a missing `wal_id` as legacy version 1 data, not as corruption.

Do not change the version 1 `frame_id` formula to include `wal_id`. A future
WAL-based formula requires a new normalized schema version and a coordinated
drain: stop admission, allow every Agent WAL entry accepted by the old Relay
fleet to receive its ACK, verify no in-flight version 1 retries remain, deploy
the new Relay fleet, and then resume admission. Without that drain, a retry can
be written once under each formula.
