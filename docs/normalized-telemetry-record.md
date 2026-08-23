# Normalized Telemetry Record Contract

Status: implemented contract for the first normalized telemetry slice

## Purpose

A normalized telemetry record is Aero Arc's internal, storage-neutral
representation of one supported MAVLink message after parsing, validation,
scaling, sentinel handling, and identity enrichment.

The record exists only on the internal telemetry path. It does not replace the
agent `TelemetryFrame`, the relay's generic `TelemetryEnvelope`, or the payload
sent to generic exporters.

One input envelope produces either zero or one normalized record:

- A supported, valid message produces one record containing only the fields
  defined for that message.
- An unsupported message produces no record and continues through generic
  routing normally.
- A supported message that cannot satisfy its required-field contract produces
  no record and a categorized normalization failure.

The record is not an aircraft-state snapshot. Position, battery, HUD, system
health, and GPS messages produce separate records with independent timestamps.
The API may compose the latest records into a current-aircraft read model.

## Logical shape

The first version has the following logical structure:

```go
type Record struct {
	SchemaVersion uint16

	Identity IdentityContext
	Source   SourceContext
	Timing   TimingContext

	MessageName string
	Fields      Fields
}

type IdentityContext struct {
	OperatorID string
	AircraftID string
	AgentID    string
	RelayID    string
	SessionID  string
	FlightID   string
	IntentID   string
}

type SourceContext struct {
	FrameID     string
	Sequence    uint64
	MessageID   uint32
	Dialect     string
	SystemID    *uint8
	ComponentID *uint8
}

type TimingContext struct {
	EventTime       time.Time
	RelayTime       time.Time
	AgentCaptureTime *time.Time
	DeviceTime      *DeviceTime
	TimestampSource TimestampSource
}

type DeviceTime struct {
	Value uint64
	Unit  DeviceTimeUnit
	Basis DeviceTimeBasis
}
```

This is a logical contract. The implementation may flatten the structures if
that materially simplifies storage mapping, but it must preserve the same
semantics.

## Record metadata

### Schema version

`schema_version` is required and begins at `1`.

The version applies to normalized field names, types, units, and semantics.
Backward-compatible optional field additions may remain in version 1. A field
rename, type change, unit change, or semantic reinterpretation requires a new
version or an explicit migration.

The schema version does not track application releases or MAVLink protocol
versions.

### Identity

Identity authority and assignment are defined in
`telemetry-identity-authority.md`.

Required identity for every record:

- `agent_id`
- `relay_id`
- `session_id`

Attribution fields are present when the agent has an authoritative assignment:

- `operator_id`
- `aircraft_id`

Operational context is optional and must come from an authoritative workflow:

- `flight_id`
- `intent_id`

Empty strings represent missing optional identity in memory. A storage backend
must omit missing optional values rather than writing empty-string tags or
invented placeholders.

### Source context

`frame_id` is the record's required idempotency key. Version 1 uses the
unambiguous encoding of:

```text
agent_id + WAL generation ID + durable agent WAL sequence
```

The concrete encoding must be stable, documented, and collision-safe. A
length-delimited or escaped encoding is required; raw concatenation is not.
Resending the same WAL entry produces the same `frame_id`. The WAL generation
ID is a UUID stored in the Agent's WAL database: it survives process restarts
and changes when the database is recreated. It therefore prevents a reset WAL
sequence from reusing an idempotency key. A future explicit frame UUID may
replace this derived encoding in a new transport contract.

`wal_id` is the Agent WAL generation identity. It is required and must not be
derived from a process, Relay session, SQLite row ID, or MAVLink packet.

`sequence` is the agent WAL sequence carried by the frame. It is required and
must not be replaced with the MAVLink packet sequence.

`message_id` is the numeric MAVLink message ID and is required.

`message_name` is the canonical Aero Arc message name and is required. Version
1 canonical names are lowercase snake case, for example:

```text
global_position_int
battery_status
heartbeat
```

Go type names, pointer markers, package paths, and casing variations from an
agent are normalized before registry lookup. The canonical name stored in the
record must not depend on the Go library's rendered type name.

`dialect` is required and uses a canonical lowercase name such as `common`.

`system_id` and `component_id` preserve MAVLink transport identity when the
agent supplies them. They are source metadata, not Aero Arc aircraft identity.
They are optional in version 1 because the current agent frame contract does
not populate them reliably.

The generic raw frame and generic string-valued fields are not copied into the
normalized record. Generic routing remains responsible for source-preserving
delivery and archival.

## Time contract

`event_time` is required and is the timestamp used for ordering and storage.
`timestamp_source` is required and explains how it was chosen.

Version 1 timestamp-source values are:

```text
device_utc
agent_capture
relay_receive
```

Selection order is:

1. A validated device UTC time when a reliable device-to-UTC mapping exists.
2. The agent capture time from `sent_at_unix_ns` when it is valid.
3. Relay receive time.

The first slice normally selects `agent_capture`. A boot-relative timestamp is
never interpreted directly as Unix time.

The current agent transport requires a positive `sent_at_unix_ns` on every WAL
frame so event-time evaluation and replay retain the durable capture instant.
Frames without it are rejected permanently before normalization; relay receive
time is not substituted for missing capture evidence.

`relay_time` is always required and records when the relay accepted the source
frame.

