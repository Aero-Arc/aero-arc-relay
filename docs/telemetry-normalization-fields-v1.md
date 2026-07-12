# Telemetry Normalization Fields v1

Status: implemented relay contract for the first normalized telemetry slice

This document defines the selected MAVLink common-message fields that may enter
the Aero Arc normalized telemetry path. Generic routing remains available for
all messages extracted by the agent.

The common record metadata, type restrictions, missing-value behavior, and
timestamp policy are defined in `normalized-telemetry-record.md`.

## Selected messages

The first API vertical slice uses `GLOBAL_POSITION_INT`, `BATTERY_STATUS`, and
`HEARTBEAT`. The next operational set adds `SYS_STATUS`, `VFR_HUD`,
`EXTENDED_SYS_STATE`, `GPS_RAW_INT`, and `SYSTEM_TIME`.

## GLOBAL_POSITION_INT (33)

`Lat` and `Lon` are required. All other fields are optional.

| Source field | Normalized field | Type | Conversion and invalid behavior |
| --- | --- | --- | --- |
| `TimeBootMs` | `device_boot_time_ms` | uint64 | Milliseconds since boot |
| `Lat` | `latitude_deg` | float64 | Divide degE7 by 1e7; valid -90 through 90 |
| `Lon` | `longitude_deg` | float64 | Divide degE7 by 1e7; valid -180 through 180 |
| `Alt` | `altitude_msl_m` | float64 | Divide millimeters by 1,000 |
| `RelativeAlt` | `relative_altitude_m` | float64 | Divide millimeters above home by 1,000; this is not labeled AGL |
| `Vx` | `velocity_north_mps` | float64 | Divide cm/s by 100 |
| `Vy` | `velocity_east_mps` | float64 | Divide cm/s by 100 |
| `Vz` | `velocity_down_mps` | float64 | Divide cm/s by 100; positive is down |
| `Vx`, `Vy` | `groundspeed_mps` | float64 | `hypot(Vx, Vy) / 100`; requires both components |
| `Hdg` | `heading_deg` | float64 | Divide centidegrees by 100; omit `UINT16_MAX` |

## BATTERY_STATUS (147)

`Id` is required and identifies the battery instance within the MAVLink
component. All measurements are optional.

| Source field | Normalized field | Type | Conversion and invalid behavior |
| --- | --- | --- | --- |
| `Id` | `battery_id` | uint64 | Valid 0 through 255 |
| `BatteryFunction` | `battery_function` | string | Lowercase enum name |
| `Type` | `battery_type` | string | Lowercase enum name |
| `Temperature` | `battery_temperature_c` | float64 | Divide cdegC by 100; omit `INT16_MAX` |
| `Voltages`, `VoltagesExt` | `battery_voltage_v` | float64 | Sum supported millivolt entries and divide by 1,000; omit base `UINT16_MAX` and extended zero entries |
| `CurrentBattery` | `battery_current_a` | float64 | Divide cA by 100; omit `-1` |
| `CurrentConsumed` | `battery_consumed_mah` | int64 | Preserve mAh; omit `-1` |
| `EnergyConsumed` | `battery_consumed_wh` | float64 | Convert hectojoules to Wh by dividing by 36; omit `-1` |
| `BatteryRemaining` | `battery_remaining_pct` | float64 | Preserve 0 through 100; omit `-1` and out-of-range values |
| `TimeRemaining` | `battery_time_remaining_s` | int64 | Preserve positive seconds; omit zero |
| `ChargeState` | `battery_charge_state` | string | Lowercase enum name |
| `Mode` | `battery_mode` | string | Lowercase enum name |
| `FaultBitmask` | `battery_fault_bits` | uint64 | Deferred until the agent sends a numeric companion value |

The voltage calculation follows MAVLink's aggregate-voltage encoding as well
as ordinary cell arrays. A record does not claim that each array entry is an
individually measured cell voltage.

## HEARTBEAT (0)

No individual source field is required beyond a valid extracted heartbeat.

| Source field | Normalized field | Type | Behavior |
| --- | --- | --- | --- |
| `Type` | `vehicle_type` | string | Lowercase enum name |
| `Autopilot` | `autopilot_type` | string | Lowercase enum name |
| `BaseMode` | `base_mode` | string | Lowercase rendered flag names |
| `CustomMode` | `custom_mode` | uint64 | Preserve autopilot-specific value |
| `SystemStatus` | `system_status` | string | Lowercase enum name |
| `MavlinkVersion` | `mavlink_version` | uint64 | Preserve protocol version |

Numeric `base_mode_bits` and derived `armed` are deferred until the agent sends
the numeric bitmask. Heartbeat observations primarily feed live state; they are
not position samples.

## SYS_STATUS (1)

All fields are optional.

