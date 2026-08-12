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
