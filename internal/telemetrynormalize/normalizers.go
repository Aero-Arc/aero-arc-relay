package telemetrynormalize

import (
	"fmt"
	"math"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func normalizeGlobalPositionInt(envelope telemetry.TelemetryEnvelope) (Record, error) {
	record, err := baseRecord(envelope, "global_position_int")
	if err != nil {
		return Record{}, err
	}
	lat, err := requiredInt64(envelope.Fields, "Lat")
	if err != nil {
		return Record{}, err
	}
	lon, err := requiredInt64(envelope.Fields, "Lon")
	if err != nil {
		return Record{}, err
	}
	if lat < -900000000 || lat > 900000000 || lon < -1800000000 || lon > 1800000000 {
		return Record{}, fmt.Errorf("latitude or longitude outside valid range")
	}
	record.Fields["latitude_deg"] = float64(lat) / 1e7
	record.Fields["longitude_deg"] = float64(lon) / 1e7
	if value, ok := optionalUint64(envelope.Fields, "TimeBootMs"); ok {
		record.Fields["device_boot_time_ms"] = value
		record.Timing.DeviceTime = &DeviceTime{Value: value, Unit: DeviceTimeUnitMilliseconds, Basis: DeviceTimeBasisSystemBoot}
	}
	if value, ok := optionalInt64(envelope.Fields, "Alt"); ok {
		record.Fields["altitude_msl_m"] = float64(value) / 1000
	}
	if value, ok := optionalInt64(envelope.Fields, "RelativeAlt"); ok {
		record.Fields["relative_altitude_m"] = float64(value) / 1000
	}
	vx, hasVX := optionalInt64(envelope.Fields, "Vx")
	vy, hasVY := optionalInt64(envelope.Fields, "Vy")
	if hasVX {
		record.Fields["velocity_north_mps"] = float64(vx) / 100
	}
	if hasVY {
		record.Fields["velocity_east_mps"] = float64(vy) / 100
	}
	if value, ok := optionalInt64(envelope.Fields, "Vz"); ok {
		record.Fields["velocity_down_mps"] = float64(value) / 100
	}
	if hasVX && hasVY {
		record.Fields["groundspeed_mps"] = math.Hypot(float64(vx), float64(vy)) / 100
	}
	if value, ok := optionalUint64(envelope.Fields, "Hdg"); ok && value != math.MaxUint16 && value <= 35999 {
		record.Fields["heading_deg"] = float64(value) / 100
	}
	return validated(record)
}

func normalizeBatteryStatus(envelope telemetry.TelemetryEnvelope) (Record, error) {
	record, err := baseRecord(envelope, "battery_status")
	if err != nil {
		return Record{}, err
	}
	id, err := requiredInt64(envelope.Fields, "Id")
	if err != nil || id < 0 || id > math.MaxUint8 {
		return Record{}, fmt.Errorf("battery ID is invalid")
	}
	record.Fields["battery_id"] = uint64(id)
	copyEnum(record.Fields, "battery_function", envelope.Fields, "BatteryFunction")
	copyEnum(record.Fields, "battery_type", envelope.Fields, "Type")
	copyEnum(record.Fields, "battery_charge_state", envelope.Fields, "ChargeState")
	copyEnum(record.Fields, "battery_mode", envelope.Fields, "Mode")
	if value, ok := optionalInt64(envelope.Fields, "Temperature"); ok && value != math.MaxInt16 {
		record.Fields["battery_temperature_c"] = float64(value) / 100
	}
	if value, ok := batteryVoltage(envelope.Fields); ok {
		record.Fields["battery_voltage_v"] = value
	}
	if value, ok := optionalInt64(envelope.Fields, "CurrentBattery"); ok && value != -1 {
		record.Fields["battery_current_a"] = float64(value) / 100
	}
	if value, ok := optionalInt64(envelope.Fields, "CurrentConsumed"); ok && value != -1 {
		record.Fields["battery_consumed_mah"] = value
	}
	if value, ok := optionalInt64(envelope.Fields, "EnergyConsumed"); ok && value != -1 {
		record.Fields["battery_consumed_wh"] = float64(value) / 36
	}
	if value, ok := optionalInt64(envelope.Fields, "BatteryRemaining"); ok && value >= 0 && value <= 100 {
		record.Fields["battery_remaining_pct"] = float64(value)
	}
	if value, ok := optionalInt64(envelope.Fields, "TimeRemaining"); ok && value > 0 {
		record.Fields["battery_time_remaining_s"] = value
	}
	// FaultBitmask is currently rendered as a descriptive string by the agent.
	// A numeric companion field is required before lossless fault bits can be stored.
	return validated(record)
}

func normalizeHeartbeat(envelope telemetry.TelemetryEnvelope) (Record, error) {
	record, err := baseRecord(envelope, "heartbeat")
	if err != nil {
		return Record{}, err
	}
	copyEnum(record.Fields, "vehicle_type", envelope.Fields, "Type")
	copyEnum(record.Fields, "autopilot_type", envelope.Fields, "Autopilot")
	copyEnum(record.Fields, "base_mode", envelope.Fields, "BaseMode")
	copyEnum(record.Fields, "system_status", envelope.Fields, "SystemStatus")
	if value, ok := optionalUint64(envelope.Fields, "CustomMode"); ok {
		record.Fields["custom_mode"] = value
	}
	if value, ok := optionalUint64(envelope.Fields, "MavlinkVersion"); ok {
		record.Fields["mavlink_version"] = value
	}
	return validated(record)
}

func normalizeSysStatus(envelope telemetry.TelemetryEnvelope) (Record, error) {
	record, err := baseRecord(envelope, "sys_status")
	if err != nil {
		return Record{}, err
	}
	if value, ok := optionalUint64(envelope.Fields, "Load"); ok && value <= 1000 {
		record.Fields["mainloop_load_pct"] = float64(value) / 10
	}
	if value, ok := optionalUint64(envelope.Fields, "DropRateComm"); ok {
		record.Fields["communication_drop_rate_pct"] = float64(value) / 100
	}
	copyUint(record.Fields, "communication_error_count", envelope.Fields, "ErrorsComm")
	copyUint(record.Fields, "autopilot_error_count_1", envelope.Fields, "ErrorsCount1")
	copyUint(record.Fields, "autopilot_error_count_2", envelope.Fields, "ErrorsCount2")
	copyUint(record.Fields, "autopilot_error_count_3", envelope.Fields, "ErrorsCount3")
	copyUint(record.Fields, "autopilot_error_count_4", envelope.Fields, "ErrorsCount4")
	// Sensor masks are descriptive strings in the current transport. Preserve
	// their readable form until numeric companion fields are available.
	copyEnum(record.Fields, "sensors_present", envelope.Fields, "OnboardControlSensorsPresent")
	copyEnum(record.Fields, "sensors_enabled", envelope.Fields, "OnboardControlSensorsEnabled")
	copyEnum(record.Fields, "sensors_health", envelope.Fields, "OnboardControlSensorsHealth")
	copyEnum(record.Fields, "sensors_present_extended", envelope.Fields, "OnboardControlSensorsPresentExtended")
	copyEnum(record.Fields, "sensors_enabled_extended", envelope.Fields, "OnboardControlSensorsEnabledExtended")
	copyEnum(record.Fields, "sensors_health_extended", envelope.Fields, "OnboardControlSensorsHealthExtended")
	return validated(record)
}

func normalizeVFRHUD(envelope telemetry.TelemetryEnvelope) (Record, error) {
	record, err := baseRecord(envelope, "vfr_hud")
	if err != nil {
		return Record{}, err
	}
	copyFloat(record.Fields, "airspeed_mps", envelope.Fields, "Airspeed")
	copyFloat(record.Fields, "groundspeed_mps", envelope.Fields, "Groundspeed")
	if value, ok := optionalInt64(envelope.Fields, "Heading"); ok && value >= 0 && value <= 360 {
		record.Fields["heading_deg"] = float64(value)
	}
	if value, ok := optionalUint64(envelope.Fields, "Throttle"); ok && value <= 100 {
		record.Fields["throttle_pct"] = float64(value)
	}
	copyFloat(record.Fields, "altitude_msl_m", envelope.Fields, "Alt")
	copyFloat(record.Fields, "climb_rate_mps", envelope.Fields, "Climb")
	return validated(record)
}

func normalizeExtendedSysState(envelope telemetry.TelemetryEnvelope) (Record, error) {
	record, err := baseRecord(envelope, "extended_sys_state")
	if err != nil {
		return Record{}, err
	}
	copyEnum(record.Fields, "vtol_state", envelope.Fields, "VtolState")
	copyEnum(record.Fields, "landed_state", envelope.Fields, "LandedState")
	return validated(record)
}

func normalizeGPSRawInt(envelope telemetry.TelemetryEnvelope) (Record, error) {
	record, err := baseRecord(envelope, "gps_raw_int")
	if err != nil {
		return Record{}, err
	}
	if value, ok := optionalUint64(envelope.Fields, "TimeUsec"); ok {
		record.Fields["device_time_usec"] = value
		record.Timing.DeviceTime = &DeviceTime{Value: value, Unit: DeviceTimeUnitMicroseconds, Basis: DeviceTimeBasisUnknown}
	}
	copyEnum(record.Fields, "gps_fix_type", envelope.Fields, "FixType")
	if value, ok := optionalInt64(envelope.Fields, "Lat"); ok && value >= -900000000 && value <= 900000000 {
		record.Fields["gps_latitude_deg"] = float64(value) / 1e7
	}
	if value, ok := optionalInt64(envelope.Fields, "Lon"); ok && value >= -1800000000 && value <= 1800000000 {
		record.Fields["gps_longitude_deg"] = float64(value) / 1e7
	}
	if value, ok := optionalInt64(envelope.Fields, "Alt"); ok {
		record.Fields["gps_altitude_msl_m"] = float64(value) / 1000
	}
	if value, ok := optionalInt64(envelope.Fields, "AltEllipsoid"); ok {
		record.Fields["gps_altitude_ellipsoid_m"] = float64(value) / 1000
	}
	copyScaledUintUnless(record.Fields, "gps_hdop", envelope.Fields, "Eph", math.MaxUint16, 100)
	copyScaledUintUnless(record.Fields, "gps_vdop", envelope.Fields, "Epv", math.MaxUint16, 100)
	copyScaledUintUnless(record.Fields, "gps_groundspeed_mps", envelope.Fields, "Vel", math.MaxUint16, 100)
	copyScaledUintUnless(record.Fields, "gps_course_over_ground_deg", envelope.Fields, "Cog", math.MaxUint16, 100)
	if value, ok := optionalUint64(envelope.Fields, "SatellitesVisible"); ok && value != math.MaxUint8 {
		record.Fields["gps_satellites_visible"] = value
	}
	copyScaledUint(record.Fields, "gps_horizontal_accuracy_m", envelope.Fields, "HAcc", 1000)
	copyScaledUint(record.Fields, "gps_vertical_accuracy_m", envelope.Fields, "VAcc", 1000)
	copyScaledUint(record.Fields, "gps_speed_accuracy_mps", envelope.Fields, "VelAcc", 1000)
	copyScaledUint(record.Fields, "gps_heading_accuracy_deg", envelope.Fields, "HdgAcc", 1e5)
	if value, ok := optionalUint64(envelope.Fields, "Yaw"); ok && value != 0 && value != math.MaxUint16 && value <= 36000 {
		record.Fields["gps_yaw_deg"] = float64(value) / 100
	}
	return validated(record)
}

func normalizeSystemTime(envelope telemetry.TelemetryEnvelope) (Record, error) {
	record, err := baseRecord(envelope, "system_time")
	if err != nil {
		return Record{}, err
	}
	if value, ok := optionalUint64(envelope.Fields, "TimeUnixUsec"); ok && value != 0 {
		record.Fields["device_unix_time_usec"] = value
	}
	if value, ok := optionalUint64(envelope.Fields, "TimeBootMs"); ok {
		record.Fields["device_boot_time_ms"] = value
	}
	return validated(record)
}

func validated(record Record) (Record, error) {
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func copyEnum(target Fields, output string, source map[string]any, input string) {
	if value, ok := optionalEnum(source, input); ok {
		target[output] = value
	}
}

func copyUint(target Fields, output string, source map[string]any, input string) {
	if value, ok := optionalUint64(source, input); ok {
		target[output] = value
	}
}

func copyFloat(target Fields, output string, source map[string]any, input string) {
	if value, ok := optionalFloat64(source, input); ok {
		target[output] = value
	}
}

func copyScaledUint(target Fields, output string, source map[string]any, input string, divisor float64) {
	if value, ok := optionalUint64(source, input); ok {
		target[output] = float64(value) / divisor
	}
}

func copyScaledUintUnless(target Fields, output string, source map[string]any, input string, sentinel uint64, divisor float64) {
	if value, ok := optionalUint64(source, input); ok && value != sentinel {
		target[output] = float64(value) / divisor
	}
}

func batteryVoltage(fields map[string]any) (float64, bool) {
	base, baseOK := optionalUint16Array(fields, "Voltages")
	extended, extendedOK := optionalUint16Array(fields, "VoltagesExt")
	if !baseOK && !extendedOK {
		return 0, false
	}
	var millivolts uint64
	for _, value := range base {
		if value != math.MaxUint16 {
			millivolts += uint64(value)
		}
	}
	for _, value := range extended {
		if value != 0 {
			millivolts += uint64(value)
		}
	}
	if millivolts == 0 {
		return 0, false
	}
	return float64(millivolts) / 1000, true
}
