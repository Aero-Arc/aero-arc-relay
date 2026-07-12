# Supported MAVLink Messages

The agent generically extracts and forwards the MAVLink common dialect. The
official Aero Arc normalized telemetry path intentionally supports a smaller
product contract:

- `HEARTBEAT`
- `GLOBAL_POSITION_INT`
- `BATTERY_STATUS`
- `SYS_STATUS`
- `VFR_HUD`
- `EXTENDED_SYS_STATE`
- `GPS_RAW_INT`
- `SYSTEM_TIME`

See `telemetry-normalization-fields-v1.md` for fields, units, conversions, and
sentinel rules. Messages outside this set remain eligible for generic sinks but
do not produce normalized telemetry records.

`ATTITUDE` is deferred until an API or UI feature consumes it.
