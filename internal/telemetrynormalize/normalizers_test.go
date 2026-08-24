package telemetrynormalize

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func TestDefaultRegistryContainsSelectedMessages(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{
		"Heartbeat", "GlobalPositionInt", "BatteryStatus", "SysStatus",
		"VFR_HUD", "ExtendedSysState", "GpsRawInt", "SystemTime",
	} {
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("registry does not contain %q", name)
		}
	}
	if _, ok := registry.Lookup("Attitude"); ok {
		t.Error("registry unexpectedly contains deferred Attitude normalizer")
	}
}

func TestSelectedMessageNormalizers(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		messageID  uint32
		fields     map[string]any
		wantFields Fields
	}{
		{
			name:       "global position",
			message:    "*standard.MessageGlobalPositionInt",
			messageID:  33,
			fields:     map[string]any{"TimeBootMs": "42", "Lat": "418781000", "Lon": "-876291000", "Alt": "123450", "RelativeAlt": "23450", "Vx": "320", "Vy": "-40", "Vz": "15", "Hdg": "65535"},
			wantFields: Fields{"device_boot_time_ms": uint64(42), "latitude_deg": 41.8781, "longitude_deg": -87.6291, "altitude_msl_m": 123.45, "relative_altitude_m": 23.45, "velocity_north_mps": 3.2, "velocity_east_mps": -0.4, "velocity_down_mps": 0.15, "groundspeed_mps": math.Hypot(320, -40) / 100},
		},
		{
			name:       "battery status",
			message:    "*common.MessageBatteryStatus",
			messageID:  147,
			fields:     map[string]any{"Id": "0", "BatteryFunction": "MAV_BATTERY_FUNCTION_ALL", "Type": "MAV_BATTERY_TYPE_LIPO", "Temperature": "2534", "Voltages": "[4200,4190,65535,65535]", "VoltagesExt": "[0,0,0,0]", "CurrentBattery": "823", "CurrentConsumed": "1050", "EnergyConsumed": "360", "BatteryRemaining": "84", "TimeRemaining": "600", "ChargeState": "MAV_BATTERY_CHARGE_STATE_OK", "Mode": "MAV_BATTERY_MODE_AUTO_DISCHARGING", "FaultBitmask": "MAV_BATTERY_FAULT_NONE"},
			wantFields: Fields{"battery_id": uint64(0), "battery_function": "mav_battery_function_all", "battery_type": "mav_battery_type_lipo", "battery_temperature_c": 25.34, "battery_voltage_v": 8.39, "battery_current_a": 8.23, "battery_consumed_mah": int64(1050), "battery_consumed_wh": 10.0, "battery_remaining_pct": 84.0, "battery_time_remaining_s": int64(600), "battery_charge_state": "mav_battery_charge_state_ok", "battery_mode": "mav_battery_mode_auto_discharging"},
		},
		{
			name:       "heartbeat",
			message:    "*minimal.MessageHeartbeat",
			messageID:  0,
			fields:     map[string]any{"Type": "MAV_TYPE_QUADROTOR", "Autopilot": "MAV_AUTOPILOT_ARDUPILOTMEGA", "BaseMode": "MAV_MODE_FLAG_CUSTOM_MODE_ENABLED", "CustomMode": "4", "SystemStatus": "MAV_STATE_ACTIVE", "MavlinkVersion": "3"},
			wantFields: Fields{"vehicle_type": "mav_type_quadrotor", "autopilot_type": "mav_autopilot_ardupilotmega", "base_mode": "mav_mode_flag_custom_mode_enabled", "custom_mode": uint64(4), "system_status": "mav_state_active", "mavlink_version": uint64(3)},
		},
		{
			name:       "system status",
			message:    "*common.MessageSysStatus",
			messageID:  1,
			fields:     map[string]any{"Load": "375", "DropRateComm": "125", "ErrorsComm": "2", "ErrorsCount1": "1", "ErrorsCount2": "0", "ErrorsCount3": "0", "ErrorsCount4": "0", "OnboardControlSensorsPresent": "GYRO|GPS", "OnboardControlSensorsEnabled": "GYRO|GPS", "OnboardControlSensorsHealth": "GYRO|GPS"},
			wantFields: Fields{"mainloop_load_pct": 37.5, "communication_drop_rate_pct": 1.25, "communication_error_count": uint64(2), "autopilot_error_count_1": uint64(1), "autopilot_error_count_2": uint64(0), "autopilot_error_count_3": uint64(0), "autopilot_error_count_4": uint64(0), "sensors_present": "gyro|gps", "sensors_enabled": "gyro|gps", "sensors_health": "gyro|gps"},
		},
		{
			name:       "vfr hud",
			message:    "VFR_HUD",
			messageID:  74,
			fields:     map[string]any{"Airspeed": "13.8", "Groundspeed": "14.2", "Heading": "92", "Throttle": "48", "Alt": "320.2", "Climb": "1.3"},
			wantFields: Fields{"airspeed_mps": 13.8, "groundspeed_mps": 14.2, "heading_deg": 92.0, "throttle_pct": 48.0, "altitude_msl_m": 320.2, "climb_rate_mps": 1.3},
		},
		{
			name:       "extended system state",
			message:    "ExtendedSysState",
			messageID:  245,
			fields:     map[string]any{"VtolState": "MAV_VTOL_STATE_MC", "LandedState": "MAV_LANDED_STATE_IN_AIR"},
			wantFields: Fields{"vtol_state": "mav_vtol_state_mc", "landed_state": "mav_landed_state_in_air"},
		},
		{
			name:       "raw gps",
			message:    "GpsRawInt",
			messageID:  24,
			fields:     map[string]any{"TimeUsec": "123456", "FixType": "GPS_FIX_TYPE_3D_FIX", "Lat": "418781000", "Lon": "-876291000", "Alt": "123450", "Eph": "80", "Epv": "120", "Vel": "320", "Cog": "9250", "SatellitesVisible": "12", "AltEllipsoid": "153450", "HAcc": "500", "VAcc": "900", "VelAcc": "200", "HdgAcc": "100000", "Yaw": "36000"},
			wantFields: Fields{"device_time_usec": uint64(123456), "gps_fix_type": "gps_fix_type_3d_fix", "gps_latitude_deg": 41.8781, "gps_longitude_deg": -87.6291, "gps_altitude_msl_m": 123.45, "gps_altitude_ellipsoid_m": 153.45, "gps_hdop": 0.8, "gps_vdop": 1.2, "gps_groundspeed_mps": 3.2, "gps_course_over_ground_deg": 92.5, "gps_satellites_visible": uint64(12), "gps_horizontal_accuracy_m": 0.5, "gps_vertical_accuracy_m": 0.9, "gps_speed_accuracy_mps": 0.2, "gps_heading_accuracy_deg": 1.0, "gps_yaw_deg": 360.0},
		},
		{
			name:       "system time",
			message:    "SystemTime",
			messageID:  2,
			fields:     map[string]any{"TimeUnixUsec": "1783890000000000", "TimeBootMs": "42000"},
			wantFields: Fields{"device_unix_time_usec": uint64(1783890000000000), "device_boot_time_ms": uint64(42000)},
		},
	}

	registry := NewRegistry()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalizer, ok := registry.Lookup(test.message)
			if !ok {
				t.Fatalf("normalizer not found for %s", test.message)
			}
			record, err := normalizer.Normalize(testEnvelope(test.message, test.messageID, test.fields))
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if !reflect.DeepEqual(record.Fields, test.wantFields) {
				t.Errorf("fields = %#v, want %#v", record.Fields, test.wantFields)
			}
			if record.Timing.TimestampSource != TimestampSourceAgent {
				t.Errorf("timestamp source = %q", record.Timing.TimestampSource)
			}
		})
	}
}

