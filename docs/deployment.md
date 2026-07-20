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
authenticates the relay to clients but does not authenticate clients to the
relay. The agent gateway and relay control service also share that listener, so
an IP/port firewall cannot authorize individual control methods.

Operation-context mutation RPCs are disabled until the control service is moved
to a private listener restricted to the trusted API workload and protected by
mutual TLS or equivalent workload identity plus agent-level authorization. See
`kubernetes.md` for the intended deployment boundary.
