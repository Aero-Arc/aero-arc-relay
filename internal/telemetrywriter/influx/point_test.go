package influx

import (
	"testing"
	"time"

	"github.com/makinje/aero-arc-relay/internal/telemetrynormalize"
)

func TestRecordToPoint(t *testing.T) {
	eventTime := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	agentTime := eventTime.Add(-time.Millisecond)
	record := telemetrynormalize.Record{
		SchemaVersion: 1,
		Identity: telemetrynormalize.IdentityContext{
			OperatorID: "operator-1", AircraftID: "aircraft-1", AgentID: "agent-1",
			RelayID: "relay-1", SessionID: "session-1", FlightID: "flight-1", IntentID: "intent-1",
		},
		Source: telemetrynormalize.SourceContext{FrameID: "7:agent-1:42", Sequence: 42, MessageID: 33, Dialect: "common"},
		Timing: telemetrynormalize.TimingContext{
			EventTime: eventTime, RelayTime: eventTime.Add(time.Millisecond), AgentCaptureTime: &agentTime,
			TimestampSource: telemetrynormalize.TimestampSourceAgent,
		},
		MessageName: "global_position_int",
		Fields:      telemetrynormalize.Fields{"latitude_deg": 41.8781, "groundspeed_mps": 3.2},
	}
	point, err := recordToPoint(record)
	if err != nil {
		t.Fatalf("recordToPoint() error = %v", err)
	}
	if point.GetMeasurement() != AircraftTelemetryMeasurement {
		t.Errorf("measurement = %q", point.GetMeasurement())
	}
	if got, _ := point.GetTag("aircraft_id"); got != "aircraft-1" {
		t.Errorf("aircraft tag = %q", got)
	}
	if got, _ := point.GetTag("message_name"); got != "global_position_int" {
		t.Errorf("message tag = %q", got)
	}
	if got := point.GetField("latitude_deg"); got != 41.8781 {
		t.Errorf("latitude field = %#v", got)
	}
	if _, ok := point.GetField("wal_sequence").(uint64); !ok {
		t.Errorf("wal_sequence type = %T", point.GetField("wal_sequence"))
	}
	if !point.Values.Timestamp.Equal(eventTime) {
		t.Errorf("timestamp = %v", point.Values.Timestamp)
	}
}

func TestRecordToPointUsesUnassignedMeasurement(t *testing.T) {
	record := telemetrynormalize.Record{
		SchemaVersion: 1,
		Identity: telemetrynormalize.IdentityContext{
			AgentID: "agent-1", RelayID: "relay-1", SessionID: "session-1",
		},
		Source: telemetrynormalize.SourceContext{FrameID: "7:agent-1:1", Sequence: 1, MessageID: 0, Dialect: "common"},
		Timing: telemetrynormalize.TimingContext{
			EventTime: time.Now().UTC(), RelayTime: time.Now().UTC(), TimestampSource: telemetrynormalize.TimestampSourceRelay,
		},
		MessageName: "heartbeat",
		Fields:      telemetrynormalize.Fields{"system_status": "mav_state_active"},
	}
	point, err := recordToPoint(record)
	if err != nil {
		t.Fatalf("recordToPoint() error = %v", err)
	}
	if point.GetMeasurement() != UnassignedTelemetryMeasurement {
		t.Errorf("measurement = %q", point.GetMeasurement())
	}
	if _, ok := point.GetTag("aircraft_id"); ok {
		t.Error("unassigned point unexpectedly has aircraft_id tag")
	}
}