func TestGlobalPositionRejectsMissingRequiredCoordinates(t *testing.T) {
	normalizer, _ := NewRegistry().Lookup("GlobalPositionInt")
	if _, err := normalizer.Normalize(testEnvelope("GlobalPositionInt", 33, map[string]any{"Lat": "1"})); err == nil {
		t.Fatal("expected missing longitude to fail normalization")
	}
}

func TestGPSRawIntOmitsUnavailableExtendedAccuracy(t *testing.T) {
	normalizer, _ := NewRegistry().Lookup("GpsRawInt")
	record, err := normalizer.Normalize(testEnvelope("GpsRawInt", 24, map[string]any{
		"FixType": "GPS_FIX_TYPE_3D_FIX",
		"HAcc":    "4294967295",
		"VAcc":    "4294967295",
		"VelAcc":  "4294967295",
		"HdgAcc":  "4294967295",
	}))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	for _, field := range []string{
		"gps_horizontal_accuracy_m",
		"gps_vertical_accuracy_m",
		"gps_speed_accuracy_mps",
		"gps_heading_accuracy_deg",
	} {
		if _, ok := record.Fields[field]; ok {
			t.Errorf("unavailable accuracy field %q was retained", field)
		}
	}
	if got := record.Fields["gps_fix_type"]; got != "gps_fix_type_3d_fix" {
		t.Errorf("gps_fix_type = %#v, want %q", got, "gps_fix_type_3d_fix")
	}
}

