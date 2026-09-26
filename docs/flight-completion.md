# Durable flight completion notifications

Configure `completion_outbox_path` on persistent local disk to enable completion
admission. Registration advertises the capability; upgraded Agents retain events
when connected to an older or unconfigured Relay.

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
