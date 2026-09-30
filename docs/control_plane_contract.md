# Relay Control-Plane Contract

This document describes the relay’s **control-plane responsibilities and guarantees**, based on the **currently implemented behavior** in this repository.

It is intended for **API/backend developers** integrating with the relay (e.g. `aeroarc-api`) so they can consume it **without guessing**.

## Scope

The relay exposes two control-plane surfaces:

- **gRPC AgentGateway** (`aeroarc.agent.v1.AgentGateway`): agent registration + bidirectional telemetry streaming.
- **gRPC RelayControl** (`aeroarc.relay.v1.RelayControl`): fleet/session status APIs (**registered**, but currently **stubbed** / empty responses in code).

The relay also optionally publishes session routing metadata to **Redis** for topology/routing lookups.

## Endpoints

- **gRPC**: `0.0.0.0:${relay.grpc_port}` (default `50051`)
- **HTTP metrics**: `:2112/metrics`
- **Health**: `:2112/healthz`
- **Ready**: `:2112/readyz`

## Data ownership rules

- **Relay is the source of truth** for **in-memory session state** during process lifetime.
- Session state is keyed by **Agent ID** (`agent_id`) in the current implementation.
- The relay does **not** guarantee persistence of session state across restarts.
- Redis (if enabled) is used only for **best-effort publication** of routing metadata; Redis is **not** authoritative for session truth.

## Supported RPCs

### AgentGateway.Register

**RPC**: `Register(RegisterRequest) returns (RegisterResponse)`

**Purpose**: Establishes an agent session in relay memory and returns a server-issued `session_id`.

**Current behavior**
- Creates/overwrites an in-memory session record keyed by `RegisterRequest.agent_id`.
- Returns:
  - `RegisterResponse.agent_id` = request `agent_id`
  - `RegisterResponse.session_id` = currently `"sess-" + agent_id` (placeholder; not cryptographically random)
  - `RegisterResponse.max_inflight` = currently a fixed default in code

**Failure semantics**
- If required fields are missing, the relay may return a gRPC error (implementation-specific).

**Side effects**
- If Redis routing publication is enabled, the relay writes the mapping described in [Redis usage guarantees](#redis-usage-guarantees).

### AgentGateway.TelemetryStream

**RPC**: `TelemetryStream(stream TelemetryFrame) returns (stream TelemetryAck)`

**Purpose**: Bidirectional streaming telemetry ingest; relay acks each received frame and forwards it to sinks.

**Request requirements**
- The incoming gRPC metadata **must** include: `aero-arc-agent-id: <agent_id>`
- The agent should call `Register()` first so a session exists in relay memory.

**Current behavior**
- On stream start, the relay associates the server stream with the in-memory session keyed by `aero-arc-agent-id`.
- For each received `TelemetryFrame`:
  - Converts it into a `TelemetryEnvelope`
  - Forwards it to all configured sinks (implementation may be synchronous per sink)
  - Sends a `TelemetryAck` (currently per-frame ack)

**Failure semantics**
- Missing metadata → `InvalidArgument`
- Missing `aero-arc-agent-id` header → `InvalidArgument`
- Unregistered agent/session → the stream returns an error (exact code depends on implementation path; callers should treat this as “must register first”)
- Stream `io.EOF` → treated as clean client close

**Disconnect semantics**
- On clean disconnect, the relay ends the stream.
- If Redis routing publication is enabled, the relay best-effort removes the routing mapping (see [Redis usage guarantees](#redis-usage-guarantees)).

### RelayControl.ListActiveDrones

**RPC**: `ListActiveDrones(ListActiveDronesRequest) returns (ListActiveDronesResponse)`

**Current behavior**
- RPC is **registered** on the gRPC server.
- Implementation is currently a **stub** and returns an empty response.

### RelayControl.GetDroneStatus

**RPC**: `GetDroneStatus(GetDroneStatusRequest) returns (GetDroneStatusResponse)`

**Current behavior**
- RPC is **registered** on the gRPC server.
- Implementation is currently a **stub** and returns an empty response.

## Failure semantics (general)

- **No hard dependency on Redis**: Redis failures should not prevent the relay from starting or serving gRPC.
- **Crash/restart**: in-memory session state is lost; any routing metadata relies on TTL expiry in Redis.

## Redis usage guarantees

Redis is optional and is enabled only if `REDIS_ADDR` is set.

### Redis connection configuration

Environment variables:

- `REDIS_ADDR`: host:port (enables Redis)
- `REDIS_PASSWORD`: optional
- `REDIS_DB`: optional integer DB index

Startup:
- Relay attempts a short `PING` on startup and logs warnings on failure.
- Relay continues running even if Redis is unreachable.

### Routing metadata publication

When an agent session is active, the relay publishes:

**Key**

- `drone:{drone_id}`
- In the current implementation, `drone_id` == `agent_id`

**Value**

JSON payload:

```json
{
  "relay_id": "…",
  "session_id": "…"
}
```

`relay_id` selection:
- `RELAY_ID` env var if set, else hostname, else `"relay"`

**TTL**
- TTL-based, defaulting to **45s**.

**Refresh**
- While the session is active, the relay refreshes the TTL periodically (currently every `TTL/2`).

**Disconnect + crash**
- On clean disconnect, the relay best-effort deletes the key.
- On crash/restart, refresh stops and the key should expire via TTL.

**Guarantee level**
- Publication is **best-effort**. Consumers must tolerate missing/late updates and should treat Redis as a cache of routing hints, not an authoritative source of truth.


