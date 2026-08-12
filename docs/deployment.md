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
