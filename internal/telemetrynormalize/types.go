package telemetrynormalize

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const SchemaVersion uint16 = 1

type TimestampSource string

const (
	TimestampSourceDeviceUTC   TimestampSource = "device_utc"
	TimestampSourceAgent       TimestampSource = "agent_capture"
	TimestampSourceRelay       TimestampSource = "relay_receive"
	DeviceTimeBasisUnixEpoch                   = "unix_epoch"
	DeviceTimeBasisSystemBoot                  = "system_boot"
	DeviceTimeBasisUnknown                     = "unknown"
	DeviceTimeUnitMilliseconds                 = "milliseconds"
	DeviceTimeUnitMicroseconds                 = "microseconds"
)

type IdentityContext struct {
	OperatorID    string
	AircraftID    string
	AgentID       string
	RelayID       string
	SessionID     string
	FlightID      string
	IntentID      string
	IntentVersion uint32
}

type SourceContext struct {
	FrameID     string
	Sequence    uint64
	MessageID   uint32
	Dialect     string
	SystemID    *uint8
	ComponentID *uint8
}

type DeviceTime struct {
	Value uint64
	Unit  string
	Basis string
}

type TimingContext struct {
	EventTime        time.Time
	RelayTime        time.Time
	AgentCaptureTime *time.Time
	DeviceTime       *DeviceTime
	TimestampSource  TimestampSource
}

type Fields map[string]any

type Record struct {
	SchemaVersion uint16
	Identity      IdentityContext
	Source        SourceContext
	Timing        TimingContext
	MessageName   string
	Fields        Fields
}

func (r Record) Validate() error {
	if r.SchemaVersion == 0 {
		return errors.New("schema version is required")
	}
	if r.Identity.AgentID == "" {
		return errors.New("agent ID is required")
	}
	if r.Source.FrameID == "" {
		return errors.New("frame ID is required")
	}
	if r.MessageName == "" {
		return errors.New("message name is required")
	}
	if r.Source.Dialect == "" {
		return errors.New("dialect is required")
	}
	if r.Timing.EventTime.IsZero() || r.Timing.RelayTime.IsZero() {
		return errors.New("event and relay times are required")
	}
	if r.Timing.TimestampSource == "" {
		return errors.New("timestamp source is required")
	}
	for name, value := range r.Fields {
		if name == "" {
			return errors.New("normalized field name is empty")
		}
		switch typed := value.(type) {
		case int64, uint64, bool, string:
		case float64:
			if math.IsNaN(typed) || math.IsInf(typed, 0) {
				return fmt.Errorf("field %s is not finite", name)
			}
		default:
			return fmt.Errorf("field %s has unsupported type %T", name, value)
		}
	}
	return nil
}