func TestNormalizersOmitValuesOutsideMAVLinkBounds(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		messageID uint32
		fields    map[string]any
		omitted   []string
	}{
		{
			name:      "global position source widths",
			message:   "GlobalPositionInt",
			messageID: 33,
			fields: map[string]any{
				"Lat": "0", "Lon": "0", "TimeBootMs": "4294967296",
				"Alt": "2147483648", "RelativeAlt": "-2147483649",
				"Vx": "32768", "Vy": "-32769", "Vz": "32768", "Hdg": "36000",
			},
			omitted: []string{
				"device_boot_time_ms", "altitude_msl_m", "relative_altitude_m",
				"velocity_north_mps", "velocity_east_mps", "velocity_down_mps",
				"groundspeed_mps", "heading_deg",
			},
		},
		{
			name:      "battery source widths and sentinels",
			message:   "BatteryStatus",
			messageID: 147,
			fields: map[string]any{
				"Id": "0", "Temperature": "32768", "CurrentBattery": "32768",
				"CurrentConsumed": "-2", "EnergyConsumed": "2147483648",
				"BatteryRemaining": "101", "TimeRemaining": "2147483648",
				"Voltages":    "[1,1,1,1,1,1,1,1,1,1,1]",
				"VoltagesExt": "[1,1,1,1,1]",
			},
			omitted: []string{
				"battery_temperature_c", "battery_voltage_v", "battery_current_a", "battery_consumed_mah",
				"battery_consumed_wh", "battery_remaining_pct", "battery_time_remaining_s",
			},
		},
		{
			name:      "heartbeat source widths",
			message:   "Heartbeat",
			messageID: 0,
			fields:    map[string]any{"CustomMode": "4294967296", "MavlinkVersion": "256"},
			omitted:   []string{"custom_mode", "mavlink_version"},
		},
		{
			name:      "system status percentages and counts",
			message:   "SysStatus",
			messageID: 1,
			fields: map[string]any{
				"Load": "1001", "DropRateComm": "10001", "ErrorsComm": "65536",
				"ErrorsCount1": "65536", "ErrorsCount2": "65536",
				"ErrorsCount3": "65536", "ErrorsCount4": "65536",
			},
			omitted: []string{
				"mainloop_load_pct", "communication_drop_rate_pct", "communication_error_count",
				"autopilot_error_count_1", "autopilot_error_count_2",
				"autopilot_error_count_3", "autopilot_error_count_4",
			},
		},
		{
			name:      "VFR HUD source widths and ranges",
			message:   "VFR_HUD",
			messageID: 74,
			fields: map[string]any{
				"Airspeed": "1e39", "Groundspeed": "-1e39", "Heading": "361",
				"Throttle": "101", "Alt": "1e39", "Climb": "-1e39",
			},
			omitted: []string{
				"airspeed_mps", "groundspeed_mps", "heading_deg",
				"throttle_pct", "altitude_msl_m", "climb_rate_mps",
			},
		},
		{
			name:      "raw GPS source widths and ranges",
			message:   "GpsRawInt",
			messageID: 24,
			fields: map[string]any{
				"Lat": "900000001", "Lon": "1800000001", "Alt": "2147483648",
				"AltEllipsoid": "-2147483649", "Eph": "65536", "Epv": "65536",
				"Vel": "65536", "Cog": "36000", "SatellitesVisible": "256",
				"HAcc": "4294967296", "VAcc": "4294967296", "VelAcc": "4294967296",
				"HdgAcc": "4294967296", "Yaw": "36001",
			},
			omitted: []string{
				"gps_latitude_deg", "gps_longitude_deg", "gps_altitude_msl_m",
				"gps_altitude_ellipsoid_m", "gps_hdop", "gps_vdop", "gps_groundspeed_mps",
				"gps_course_over_ground_deg", "gps_satellites_visible",
				"gps_horizontal_accuracy_m", "gps_vertical_accuracy_m",
				"gps_speed_accuracy_mps", "gps_heading_accuracy_deg", "gps_yaw_deg",
			},
		},
		{
			name:      "system time source width",
			message:   "SystemTime",
			messageID: 2,
			fields:    map[string]any{"TimeUnixUsec": "0", "TimeBootMs": "4294967296"},
			omitted:   []string{"device_unix_time_usec", "device_boot_time_ms"},
		},
	}

	registry := NewRegistry()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalizer, ok := registry.Lookup(test.message)
			if !ok {
				t.Fatalf("normalizer not found for %s", test.message)
			}
			record, err := normalizer.Normalize(testEnvelope(test.message, test.messageID, test.fields))
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			for _, field := range test.omitted {
				if value, exists := record.Fields[field]; exists {
					t.Errorf("out-of-range field %q was retained as %#v", field, value)
				}
			}
		})
	}
}

