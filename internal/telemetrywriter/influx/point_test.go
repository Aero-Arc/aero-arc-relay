package influx

import (
	"maps"
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
		Source: telemetrynormalize.SourceContext{FrameID: "7:agent-1:36:0195f6a8-86d1-7be7-a104-3a814dc19f9e:42", WALID: "0195f6a8-86d1-7be7-a104-3a814dc19f9e", Sequence: 42, MessageID: 33, Dialect: "common"},
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
	if _, ok := point.GetTag("aircraft_id"); ok {
		t.Error("mutable aircraft ID unexpectedly participates in point identity")
	}
	if got := point.GetField("aircraft_id"); got != "aircraft-1" {
		t.Errorf("aircraft field = %#v", got)
	}
	if got, _ := point.GetTag("message_name"); got != "global_position_int" {
		t.Errorf("message tag = %q", got)
	}
	if got, _ := point.GetTag("frame_id"); got != record.Source.FrameID {
		t.Errorf("frame ID tag = %q, want %q", got, record.Source.FrameID)
	}
	if got := point.GetField("frame_id"); got != nil {
		t.Errorf("frame_id stored as field = %#v", got)
	}
	if got := point.GetField("latitude_deg"); got != 41.8781 {
		t.Errorf("latitude field = %#v", got)
	}
	if _, ok := point.GetField("wal_sequence").(uint64); !ok {
		t.Errorf("wal_sequence type = %T", point.GetField("wal_sequence"))
	}
	if got := point.GetField("wal_id"); got != record.Source.WALID {
		t.Errorf("wal_id = %#v, want %q", got, record.Source.WALID)
	}
	if !point.Values.Timestamp.Equal(eventTime) {
		t.Errorf("timestamp = %v", point.Values.Timestamp)
	}
}

func TestRecordToPointUsesFrameIDForPointIdentity(t *testing.T) {
	eventTime := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	agentTime := eventTime
	record := telemetrynormalize.Record{
		SchemaVersion: 1,
		Identity: telemetrynormalize.IdentityContext{
			AgentID: "agent-1", RelayID: "relay-1", SessionID: "session-1", AircraftID: "aircraft-1",
		},
		Source: telemetrynormalize.SourceContext{
			FrameID: "7:agent-1:36:0195f6a8-86d1-7be7-a104-3a814dc19f9e:42", WALID: "0195f6a8-86d1-7be7-a104-3a814dc19f9e", Sequence: 42, MessageID: 33, Dialect: "common",
		},
		Timing: telemetrynormalize.TimingContext{
			EventTime: eventTime, RelayTime: eventTime, AgentCaptureTime: &agentTime,
			TimestampSource: telemetrynormalize.TimestampSourceAgent,
		},
		MessageName: "global_position_int",
		Fields:      telemetrynormalize.Fields{"latitude_deg": 41.8781},
	}

	first, err := recordToPoint(record)
	if err != nil {
		t.Fatalf("recordToPoint(first) error = %v", err)
	}
	retryRecord := record
	retryRecord.Identity = telemetrynormalize.IdentityContext{
		OperatorID: "operator-2", AircraftID: "", AgentID: "agent-1",
		RelayID: "relay-2", SessionID: "session-2", FlightID: "flight-2",
		IntentID: "intent-2", IntentVersion: 7,
	}
	retryRecord.Timing.RelayTime = eventTime.Add(time.Second)
	retry, err := recordToPoint(retryRecord)
	if err != nil {
		t.Fatalf("recordToPoint(retry) error = %v", err)
	}
	secondRecord := record
	secondRecord.Source.FrameID = "7:agent-1:36:0195f6a8-86d1-7be7-a104-3a814dc19f9e:43"
	secondRecord.Source.Sequence = 43
	second, err := recordToPoint(secondRecord)
	if err != nil {
		t.Fatalf("recordToPoint(second) error = %v", err)
	}

	firstFrameID, _ := first.GetTag("frame_id")
	secondFrameID, _ := second.GetTag("frame_id")
	if first.GetMeasurement() != retry.GetMeasurement() ||
		!maps.Equal(first.Values.Tags, retry.Values.Tags) ||
		!first.Values.Timestamp.Equal(retry.Values.Timestamp) {
		t.Fatalf("retry identity changed: first=(%q, %#v, %v), retry=(%q, %#v, %v)",
			first.GetMeasurement(), first.Values.Tags, first.Values.Timestamp,
			retry.GetMeasurement(), retry.Values.Tags, retry.Values.Timestamp)
	}
	if got := retry.GetField("relay_id"); got != "relay-2" {
		t.Fatalf("retry relay field = %#v", got)
	}
	if got := retry.GetField("flight_id"); got != "flight-2" {
		t.Fatalf("retry flight field = %#v", got)
	}
	if firstFrameID == secondFrameID {
		t.Fatalf("distinct WAL frames share frame ID tag %q", firstFrameID)
	}
	if !first.Values.Timestamp.Equal(second.Values.Timestamp) {
		t.Fatalf("test records do not share capture timestamp: %v != %v", first.Values.Timestamp, second.Values.Timestamp)
	}
}

func TestRecordToPointUsesStableMeasurementWithoutAssignment(t *testing.T) {
	record := telemetrynormalize.Record{
		SchemaVersion: 1,
		Identity: telemetrynormalize.IdentityContext{
			AgentID: "agent-1", RelayID: "relay-1", SessionID: "session-1",
		},
		Source: telemetrynormalize.SourceContext{FrameID: "7:agent-1:36:0195f6a8-86d1-7be7-a104-3a814dc19f9e:1", WALID: "0195f6a8-86d1-7be7-a104-3a814dc19f9e", Sequence: 1, MessageID: 0, Dialect: "common"},
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
	if point.GetMeasurement() != AircraftTelemetryMeasurement {
		t.Errorf("measurement = %q", point.GetMeasurement())
	}
	if _, ok := point.GetTag("aircraft_id"); ok {
		t.Error("unassigned point unexpectedly has aircraft_id tag")
	}
}
