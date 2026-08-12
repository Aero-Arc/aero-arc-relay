# Telemetry Identity Authority

Status: accepted contract for the first normalized telemetry slice

## Purpose

This document defines which Aero Arc component creates and owns each identity
used by normalized telemetry. It is a platform contract implemented at the
relay boundary; the relay is not the source of truth for aircraft, operators,
flights, or operational intents.

The contract keeps the physical aircraft, its current agent installation, and
individual relay connections distinct. Those entities have different
lifecycles and must not share an identifier.

## Identity model

An aircraft is a durable Aero Arc record representing one physical airframe.
Its `aircraft_id` is an immutable, randomly generated Aero Arc UUID. The UUID
is associated with the physical airframe during operator-controlled enrollment
using real-world identifying information such as registration jurisdiction and
number, manufacturer, model, manufacturer serial number, and Remote ID serial.

The UUID is not derived from those attributes. Registration details can be
corrected or changed without changing the aircraft's identity or disconnecting
its telemetry, flight, maintenance, and evidence history.

An agent is a particular Aero Arc Agent installation on the aircraft's
companion computer. The agent's existing persisted identity remains the
`agent_id`. Reinstalling the agent or replacing the companion computer may
produce a new `agent_id`; that does not produce a new aircraft.

For the initial product model:

- One agent is assigned to at most one aircraft at a time.
- One aircraft has at most one active agent assignment at a time.
- One aircraft may have multiple agent assignments over its lifetime.
- Changing the agent, companion computer, registration, or other replaceable
  components does not change `aircraft_id`.
- Replacing the physical airframe creates a new aircraft record and UUID.

## Authority table

| Identity | Authority | Created when | Lifecycle and telemetry rule |
| --- | --- | --- | --- |
| `operator_id` | Aero Arc API durable store | An operator organization is provisioned | Stable tenant identity. The relay obtains it from the authoritative agent assignment and never infers it. |
| `aircraft_id` | Aero Arc API durable store | An authenticated operator enrolls a physical airframe | Immutable Aero Arc UUID. Registration, tail number, serial number, Remote ID, MAVLink system ID, and agent ID are not substitutes. |
| `agent_id` | Aero Arc Agent | The agent first initializes its persisted identity | Identifies an installation, not an aircraft. It is sent during registration and on frames and remains stable across WAL retries. |
| `relay_id` | Relay deployment identity | A relay instance or logical relay is provisioned | Identifies the relay that accepted the frame. It must be configured or durably generated; it is not derived from network location. |
| `session_id` | Relay | Successful agent registration | Identifies one accepted registration/connection lifecycle. A new registration receives a new opaque ID. It is not the `agent_id`. |
| `flight_id` | Aero Arc API flight workflow | A flight record is explicitly created | Optional until assigned by the authoritative flight workflow. The normalizer must not infer a flight from arming, takeoff, or MAVLink state. |
| `intent_id` | Aero Arc API operational-intent workflow | An operational intent is created | Optional telemetry context obtained through the aircraft/flight association. The normalizer must not infer it. |
| `frame_id` | Telemetry ingestion contract | A frame is written to the agent WAL | For the first slice, the stable idempotency key is `agent_id` plus the durable WAL sequence. It must remain unchanged across resend and reconnect. |

## Aircraft enrollment and agent assignment

Before attributed telemetry is expected, an authenticated operator creates an
aircraft record and supplies the available physical and legal identifiers. The
API generates `aircraft_id` and stores the aircraft independently of any agent.

The operator then assigns the detected or supplied `agent_id` to the aircraft.
The durable store owns this association:

```text
operator_id + aircraft_id + agent_id + effective assignment period
```

The assignment should retain effective start and end times so historical
telemetry remains attributable after an agent is replaced. Reassignment must
end the previous active association rather than rewriting its history.

The first implementation may expose this association to the relay through
static configuration. That is a bootstrap mechanism, not a second source of
truth. The intended production resolver reads an API- or registry-published
view of the API-owned assignment.

## Registration and session binding

When an agent registers, the relay authenticates the presented `agent_id`,
resolves its current aircraft assignment, creates a fresh `session_id`, and
binds the following immutable context to that session:

```text
agent_id
operator_id, when assigned
aircraft_id, when assigned
relay_id
session_id
```

Frames on the stream are evaluated against the bound session. A frame-provided
identity must not override the server-resolved operator or aircraft identity.
Flight and intent context may be added from an authoritative assignment, but
must never be guessed from MAVLink fields.

## Unassigned agents

A supported MAVLink message from an authenticated but unassigned agent is
still normalized. Missing identity is explicit:

- `agent_id`, `relay_id`, and `session_id` are populated.
- `operator_id` and `aircraft_id` remain absent.
- The relay does not manufacture an aircraft record or infer identity from
  MAVLink system ID, component ID, position, registration-like text, or other
  telemetry fields.
- The normalized record uses the same retry-stable measurement as attributed
  telemetry but omits `aircraft_id` and `operator_id`; queries isolate it with
  an `aircraft_id IS NULL` predicate.
- Generic forwarding continues according to its existing routing policy.
- The relay reports the unassigned condition with bounded-cardinality metrics
  and rate-limited logs.

After assignment, new telemetry is attributed through the newly resolved
session. Historical backfill of unassigned records is a separate workflow and
is not required by the first slice.

## Trust boundary

The first slice assumes an authenticated, non-malicious operator correctly
enrolls the physical airframe and assigns its installed agent. The association
is an authoritative business record; MAVLink telemetry alone cannot prove the
physical airframe's legal identity.

Registry verification, enrollment approval, device certificates, signed
telemetry, and hardware-backed attestation may strengthen this trust model in
later work. They do not change the identifiers or ownership rules above.

## Prohibited inferences

Implementations must not:

- Use `agent_id` as `aircraft_id`.
- Use MAVLink system or component IDs as globally unique aircraft identities.
- Derive `aircraft_id` from registration, serial number, or a combination of
  mutable aircraft attributes.
- Treat a relay connection or process lifetime as a flight.
- Infer `flight_id` or `intent_id` from vehicle state.
- Replace server-resolved identity with values supplied by an individual
  telemetry frame.

## Current implementation gaps

The Relay authenticates Registry-visible Agent sessions with per-Agent bearer
credentials, returns a cryptographically random session ID, requires stream
setup and every frame to carry it, and reports connected Agent and Relay
liveness to the registry.
The current assignment resolver is still the static
`telemetry.agent_mappings` bootstrap configuration; it does not yet consume an
API-owned assignment view. Operation-context mutation is also disabled until
the Relay control plane has workload authentication and authorization, so
`flight_id` and `intent_id` remain absent unless an authoritative context was
already established.

The exact assignment delivery mechanism, unassigned-data retention period, and
replacement for the bootstrap token mechanism remain operational design
decisions.
They do not block the identity model or normalized record contract.