func TestDocumentedUpperBoundsArePreserved(t *testing.T) {
	registry := NewRegistry()

	sysStatus, _ := registry.Lookup("SysStatus")
	record, err := sysStatus.Normalize(testEnvelope("SysStatus", 1, map[string]any{"DropRateComm": "10000"}))
	if err != nil {
		t.Fatalf("normalize SYS_STATUS: %v", err)
	}
	if got := record.Fields["communication_drop_rate_pct"]; got != 100.0 {
		t.Errorf("communication_drop_rate_pct = %#v, want 100", got)
	}

	gps, _ := registry.Lookup("GpsRawInt")
	record, err = gps.Normalize(testEnvelope("GpsRawInt", 24, map[string]any{"Cog": "35999"}))
	if err != nil {
		t.Fatalf("normalize GPS_RAW_INT: %v", err)
	}
	if got := record.Fields["gps_course_over_ground_deg"]; got != 359.99 {
		t.Errorf("gps_course_over_ground_deg = %#v, want 359.99", got)
	}
}

func TestEnumNamesRemainForwardCompatible(t *testing.T) {
	normalizer, _ := NewRegistry().Lookup("ExtendedSysState")
	record, err := normalizer.Normalize(testEnvelope("ExtendedSysState", 245, map[string]any{
		"VtolState":   "MAV_VTOL_STATE_FUTURE",
		"LandedState": "MAV_LANDED_STATE_FUTURE",
	}))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got := record.Fields["vtol_state"]; got != "mav_vtol_state_future" {
		t.Errorf("vtol_state = %#v", got)
	}
	if got := record.Fields["landed_state"]; got != "mav_landed_state_future" {
		t.Errorf("landed_state = %#v", got)
	}
}

func TestRecordValidationRequiresRelayAndSessionIdentity(t *testing.T) {
	normalizer, _ := NewRegistry().Lookup("Heartbeat")
	record, err := normalizer.Normalize(testEnvelope("Heartbeat", 0, nil))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	record.Identity.RelayID = ""
	if err := record.Validate(); err == nil {
		t.Fatal("record without relay ID passed validation")
	}
	record.Identity.RelayID = "relay-1"
	record.Identity.SessionID = ""
	if err := record.Validate(); err == nil {
		t.Fatal("record without session ID passed validation")
	}
}

