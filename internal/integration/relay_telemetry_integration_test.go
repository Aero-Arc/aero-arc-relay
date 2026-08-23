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
	testWALID      = "0195f6a8-86d1-7be7-a104-3a814dc19f9e"
	testWALSeq     = uint64(424242)
	testBatchSize  = 8
)

var testCaptureTime = time.Date(2026, time.July, 23, 17, 34, 56, 789123456, time.UTC)

func TestRelayTelemetry_NormalizesPersistsQueriesAndFlushesInfluxDB(t *testing.T) {
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

	frame := globalPositionIntFrame(testWALSeq, testCaptureTime)
	admissionStarted := time.Now().UTC()
	sendAndRequireOK(t, agent, frame)
	admissionFinished := time.Now().UTC()
	// STATUS_OK confirms validation, normalization, and admission into the
	// Relay's in-memory telemetry writer queue. It does not mean InfluxDB has
	// flushed the batch or that the row is durably queryable.

	// Include a second independently queried telemetry group, then fill the
	// configured production batch so both records persist without relying on the
	// deliberately long periodic flush interval.
	batteryCaptureTime := testCaptureTime.Add(time.Nanosecond)
	batterySequence := testWALSeq + 1
	sendAndRequireOK(t, agent, batteryStatusFrame(batterySequence, batteryCaptureTime))
	for offset := 2; offset < testBatchSize; offset++ {
		sendAndRequireOK(
			t,
			agent,
			globalPositionIntFrame(testWALSeq+uint64(offset), testCaptureTime.Add(time.Duration(offset))),
		)
	}

	primaryFrameID := frameID(testWALSeq, testCaptureTime)
	query := frameQuery(primaryFrameID, agent.SessionID)
	queryCtx, cancelQuery := context.WithTimeout(context.Background(), 20*time.Second)
	row, err := influx.AwaitRow(queryCtx, 100*time.Millisecond, query, "frame_id="+primaryFrameID)
	cancelQuery()
	if err != nil {
		t.Fatalf("query normalized telemetry (relay=%s influx=%s): %v", relay.Address, influx.URL, err)
	}
	assertGlobalPositionIntRow(t, row, primaryFrameID, agent.SessionID, admissionStarted, admissionFinished)

	batteryFrameID := frameID(batterySequence, batteryCaptureTime)
	batteryCtx, cancelBattery := context.WithTimeout(context.Background(), 20*time.Second)
	batteryRow, err := influx.AwaitRow(
		batteryCtx,
		100*time.Millisecond,
		frameQuery(batteryFrameID, agent.SessionID),
		"frame_id="+batteryFrameID,
	)
	cancelBattery()
	if err != nil {
		t.Fatalf("query normalized battery telemetry (relay=%s influx=%s): %v", relay.Address, influx.URL, err)
	}
	assertBatteryStatusRow(t, batteryRow, batteryFrameID, agent.SessionID, batterySequence, batteryCaptureTime)

	// This ninth record cannot reach BatchSize, and the periodic flush interval
	// is one hour. Confirm it is not queryable before shutdown, then require
	// Relay.Close to drain and flush it.
	shutdownSequence := testWALSeq + testBatchSize
	shutdownCaptureTime := testCaptureTime.Add(testBatchSize * time.Nanosecond)
	sendAndRequireOK(t, agent, globalPositionIntFrame(shutdownSequence, shutdownCaptureTime))
	shutdownFrameID := frameID(shutdownSequence, shutdownCaptureTime)
	shutdownQuery := frameQuery(shutdownFrameID, agent.SessionID)
	pendingCtx, cancelPending := context.WithTimeout(context.Background(), 5*time.Second)
	pendingRows, err := influx.QueryRows(pendingCtx, shutdownQuery)
	cancelPending()
	if err != nil {
		t.Fatalf("query pending shutdown frame before Relay shutdown: %v", err)
	}
	if len(pendingRows) != 0 {
		t.Fatalf("shutdown frame was already persisted before shutdown: frame_id=%s rows=%#v", shutdownFrameID, pendingRows)
	}

	if err := agent.Close(); err != nil {
		t.Fatalf("close fake Agent stream: %v", err)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	if err := relay.Shutdown(shutdownCtx); err != nil {
		cancelShutdown()
		t.Fatalf("cleanly shut down Relay and flush telemetry: %v", err)
	}
	cancelShutdown()

	// The normalized telemetry writer is now drained and closed. The pending
	// ninth frame becoming queryable specifically proves shutdown flushing.
	postShutdownCtx, cancelPostShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	rows, err := influx.QueryRows(postShutdownCtx, shutdownQuery)
	cancelPostShutdown()
	if err != nil {
		t.Fatalf("query shutdown-flushed record after Relay shutdown: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf(
			"shutdown-flushed rows = %d, want 1; frame_id=%s rows=%#v",
			len(rows), shutdownFrameID, rows,
		)
	}
	assertString(t, rows[0], "wal_id", testWALID)
	assertUint(t, rows[0], "wal_sequence", shutdownSequence)
	assertInt(t, rows[0], "agent_capture_time_ns", shutdownCaptureTime.UnixNano())
}

func batteryStatusFrame(sequence uint64, captureTime time.Time) *agentv1.TelemetryFrame {
	return &agentv1.TelemetryFrame{
		WalId: testWALID, Seq: sequence, SentAtUnixNs: captureTime.UnixNano(), Dialect: "common", MsgId: 147, MsgName: "BATTERY_STATUS",
		Fields: map[string]string{
			"Id": "0", "BatteryFunction": "MAV_BATTERY_FUNCTION_ALL", "Type": "MAV_BATTERY_TYPE_LIPO",
			"Temperature": "2534", "Voltages": "[4200,4190,65535,65535]", "VoltagesExt": "[0,0,0,0]",
			"CurrentBattery": "823", "CurrentConsumed": "1050", "EnergyConsumed": "360",
			"BatteryRemaining": "84", "TimeRemaining": "600", "ChargeState": "MAV_BATTERY_CHARGE_STATE_OK",
			"Mode": "MAV_BATTERY_MODE_AUTO_DISCHARGING", "FaultBitmask": "MAV_BATTERY_FAULT_NONE",
		},
	}
}

func globalPositionIntFrame(sequence uint64, captureTime time.Time) *agentv1.TelemetryFrame {
	return &agentv1.TelemetryFrame{
		WalId:        testWALID,
		Seq:          sequence,
		SentAtUnixNs: captureTime.UnixNano(),
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

func sendAndRequireOK(
	t *testing.T,
	agent *testsupport.FakeAgent,
	frame *agentv1.TelemetryFrame,
) *agentv1.TelemetryAck {
	t.Helper()
	ack, err := agent.Send(frame)
	if err != nil {
		t.Fatalf("send deterministic GLOBAL_POSITION_INT sequence %d: %v", frame.Seq, err)
	}
	if ack.GetSeq() != frame.Seq {
		t.Fatalf("ACK sequence = %d, want %d", ack.GetSeq(), frame.Seq)
	}
	if ack.GetStatus() != agentv1.TelemetryAck_STATUS_OK {
		t.Fatalf("Relay rejected sequence %d: status=%s error=%q", frame.Seq, ack.GetStatus(), ack.GetError())
	}
	return ack
}

func frameID(sequence uint64, _ time.Time) string {
	return fmt.Sprintf("%d:%s:%d:%s:%d", len(testAgentID), testAgentID, len(testWALID), testWALID, sequence)
}

func frameQuery(frameID, sessionID string) string {
	return fmt.Sprintf(`
SELECT *
FROM aircraft_telemetry
WHERE frame_id = '%s' AND session_id = '%s'
`, frameID, sessionID)
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
	assertString(t, row, "wal_id", testWALID)
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

func assertBatteryStatusRow(
	t *testing.T,
	row map[string]any,
	frameID, sessionID string,
	sequence uint64,
	captureTime time.Time,
) {
	t.Helper()
	assertString(t, row, "message_name", "battery_status")
	assertString(t, row, "frame_id", frameID)
	assertString(t, row, "agent_id", testAgentID)
	assertString(t, row, "aircraft_id", testAircraftID)
	assertString(t, row, "relay_id", testRelayID)
	assertString(t, row, "session_id", sessionID)
	assertString(t, row, "wal_id", testWALID)
	assertString(t, row, "battery_function", "mav_battery_function_all")
	assertString(t, row, "battery_type", "mav_battery_type_lipo")
	assertString(t, row, "battery_charge_state", "mav_battery_charge_state_ok")
	assertString(t, row, "battery_mode", "mav_battery_mode_auto_discharging")
	assertFloat(t, row, "battery_temperature_c", 25.34, 1e-9)
	assertFloat(t, row, "battery_voltage_v", 8.39, 1e-9)
	assertFloat(t, row, "battery_current_a", 8.23, 1e-9)
	assertFloat(t, row, "battery_consumed_wh", 10, 1e-9)
	assertFloat(t, row, "battery_remaining_pct", 84, 1e-9)
	assertUint(t, row, "battery_id", 0)
	assertInt(t, row, "battery_consumed_mah", 1050)
	assertInt(t, row, "battery_time_remaining_s", 600)
	assertUint(t, row, "wal_sequence", sequence)
	assertInt(t, row, "agent_capture_time_ns", captureTime.UnixNano())
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
