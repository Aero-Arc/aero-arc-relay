//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/makinje/aero-arc-relay/internal/testsupport"
)

const (
	testAgentID    = "agent-integration-gpi"
	testAircraftID = "aircraft-integration-gpi"
	testRelayID    = "relay-integration"
	testWALSeq     = uint64(424242)
)

var testCaptureTime = time.Date(2026, time.July, 23, 17, 34, 56, 789123456, time.UTC)

func TestRelayTelemetry_GlobalPositionIntPersistsToInfluxDB(t *testing.T) {
	influx := testsupport.StartInfluxDB(t)
	relay := testsupport.StartRelay(t, influx, testAgentID, testAircraftID, testRelayID)

	agentCtx, cancelAgent := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelAgent()
	agent, err := testsupport.RegisterFakeAgent(agentCtx, relay.Conn, testAgentID)
	if err != nil {
		t.Fatalf("establish fake Agent session through Relay at %s: %v", relay.Address, err)
	}
	if agent.SessionID == "" {
		t.Fatal("Relay registration returned an empty session ID")
	}

	frame := globalPositionIntFrame()
	admissionStarted := time.Now().UTC()
	ack, err := agent.Send(frame)
	if err != nil {
		t.Fatalf("send deterministic GLOBAL_POSITION_INT: %v", err)
	}
	admissionFinished := time.Now().UTC()
	if ack.GetSeq() != testWALSeq {
		t.Fatalf("ACK sequence = %d, want %d", ack.GetSeq(), testWALSeq)
	}
	if ack.GetStatus() != agentv1.TelemetryAck_STATUS_OK {
		t.Fatalf("Relay rejected telemetry: status=%s error=%q", ack.GetStatus(), ack.GetError())
	}
	// STATUS_OK confirms validation, normalization, and admission into the
	// Relay's in-memory telemetry writer queue. It does not mean InfluxDB has
	// flushed the batch or that the row is durably queryable.

	frameID := fmt.Sprintf("%d:%s:%d:%d", len(testAgentID), testAgentID, testCaptureTime.UnixNano(), testWALSeq)
	query := fmt.Sprintf(`
SELECT *
FROM aircraft_telemetry
WHERE frame_id = '%s' AND session_id = '%s'
`, frameID, agent.SessionID)
	queryCtx, cancelQuery := context.WithTimeout(context.Background(), 20*time.Second)
	row, err := influx.AwaitRow(queryCtx, 100*time.Millisecond, query, "frame_id="+frameID)
	cancelQuery()
	if err != nil {
		t.Fatalf("query normalized telemetry (relay=%s influx=%s): %v", relay.Address, influx.URL, err)
	}
	assertGlobalPositionIntRow(t, row, frameID, agent.SessionID, admissionStarted, admissionFinished)

	if err := agent.Close(); err != nil {
		t.Fatalf("close fake Agent stream: %v", err)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	if err := relay.Shutdown(shutdownCtx); err != nil {
		cancelShutdown()
		t.Fatalf("cleanly shut down Relay and flush telemetry: %v", err)
	}
	cancelShutdown()

	// The normalized telemetry writer is now drained and closed; confirm the
	// accepted record remains independently queryable after Relay shutdown.
	postShutdownCtx, cancelPostShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	rows, err := influx.QueryRows(postShutdownCtx, query)
	cancelPostShutdown()
	if err != nil {
		t.Fatalf("query persisted record after Relay shutdown: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows after Relay shutdown = %d, want 1; frame_id=%s rows=%#v", len(rows), frameID, rows)
	}
}

func globalPositionIntFrame() *agentv1.TelemetryFrame {
	return &agentv1.TelemetryFrame{
		Seq:          testWALSeq,
		SentAtUnixNs: testCaptureTime.UnixNano(),
		Dialect:      "common",
		MsgId:        33,
		MsgName:      "GLOBAL_POSITION_INT",
		Fields: map[string]string{
			"TimeBootMs":  "987654321",
			"Lat":         "-353632620",
			"Lon":         "1491652370",
			"Alt":         "584082",
			"RelativeAlt": "123456",
			"Vx":          "1234",
			"Vy":          "-567",
			"Vz":          "-321",
			"Hdg":         "12345",
		},
	}
}

func assertGlobalPositionIntRow(
	t *testing.T,
	row map[string]any,
	frameID, sessionID string,
	admissionStarted, admissionFinished time.Time,
) {
	t.Helper()
	assertString(t, row, "message_name", "global_position_int")
	assertString(t, row, "dialect", "common")
	assertString(t, row, "schema_version", "1")
	assertString(t, row, "frame_id", frameID)
	assertString(t, row, "agent_id", testAgentID)
	assertString(t, row, "aircraft_id", testAircraftID)
	assertString(t, row, "relay_id", testRelayID)
	assertString(t, row, "session_id", sessionID)
	assertString(t, row, "timestamp_source", "agent_capture")
	assertString(t, row, "device_time_basis", "system_boot")
	assertString(t, row, "device_time_unit", "milliseconds")

	assertFloat(t, row, "latitude_deg", -35.363262, 1e-9)
	assertFloat(t, row, "longitude_deg", 149.165237, 1e-9)
	assertFloat(t, row, "altitude_msl_m", 584.082, 1e-9)
	assertFloat(t, row, "relative_altitude_m", 123.456, 1e-9)
	assertFloat(t, row, "groundspeed_mps", math.Hypot(12.34, -5.67), 1e-9)
	assertFloat(t, row, "heading_deg", 123.45, 1e-9)
	assertFloat(t, row, "velocity_down_mps", -3.21, 1e-9)

	assertUint(t, row, "wal_sequence", testWALSeq)
	assertUint(t, row, "message_id", 33)
	assertUint(t, row, "device_boot_time_ms", 987654321)
	assertUint(t, row, "device_time_value", 987654321)
	assertInt(t, row, "agent_capture_time_ns", testCaptureTime.UnixNano())

	eventTime, ok := row["time"].(time.Time)
	if !ok {
		t.Fatalf("time = %#v (%T), want time.Time", row["time"], row["time"])
	}
	if !eventTime.Equal(testCaptureTime) {
		t.Errorf("database event time = %s, want agent capture time %s", eventTime, testCaptureTime)
	}
	relayTimeNS := asInt64(t, row, "relay_time_ns")
	relayTime := time.Unix(0, relayTimeNS).UTC()
	if relayTime.Before(admissionStarted.Add(-time.Second)) || relayTime.After(admissionFinished.Add(time.Second)) {
		t.Errorf("relay timestamp %s outside admission window [%s, %s]", relayTime, admissionStarted, admissionFinished)
	}
}

func assertString(t *testing.T, row map[string]any, field, want string) {
	t.Helper()
	got, ok := row[field].(string)
	if !ok || got != want {
		t.Errorf("%s = %#v (%T), want %q", field, row[field], row[field], want)
	}
}

func assertFloat(t *testing.T, row map[string]any, field string, want, tolerance float64) {
	t.Helper()
	got, ok := row[field].(float64)
	if !ok || math.Abs(got-want) > tolerance {
		t.Errorf("%s = %#v (%T), want %v ± %v", field, row[field], row[field], want, tolerance)
	}
}

func assertUint(t *testing.T, row map[string]any, field string, want uint64) {
	t.Helper()
	got, ok := row[field].(uint64)
	if !ok || got != want {
		t.Errorf("%s = %#v (%T), want %d", field, row[field], row[field], want)
	}
}

func assertInt(t *testing.T, row map[string]any, field string, want int64) {
	t.Helper()
	got := asInt64(t, row, field)
	if got != want {
		t.Errorf("%s = %d, want %d", field, got, want)
	}
}

func asInt64(t *testing.T, row map[string]any, field string) int64 {
	t.Helper()
	got, ok := row[field].(int64)
	if !ok {
		t.Errorf("%s = %#v (%T), want int64", field, row[field], row[field])
		return 0
	}
	return got
}