func TestRecordValidationAcceptsLegacyV1WithoutWALGenerationID(t *testing.T) {
	normalizer, _ := NewRegistry().Lookup("Heartbeat")
	record, err := normalizer.Normalize(testEnvelope("Heartbeat", 0, nil))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	record.Source.WALID = ""
	if err := record.Validate(); err != nil {
		t.Fatalf("legacy schema-v1 record without WAL generation ID failed validation: %v", err)
	}
	record.Source.WALID = "not-a-uuid"
	if err := record.Validate(); err == nil {
		t.Fatal("schema-v1 record with invalid WAL generation ID passed validation")
	}
}

func testEnvelope(message string, messageID uint32, fields map[string]any) telemetry.TelemetryEnvelope {
	return telemetry.TelemetryEnvelope{
		AgentID:        "agent-1",
		RelayID:        "relay-1",
		SessionID:      "session-1",
		TimestampRelay: time.Date(2026, 7, 12, 12, 0, 1, 0, time.UTC),
		TimestampAgent: time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC),
		Dialect:        "common",
		MsgID:          messageID,
		MsgName:        message,
		WALID:          "0195f6a8-86d1-7be7-a104-3a814dc19f9e",
		WALSequence:    42,
		Fields:         fields,
	}
}

func TestNormalizeRejectsMissingAgentCaptureTime(t *testing.T) {
	envelope := testEnvelope("GlobalPositionInt", 33, map[string]any{
		"Lat": "418781000", "Lon": "-876291000", "Alt": "123450",
	})
	envelope.TimestampAgent = time.Time{}
	normalizer, ok := NewRegistry().Lookup(envelope.MsgName)
	if !ok {
		t.Fatal("GlobalPositionInt normalizer is not registered")
	}
	if _, err := normalizer.Normalize(envelope); err == nil {
		t.Fatal("Normalize() accepted an envelope without agent capture time")
	}
}

func TestNormalizeRejectsInvalidWALGenerationID(t *testing.T) {
	for _, walID := range []string{"", "not-a-uuid"} {
		envelope := testEnvelope("GlobalPositionInt", 33, map[string]any{
			"Lat": "418781000", "Lon": "-876291000", "Alt": "123450",
		})
		envelope.WALID = walID
		normalizer, ok := NewRegistry().Lookup(envelope.MsgName)
		if !ok {
			t.Fatal("GlobalPositionInt normalizer is not registered")
		}
		if _, err := normalizer.Normalize(envelope); err == nil {
			t.Fatalf("Normalize() accepted WAL generation ID %q", walID)
		}
	}
}

func TestNormalizePreservesV1FrameIDAcrossWALIdentityRollout(t *testing.T) {
	normalizer, ok := NewRegistry().Lookup("GlobalPositionInt")
	if !ok {
		t.Fatal("GlobalPositionInt normalizer is not registered")
	}
	envelope := testEnvelope("GlobalPositionInt", 33, map[string]any{
		"Lat": "418781000", "Lon": "-876291000", "Alt": "123450",
	})
	wantFrameID := fmt.Sprintf(
		"%d:%s:%d:%d",
		len(envelope.AgentID),
		envelope.AgentID,
		envelope.TimestampAgent.UnixNano(),
		envelope.WALSequence,
	)

	first, err := normalizer.Normalize(envelope)
	if err != nil {
		t.Fatalf("Normalize(first WAL) error = %v", err)
	}
	envelope.WALID = "0195f6a8-86d1-7be7-a104-3a814dc19f9f"
	second, err := normalizer.Normalize(envelope)
	if err != nil {
		t.Fatalf("Normalize(second WAL) error = %v", err)
	}

	if first.Source.FrameID != wantFrameID || second.Source.FrameID != wantFrameID {
		t.Fatalf(
			"schema-v1 frame IDs changed with WAL metadata: first=%q second=%q want=%q",
			first.Source.FrameID,
			second.Source.FrameID,
			wantFrameID,
		)
	}
	if first.Source.WALID == second.Source.WALID {
		t.Fatalf("test WAL identities are equal: %q", first.Source.WALID)
	}
}
