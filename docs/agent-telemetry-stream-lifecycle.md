# Agent Telemetry Stream Lifecycle

This document describes how an Aero Arc agent registers with the relay, opens a
bidirectional telemetry stream, exchanges telemetry and control messages, replaces
an existing stream, and disconnects. It also defines the ownership and locking
rules that keep messages attached to the correct connection during replacement.

## Lifecycle at a Glance

```text
Agent                       Relay                         Outputs / Control API
  |                           |                                   |
  | Register(agent_id)        |                                   |
  |-------------------------->| create DroneSession + session ID  |
  |<--------------------------|                                   |
  |                           |                                   |
  | TelemetryStream metadata  |                                   |
  |-------------------------->| attach stream generation N        |
  |                           |                                   |
  | telemetry frame           |                                   |
  |-------------------------->| validate and route -------------->|
  |<--------------------------| ACK on the receiving stream        |
  |                           |                                   |
  |                           |<---------- operation command ------|
  |<--------------------------| send on current active stream      |
  | command ACK               |                                   |
  |-------------------------->| complete waiting control request  |
  |                           |                                   |
  | replacement stream        |                                   |
  |-------------------------->| attach generation N+1             |
  |                           |                                   |
  | old stream closes         | ignore stale cleanup              |
  | active stream closes      | remove active session             |
```

## 1. Registration

An agent first calls `Register` with a non-empty agent ID. The relay:

1. Generates a new opaque session ID.
2. Creates a `DroneSession` containing the agent identity, timestamps, stream
   state, operation-context state, and pending command map.
3. Stores the session in `grpcSessions`, keyed by agent ID.
4. Returns the agent ID, session ID, and advertised maximum in-flight frame
   count.

Registration does not open the telemetry stream. It creates the session that a
subsequent `TelemetryStream` call must attach to.

Registering the same agent ID again replaces the map entry with a new
`DroneSession` and a new session ID. The old stream handler can still be running,
but it no longer owns the active registered session.

## 2. Attaching a Telemetry Stream

The agent opens the bidirectional `TelemetryStream` RPC and supplies its agent ID
in the `aero-arc-agent-id` request metadata. The relay rejects calls with missing
metadata, missing agent ID, or no prior registration.

For an accepted call, `updateStream`:

1. Resolves the current `DroneSession` for the agent ID.
2. Serializes the attachment with sends targeting the active stream.
3. Increments the session's stream generation.
4. Stores the new stream as the session's active stream.
5. Returns the exact session pointer and generation to the stream handler.

The handler retains both values for its entire lifetime. They form the ownership
token used during cleanup.

Only one stream is designated as active, but a replaced handler is not forcibly
cancelled. During a handoff, the old and replacement handlers may therefore both
be alive briefly.

## 3. Receiving Telemetry

Each stream handler reads messages from its own RPC stream. For a telemetry frame,
the relay builds an ACK using the frame sequence number and validates:

- The frame agent ID matches the authenticated metadata agent ID.
- The frame session ID matches the currently registered session ID, when an
  active session exists.
- The MAVLink message name is not empty.

An invalid frame receives a permanent-error ACK and is not routed. An accepted
frame updates the session heartbeat, is converted into a telemetry envelope, and
is routed to configured outputs.

The ACK is then sent on the same stream from which the frame was read. This is a
response-bound operation: the sender of a frame must receive the corresponding
ACK even if another stream becomes active between `Recv` and `Send`.

All sends associated with a session use `sendMu`. gRPC permits one concurrent
reader and one concurrent writer for a stream, but multiple concurrent writers
must be serialized. The shared send lock prevents ACK and control-command sends
from calling `Send` concurrently.

## 4. Delivering Control Commands

The relay control API can send operation-context commands to an agent. Unlike a
telemetry ACK, a control command is not a response to a message received on a
particular stream. It should target whichever stream is currently active.

The delivery path:

1. Validates the agent ID and command ID.
2. Resolves the current registered session.
3. Adds a pending ACK channel keyed by command ID.
4. Calls `sendToAgent`, which selects the active stream while holding the session
   send lock.
5. Waits for the matching command ACK or for the caller's context to end.
6. Removes the pending entry when delivery fails, times out, is cancelled, or
   receives an ACK.

Incoming operation-context ACKs update the session's active flight and intent
state when applicable, then notify the pending control request.

This creates an intentional routing distinction:

| Message | Destination rule | Reason |
| --- | --- | --- |
| Telemetry ACK | Stream that received the frame | It answers a specific inbound message. |
| Control command | Current active stream | It targets the current agent connection. |

## 5. Replacing a Stream

A registered agent may open a replacement `TelemetryStream` before its old handler
exits. The replacement increments `streamGeneration` and becomes the active
stream.

After replacement:

- New control commands target the replacement stream.
- A frame already read, or subsequently read, by the old handler is ACKed on the
  old stream.
- Sends remain serialized through the session's shared send lock.
- Cleanup from the old generation cannot remove the replacement.

The generation is necessary because both streams can belong to the same
registered session and therefore have the same session ID. Session ID comparison
alone cannot distinguish an old connection from its replacement.

If the agent registers again instead of only replacing its stream, the new map
entry has a different `DroneSession` pointer and session ID. The old handler's
cleanup is rejected by the session pointer check. Frames from the old registration
also fail the active-session-ID validation and receive their error ACK on the old
stream.

## 6. Disconnect and Cleanup

The handler returns when it receives EOF, its context ends, receiving fails, or
sending an ACK fails. A deferred cleanup call then checks two conditions while
holding the session map lock:

1. The map still contains the exact `DroneSession` captured by this handler.
2. The captured stream generation is still the session's active generation.

The relay removes the session only when both conditions hold. Consequently:

- An old registration cannot delete a newly registered session.
- An old stream generation cannot delete a replacement stream in the same
  session.
- Closing the currently active stream removes its session from the active map.

Once the active session is removed, the agent must register again before opening
another telemetry stream.

## Synchronization and Ownership

The stream lifecycle uses four locks with separate responsibilities:

| Lock | Protects |
| --- | --- |
| `sessionsMu` | The `grpcSessions` map and session identity replacement. |
| `sessionMu` | Mutable session state, active stream, and stream generation. |
| `sendMu` | Serialized stream sends and active-stream replacement relative to sends. |
| `pendingMu` | The pending operation-command ACK map. |

The important ownership invariants are:

1. A handler always receives from its own stream argument.
2. A telemetry ACK is sent through that same stream argument.
3. An unsolicited command resolves the session's active stream.
4. A handler may clean up only the exact session and stream generation it owns.
5. Replacing an active stream is serialized with active-stream command sends.

These rules prevent a replacement connection from receiving an unrelated ACK and
prevent stale handler cleanup from tearing down the current connection.

## Current Handoff Semantics

Stream replacement changes which stream is active, but it does not cancel the old
handler. This allows a short drain period in which the old stream can finish work
and receive ACKs for its own frames. It also means both handlers can temporarily
submit valid telemetry for the same session.

If the protocol later requires strict single-stream ingestion, replacement should
also cancel the previous handler or cause stale generations to reject new frames.
That would be a separate policy change; the current generation mechanism provides
safe routing and cleanup without imposing that policy.

## Regression Coverage

`TestTelemetryStream_ReplacementKeepsACKAndCleanupOnReceivingStream` exercises the
replacement race by opening two streams for one registered session. It verifies
that:

- A frame read by the old stream is ACKed only on the old stream.
- Closing the old stream preserves the replacement session.
- Closing the active replacement stream removes the session.

The relay package is also run under Go's race detector to check the synchronization
paths used by stream attachment, sending, and cleanup.