| Source field | Normalized field | Type | Conversion and invalid behavior |
| --- | --- | --- | --- |
| `Load` | `mainloop_load_pct` | float64 | Divide decipercent by 10; omit above 1000 |
| `DropRateComm` | `communication_drop_rate_pct` | float64 | Divide centipercent by 100 |
| `ErrorsComm` | `communication_error_count` | uint64 | Preserve count |
| `ErrorsCount1..4` | `autopilot_error_count_1..4` | uint64 | Preserve autopilot-specific counts |
| Sensor status strings | `sensors_present`, `sensors_enabled`, `sensors_health` | string | Lowercase rendered flags |
| Extended sensor strings | Corresponding `_extended` fields | string | Lowercase rendered flags |

Numeric sensor bit fields are deferred until the agent exposes numeric
companions. Battery fields in `SYS_STATUS` are not normalized because MAVLink
defines them as ambiguous on multi-battery systems; `BATTERY_STATUS` is the
product source.

## VFR_HUD (74)

All fields are optional and already use product units.

| Source field | Normalized field | Type | Invalid behavior |
| --- | --- | --- | --- |
| `Airspeed` | `airspeed_mps` | float64 | Omit non-finite values |
| `Groundspeed` | `groundspeed_mps` | float64 | Omit non-finite values |
| `Heading` | `heading_deg` | float64 | Preserve 0 through 360 |
| `Throttle` | `throttle_pct` | float64 | Preserve 0 through 100 |
| `Alt` | `altitude_msl_m` | float64 | Omit non-finite values |
| `Climb` | `climb_rate_mps` | float64 | Omit non-finite values |

## EXTENDED_SYS_STATE (245)

Both fields are optional.

| Source field | Normalized field | Type | Behavior |
| --- | --- | --- | --- |
| `VtolState` | `vtol_state` | string | Lowercase enum name |
| `LandedState` | `landed_state` | string | Lowercase enum name |

These observations do not independently create or close a flight record.

## GPS_RAW_INT (24)

All fields are optional. GPS-derived fields retain a `gps_` prefix so they are
not confused with the filtered vehicle estimate from `GLOBAL_POSITION_INT`.

| Source field | Normalized field | Type | Conversion and invalid behavior |
| --- | --- | --- | --- |
| `TimeUsec` | `device_time_usec` | uint64 | Preserve; basis remains unknown until classified |
| `FixType` | `gps_fix_type` | string | Lowercase enum name |
| `Lat` | `gps_latitude_deg` | float64 | Divide degE7 by 1e7; enforce latitude range |
| `Lon` | `gps_longitude_deg` | float64 | Divide degE7 by 1e7; enforce longitude range |
| `Alt` | `gps_altitude_msl_m` | float64 | Divide millimeters by 1,000 |
| `AltEllipsoid` | `gps_altitude_ellipsoid_m` | float64 | Divide millimeters by 1,000 |
| `Eph` | `gps_hdop` | float64 | Divide by 100; omit `UINT16_MAX` |
| `Epv` | `gps_vdop` | float64 | Divide by 100; omit `UINT16_MAX` |
| `Vel` | `gps_groundspeed_mps` | float64 | Divide cm/s by 100; omit `UINT16_MAX` |
| `Cog` | `gps_course_over_ground_deg` | float64 | Divide centidegrees by 100; omit `UINT16_MAX` |
| `SatellitesVisible` | `gps_satellites_visible` | uint64 | Omit `UINT8_MAX` |
| `HAcc` | `gps_horizontal_accuracy_m` | float64 | Divide millimeters by 1,000 |
| `VAcc` | `gps_vertical_accuracy_m` | float64 | Divide millimeters by 1,000 |
| `VelAcc` | `gps_speed_accuracy_mps` | float64 | Divide mm/s by 1,000 |
| `HdgAcc` | `gps_heading_accuracy_deg` | float64 | Divide degE5 by 1e5 |
| `Yaw` | `gps_yaw_deg` | float64 | Divide centidegrees by 100; omit 0 and `UINT16_MAX`; preserve 360 as north |

## SYSTEM_TIME (2)

Both fields are optional.

| Source field | Normalized field | Type | Behavior |
| --- | --- | --- | --- |
| `TimeUnixUsec` | `device_unix_time_usec` | uint64 | Preserve nonzero Unix microseconds |
| `TimeBootMs` | `device_boot_time_ms` | uint64 | Preserve boot-relative milliseconds |

The record does not automatically promote the device time to `event_time`.
Clock validation and boot-to-UTC correlation are a separate policy.

## Current transport limitations

The agent currently renders enums and bitmasks as strings. String enum names
are normalized safely, but numeric bitmasks cannot be reconstructed without
depending on gomavlib display formatting. The agent/protobuf contract needs
numeric companion fields for heartbeat mode bits, system sensor masks, and
battery fault masks before those numeric normalized fields are enabled.

The current frame transport also does not expose MAVLink system ID, component
ID, or packet sequence. Those remain optional source metadata and are not used
as aircraft identity.
