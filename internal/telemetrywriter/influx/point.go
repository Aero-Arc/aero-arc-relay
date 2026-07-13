package influx

import (
	"fmt"
	"strconv"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/makinje/aero-arc-relay/internal/telemetrynormalize"
)

const (
	AircraftTelemetryMeasurement   = "aircraft_telemetry"
	UnassignedTelemetryMeasurement = "unassigned_telemetry"
)

func recordToPoint(record telemetrynormalize.Record) (*influxdb3.Point, error) {
	if err := record.Validate(); err != nil {
		return nil, fmt.Errorf("invalid normalized record: %w", err)
	}
	measurement := AircraftTelemetryMeasurement
	if record.Identity.AircraftID == "" {
		measurement = UnassignedTelemetryMeasurement
	}
	tags := map[string]string{
		"agent_id":       record.Identity.AgentID,
		"message_name":   record.MessageName,
		"schema_version": strconv.FormatUint(uint64(record.SchemaVersion), 10),
	}
	optionalTag(tags, "operator_id", record.Identity.OperatorID)
	optionalTag(tags, "aircraft_id", record.Identity.AircraftID)
	optionalTag(tags, "relay_id", record.Identity.RelayID)
	optionalTag(tags, "flight_id", record.Identity.FlightID)
	optionalTag(tags, "intent_id", record.Identity.IntentID)
	if record.Identity.IntentVersion != 0 {
		tags["intent_version"] = strconv.FormatUint(uint64(record.Identity.IntentVersion), 10)
	}

	fields := make(map[string]interface{}, len(record.Fields)+12)
	for name, value := range record.Fields {
		fields[name] = value
	}
	fields["frame_id"] = record.Source.FrameID
	fields["wal_sequence"] = record.Source.Sequence
	fields["message_id"] = uint64(record.Source.MessageID)
	fields["dialect"] = record.Source.Dialect
	fields["timestamp_source"] = string(record.Timing.TimestampSource)
	fields["relay_time_ns"] = record.Timing.RelayTime.UnixNano()
	if record.Identity.SessionID != "" {
		fields["session_id"] = record.Identity.SessionID
	}
	if record.Timing.AgentCaptureTime != nil {
		fields["agent_capture_time_ns"] = record.Timing.AgentCaptureTime.UnixNano()
	}
	if record.Timing.DeviceTime != nil {
		fields["device_time_value"] = record.Timing.DeviceTime.Value
		fields["device_time_unit"] = record.Timing.DeviceTime.Unit
		fields["device_time_basis"] = record.Timing.DeviceTime.Basis
	}
	if record.Source.SystemID != nil {
		fields["mavlink_system_id"] = uint64(*record.Source.SystemID)
	}
	if record.Source.ComponentID != nil {
		fields["mavlink_component_id"] = uint64(*record.Source.ComponentID)
	}
	return influxdb3.NewPoint(measurement, tags, fields, record.Timing.EventTime), nil
}

func optionalTag(tags map[string]string, name, value string) {
	if value != "" {
		tags[name] = value
	}
}
