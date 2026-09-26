# Durable C2 transport

`ExchangeCommand` is an authenticated API control RPC. It verifies canonical
identity, configured operator/aircraft-to-Agent mapping, execution capability,
and current session ownership before handing authority to the Agent stream.
Agent capabilities are advertised at registration and exposed in DroneStatus.

Relay receipt and stream dispatch are per-attempt events. Only correlated
Agent evidence establishes acknowledgement/application/observation. A Relay
response or network timeout alone establishes no aircraft execution outcome.
Stale telemetry stream bindings cannot answer a current exchange.

Relay owns no durable command state. API owns the acceptance/outbox ledger;
Agent owns its execution journal. A reconnect or lost response is recovered by
re-exchanging the same command UUID and digest. Command evidence travels over
the existing Agent stream to API workers. It is not normalized into the
`aircraft_telemetry` measurement or copied into the raw-telemetry archive.

Roll out the shared protocol module, compatible Relay, and capable Agent before
enabling API/Ops controls. New server definitions using existing numeric MAVLink
execution need no Relay schema addition or per-command RPC.

## Streaming execution evidence

`ExecuteCommand` is an additive server-streaming control RPC. It sends the
immutable command once and forwards cumulative Agent evidence through the
current authenticated stream. The original unary `ExchangeCommand` remains
available. A bounded latest-snapshot queue preserves completion under backpressure
because each snapshot includes all previous immutable events. Old Agent streams
cannot supply evidence for a replacement session.

The command admission slot is released on the first Agent response, while the
progress subscription remains active. Completion, Agent delivery-pass completion,
disconnect, or the bounded exchange deadline ends the subscription. Missing
outcomes must be recovered with the same authority, never inferred from EOF.
