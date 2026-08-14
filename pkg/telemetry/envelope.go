/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

// Package telemetry defines relay message envelopes and helpers for converting
// MAVLink messages into JSON and binary payloads.
package telemetry

import (
	"encoding/json"
	"time"

	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
)

type TelemetryEnvelope struct {
	AgentID         string         `json:"agent_id"`
	OperatorID      string         `json:"operator_id,omitempty"`
	AircraftID      string         `json:"aircraft_id,omitempty"`
	RelayID         string         `json:"relay_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	FlightID        string         `json:"flight_id,omitempty"`
	IntentID        string         `json:"intent_id,omitempty"`
	IntentVersion   uint32         `json:"intent_version,omitempty"`
	Source          string         `json:"source"`
	TimestampRelay  time.Time      `json:"timestamp_relay"`
	TimestampAgent  time.Time      `json:"timestamp_agent,omitempty"`
	TimestampDevice float64        `json:"timestamp_device"`
	Dialect         string         `json:"dialect,omitempty"`
	MsgID           uint32         `json:"msg_id"`
	MsgName         string         `json:"msg_name"`
	WALSequence     uint64         `json:"wal_sequence,omitempty"`
	SystemID        uint8          `json:"system_id"`
	ComponentID     uint8          `json:"component_id"`
	Sequence        uint16         `json:"sequence"` // MAVLink packet sequence, when available.
	Fields          map[string]any `json:"fields"`
	Raw             []byte         `json:"raw"`
}

// TelemetryMessage describes a serialisable telemetry payload. The interface
// exists to ease integration with sinks that previously consumed the old
// message structs.
type TelemetryMessage interface {
	GetSource() string
	GetTimestamp() time.Time
	GetMessageType() string
	ToJSON() ([]byte, error)
	ToEnvelope() TelemetryEnvelope
	ToBinary() ([]byte, error)
}

// GetSource returns the Agent source identifier carried by the envelope.
//
// Returns:
//   - result: is the string value produced by GetSource.
func (e TelemetryEnvelope) GetSource() string {
	return e.Source
}

// GetTimestamp returns Relay receipt time when present, otherwise converts the
// device timestamp from fractional Unix seconds. It does not consult the Agent
// timestamp field.
//
// Returns:
//   - timestamp: is UTC device time when Relay receipt time is absent, or zero
//     when neither source is available.
func (e TelemetryEnvelope) GetTimestamp() time.Time {
	if !e.TimestampRelay.IsZero() {
		return e.TimestampRelay
	}

	if e.TimestampDevice != 0 {
		secs := int64(e.TimestampDevice)
		nanos := int64((e.TimestampDevice - float64(secs)) * 1e9)
		return time.Unix(secs, nanos).UTC()
	}

	return time.Time{}
}

// GetMessageType returns MsgName exactly as stored in the envelope. Callers that
// require lower-snake-case grouping must normalize it separately.
//
// Returns:
//   - messageType: may contain a legacy mixed-case MAVLink name.
func (e TelemetryEnvelope) GetMessageType() string {
	return e.MsgName
}

// ToJSON converts TelemetryEnvelope to the requested representation.
//
// Returns:
//   - result: is the []byte value produced by ToJSON.
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (e TelemetryEnvelope) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ToEnvelope converts TelemetryEnvelope to the requested representation.
//
// Returns:
//   - result: is the TelemetryEnvelope value produced by ToEnvelope.
func (e TelemetryEnvelope) ToEnvelope() TelemetryEnvelope {
	return e
}

// ToBinary converts TelemetryEnvelope to the requested representation.
//
// Returns:
//   - result: is the []byte value produced by ToBinary.
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (e TelemetryEnvelope) ToBinary() ([]byte, error) {
	return e.ToJSON()
}

// BuildHeartbeatEnvelope builds a telemetry value from the supplied inputs.
//
// Parameters:
//   - source: is the string value supplied to BuildHeartbeatEnvelope.
//   - msg: is the *common.MessageHeartbeat value supplied to BuildHeartbeatEnvelope.
//
// Returns:
//   - result: is the TelemetryEnvelope value produced by BuildHeartbeatEnvelope.
func BuildHeartbeatEnvelope(source string, msg *common.MessageHeartbeat) TelemetryEnvelope {
	envelope := TelemetryEnvelope{
		AgentID:         source,
		Source:          source,
		TimestampRelay:  time.Now().UTC(),
		TimestampDevice: 0,
		MsgID:           msg.GetID(),
		MsgName:         "Heartbeat",
		SystemID:        0,
		ComponentID:     0,
		Sequence:        0,
		Fields: map[string]any{
			"type": msg.Type.String(),
		},
	}

	return envelope
}

// BuildGlobalPositionIntEnvelope builds a telemetry value from the supplied inputs.
//
// Parameters:
//   - source: is the string value supplied to BuildGlobalPositionIntEnvelope.
//   - msg: is the *common.MessageGlobalPositionInt value supplied to BuildGlobalPositionIntEnvelope.
//
// Returns:
//   - result: is the TelemetryEnvelope value produced by BuildGlobalPositionIntEnvelope.
func BuildGlobalPositionIntEnvelope(source string, msg *common.MessageGlobalPositionInt) TelemetryEnvelope {
	envelope := TelemetryEnvelope{
		AgentID:         source,
		Source:          source,
		TimestampRelay:  time.Now().UTC(),
		TimestampDevice: 0,
		MsgID:           msg.GetID(),
		MsgName:         "GlobalPositionInt",
		SystemID:        0,
		ComponentID:     0,
		Sequence:        0,
		Fields: map[string]any{
			"latitude":     msg.Lat,
			"longitude":    msg.Lon,
			"altitude":     msg.Alt,
			"relative_alt": msg.RelativeAlt,
			"vx":           msg.Vx,
			"vy":           msg.Vy,
			"vz":           msg.Vz,
			"heading":      msg.Hdg,
		},
	}

	return envelope
}

// BuildAttitudeEnvelope builds a telemetry value from the supplied inputs.
//
// Parameters:
//   - source: is the string value supplied to BuildAttitudeEnvelope.
//   - msg: is the *common.MessageAttitude value supplied to BuildAttitudeEnvelope.
//
// Returns:
//   - result: is the TelemetryEnvelope value produced by BuildAttitudeEnvelope.
func BuildAttitudeEnvelope(source string, msg *common.MessageAttitude) TelemetryEnvelope {
	envelope := TelemetryEnvelope{
		AgentID:         source,
		Source:          source,
		TimestampRelay:  time.Now().UTC(),
		TimestampDevice: 0,
		MsgID:           msg.GetID(),
		MsgName:         "Attitude",
		SystemID:        0,
		ComponentID:     0,
		Sequence:        0,
		Fields: map[string]any{
			"pitch":       msg.Pitch,
			"roll":        msg.Roll,
			"yaw":         msg.Yaw,
			"pitch_speed": msg.Pitchspeed,
			"roll_speed":  msg.Rollspeed,
			"yaw_speed":   msg.Yawspeed,
		},
	}

	return envelope
}

// BuildVfrHudEnvelope builds a telemetry value from the supplied inputs.
//
// Parameters:
//   - source: is the string value supplied to BuildVfrHudEnvelope.
//   - msg: is the *common.MessageVfrHud value supplied to BuildVfrHudEnvelope.
//
// Returns:
//   - result: is the TelemetryEnvelope value produced by BuildVfrHudEnvelope.
func BuildVfrHudEnvelope(source string, msg *common.MessageVfrHud) TelemetryEnvelope {
	envelope := TelemetryEnvelope{
		AgentID:         source,
		Source:          source,
		TimestampRelay:  time.Now().UTC(),
		TimestampDevice: 0,
		MsgID:           msg.GetID(),
		MsgName:         "VFR_HUD",
		SystemID:        0,
		ComponentID:     0,
		Sequence:        0,
		Fields: map[string]any{
			"ground_speed": msg.Groundspeed,
			"altitude":     msg.Alt,
			"heading":      msg.Heading,
			"throttle":     msg.Throttle,
			"climb_rate":   msg.Climb,
		},
	}

	return envelope
}

// BuildSysStatusEnvelope builds a telemetry value from the supplied inputs.
//
// Parameters:
//   - source: is the string value supplied to BuildSysStatusEnvelope.
//   - msg: is the *common.MessageSysStatus value supplied to BuildSysStatusEnvelope.
//
// Returns:
//   - result: is the TelemetryEnvelope value produced by BuildSysStatusEnvelope.
func BuildSysStatusEnvelope(source string, msg *common.MessageSysStatus) TelemetryEnvelope {
	envelope := TelemetryEnvelope{
		AgentID:         source,
		Source:          source,
		TimestampRelay:  time.Now().UTC(),
		TimestampDevice: 0,
		MsgID:           msg.GetID(),
		MsgName:         "SystemStatus",
		SystemID:        0,
		ComponentID:     0,
		Sequence:        0,
		Fields: map[string]any{
			"battery_remaining":               msg.BatteryRemaining,
			"voltage_battery":                 msg.VoltageBattery,
			"onboard_control_sensors_present": msg.OnboardControlSensorsPresent.String(),
			"onboard_control_sensors_enabled": msg.OnboardControlSensorsEnabled.String(),
			"onboard_control_sensors_health":  msg.OnboardControlSensorsHealth.String(),
			"load":                            msg.Load,
			"drop_rate_comm":                  msg.DropRateComm,
			"errors_comm":                     msg.ErrorsComm,
			"errors_count1":                   msg.ErrorsCount1,
			"errors_count2":                   msg.ErrorsCount2,
			"errors_count3":                   msg.ErrorsCount3,
			"errors_count4":                   msg.ErrorsCount4,
			"sensors_present_extended":        msg.OnboardControlSensorsPresentExtended.String(),
			"sensors_enabled_extended":        msg.OnboardControlSensorsEnabledExtended.String(),
			"sensors_health_extended":         msg.OnboardControlSensorsHealthExtended.String(),
		},
	}

	return envelope
}
