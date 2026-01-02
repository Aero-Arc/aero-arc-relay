package telemetry

import (
	"encoding/json"
	"time"

	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
)

type TelemetryEnvelope struct {
	AgentID         string         `json:"agent_id"`
	Source          string         `json:"source"`
	TimestampRelay  time.Time      `json:"timestamp_relay"`
	TimestampDevice float64        `json:"timestamp_device"`
	MsgID           uint32         `json:"msg_id"`
	MsgName         string         `json:"msg_name"`
	SystemID        uint8          `json:"system_id"`
	ComponentID     uint8          `json:"component_id"`
	Sequence        uint16         `json:"sequence"`
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

func (e TelemetryEnvelope) GetSource() string {
	return e.Source
}

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

func (e TelemetryEnvelope) GetMessageType() string {
	return e.MsgName
}

func (e TelemetryEnvelope) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

func (e TelemetryEnvelope) ToEnvelope() TelemetryEnvelope {
	return e
}

func (e TelemetryEnvelope) ToBinary() ([]byte, error) {
	return e.ToJSON()
}

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