`agent_capture_time` is retained when `sent_at_unix_ns` is positive and can be
represented as a valid timestamp, even when another source becomes the event
time.

`device_time` is optional structured metadata. Its basis is explicit:

```text
unix_epoch
system_boot
unknown
```

Its unit is explicit, initially `milliseconds` or `microseconds`. Normalizers
must not guess a timestamp basis unless the MAVLink field contract defines a
safe discriminator. `SYSTEM_TIME` may later establish a mapping between boot
time and UTC; that mapping policy is separate from the record shape.

## Normalized fields

`fields` contains only fields allowed by the contract for `message_name` and
`schema_version`.

Allowed value types are:

- signed 64-bit integer
- unsigned 64-bit integer
- finite 64-bit floating point number
- boolean
- UTF-8 string

Arrays, nested objects, arbitrary byte strings, NaN, positive infinity, and
negative infinity are not valid normalized field values in version 1. A future
message that requires an array must define a controlled extension or expand it
into documented scalar fields.

Implementations should use a constrained `Value` representation or validate a
`map[string]any` at the record boundary. Backend-specific types must not appear
in the record.

Normalized field names use lowercase snake case and include a unit suffix when
the value represents a physical quantity, for example:

```text
latitude_deg
altitude_msl_m
groundspeed_mps
battery_voltage_v
battery_remaining_pct
device_boot_time_ms
```

Unitless counts, identifiers, booleans, enum names, and bitmasks do not require
a unit suffix.

Enum fields use stable lowercase snake-case names when the value is intended
for product display or filtering. A numeric enum code may also be retained in
a separately documented field when lossless source interpretation is useful.
Unknown enum codes must not be silently mapped to a known name.

Bitmasks remain unsigned integers. Expanding individual bits into booleans is
allowed only when the message contract defines stable field names for them.

## Missing, invalid, and partial values

Missing, unsupported, malformed, sentinel, NaN, and infinite optional values
are omitted from `fields`. They are not stored as zero, empty strings, magic
numbers, or textual `null` values.

A legitimate zero is retained. Examples include zero groundspeed, zero degrees
heading, and zero battery percentage.

Each message contract defines its required fields. If any required field is
missing, malformed, outside a hard validity range, or carries an invalid
sentinel, normalization fails for the whole record.

An invalid optional field does not discard otherwise valid data. The field is
omitted and the normalizer records a bounded reason-category metric. Raw field
values must not appear in metric labels.

Soft expected ranges may produce diagnostics without rejecting a value. Hard
validity ranges and soft expected ranges must be distinguished in the
message-level contract.

## Invariants

Every normalized record must satisfy all of the following:

1. It represents exactly one input telemetry frame and one canonical MAVLink
   message.
2. `schema_version`, `agent_id`, `relay_id`, `session_id`, `frame_id`,
   `wal_id`, `sequence`, `message_id`, `message_name`, `dialect`, `event_time`,
   `relay_time`, and `timestamp_source` are present and valid.
3. `frame_id` remains stable across retry and reconnect and does not collide
   after WAL recreation.
4. Operator, aircraft, flight, and intent identities are authoritative or
   absent; they are never inferred from MAVLink fields.
5. Fields are approved for the record's message and schema version.
6. Field values use approved scalar types and documented units.
7. Invalid sentinel values are not exposed as measurements.
8. The record contains no generic raw payload and no storage-specific point,
   tag, bucket, measurement, or client type.
9. The normalizer is deterministic for the same envelope and bound identity
   and timing context.

## Backend responsibilities

A backend maps a valid record into its storage representation. It may choose
tags, measurements, indexes, batching, and retry behavior, but it must not:

- Reparse generic MAVLink fields.
- Change normalized units or meanings.
- Convert invalid values into defaults.
- Infer missing identity.
- Rename fields independently of the schema contract.

The InfluxDB backend is therefore a `Record`-to-point adapter. A memory backend
stores the same logical records for tests and local use.

For InfluxDB, measurement, tag set, and timestamp form the point identity. All
records therefore use the stable `aircraft_telemetry` measurement, and only
frame-invariant values (`frame_id`, `agent_id`, `message_name`, and
`schema_version`) are tags. This keeps retries idempotent while ensuring frames
with the same capture timestamp and different WAL cursors remain distinct.
Retry-variable relay, session, assignment, flight, and intent metadata remain
queryable fields. `wal_id` and `wal_sequence` remain fields for replay ordering,
inspection, and diagnostics.

## API relationship

Normalized records are the write contract, not necessarily the API response
contract.

- Historical track samples primarily query `global_position_int` records.
- Latest telemetry composes the newest applicable records by message group and
  preserves their independent freshness timestamps.
- Live connection state is owned by the registry path and may use heartbeat
  observations without treating every heartbeat as a position sample.

The API must not assume that fields from independently emitted MAVLink
messages were observed at the same instant.

## Deferred decisions

The following are intentionally outside this contract and are specified by
later Phase 1 documents:

- The exact fields and required-field rules for each supported message.
- Queue capacity, overflow, batching, retry, and shutdown policies.
- InfluxDB measurement, tag, field, and retention design.
- How a production relay receives the API-owned agent assignment.
- Precise `SYSTEM_TIME` and `TIMESYNC` clock-correlation behavior.
