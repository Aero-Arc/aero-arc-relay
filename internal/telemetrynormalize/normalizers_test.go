package telemetrynormalize

import (
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
		WALSequence:    42,
		Fields:         fields,
	}
}
