package relay

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/mock"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func TestRelayCreation(t *testing.T) {
	cfg := &config.Config{
		Sinks:   config.SinksConfig{},
		Logging: config.LoggingConfig{Level: "info", Format: "text"},
	}

	_, err := New(cfg)
	if err == nil {
		t.Error("Expected error when no sinks are configured")
	}

	tempDir := t.TempDir()
	cfg.Sinks.File = &config.FileConfig{
		Path:             tempDir,
		Prefix:           "telemetry",
		Format:           "json",
		RotationInterval: time.Hour,
	}

	relay, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create relay: %v", err)
	}
	for _, sink := range relay.sinks {
		_ = sink.Close(context.Background())
	}
}

func TestHandleTelemetryMessage(t *testing.T) {
	mockSink := mock.NewMockSink()
	relay := &Relay{
		sinks: []sinks.Sink{mockSink},
	}

	msg := telemetry.TelemetryEnvelope{
		AgentID:        "test-agent",
		Source:         "test-agent",
		TimestampRelay: time.Now().UTC(),
		MsgName:        "Heartbeat",
		Fields: map[string]any{
			"type": "AUTO",
		},
	}

	relay.handleTelemetryMessage(msg)

	if mockSink.GetMessageCount() != 1 {
		t.Fatalf("Expected 1 message, got %d", mockSink.GetMessageCount())
	}

	received := mockSink.GetMessages()[0]
	if received.AgentID != "test-agent" {
		t.Errorf("Expected AgentID 'test-agent', got '%s'", received.AgentID)
	}
	if received.MsgName != "Heartbeat" {
		t.Errorf("Expected MsgName 'Heartbeat', got '%s'", received.MsgName)
	}
}

func TestHandleTelemetryMessageMultipleSinks(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink(), mock.NewMockSink()},
	}

	msg := telemetry.TelemetryEnvelope{
		AgentID:        "test-agent",
		Source:         "test-agent",
		TimestampRelay: time.Now().UTC(),
		MsgName:        "Status",
	}

	relay.handleTelemetryMessage(msg)

	for i, sink := range relay.sinks {
		mockSink := sink.(*mock.MockSink)
		if mockSink.GetMessageCount() != 1 {
			t.Errorf("Sink %d: Expected 1 message, got %d", i, mockSink.GetMessageCount())
		}
	}
}

func TestBuildTelemetryFrameEnvelope(t *testing.T) {
	relay := &Relay{}

	before := time.Now().UTC()
	frame := &agentv1.TelemetryFrame{
		AgentId: "agent-1",
		MsgId:   42,
		MsgName: "Status",
		Fields: map[string]string{
			"mode": "AUTO",
		},
	}

	envelope := relay.buildTelemetryFrameEnvelope(frame)
	after := time.Now().UTC()

	if envelope.AgentID != "agent-1" {
		t.Errorf("Expected AgentID 'agent-1', got '%s'", envelope.AgentID)
	}
	if envelope.MsgID != 42 {
		t.Errorf("Expected MsgID 42, got %d", envelope.MsgID)
	}
	if envelope.MsgName != "Status" {
		t.Errorf("Expected MsgName 'Status', got '%s'", envelope.MsgName)
	}
	if got := envelope.Fields["mode"]; got != "AUTO" {
		t.Errorf("Expected field 'mode' to be 'AUTO', got '%v'", got)
	}
	if envelope.TimestampRelay.Before(before) || envelope.TimestampRelay.After(after) {
		t.Errorf("TimestampRelay %v not within expected range", envelope.TimestampRelay)
	}
	if len(envelope.Raw) == 0 {
		t.Error("Expected Raw payload to be set")
	}
}

func TestHandleTelemetryFrame(t *testing.T) {
	mockSink := mock.NewMockSink()
	relay := &Relay{
		sinks: []sinks.Sink{mockSink},
	}

	frame := &agentv1.TelemetryFrame{
		AgentId: "agent-2",
		MsgId:   7,
		MsgName: "Heartbeat",
		Fields: map[string]string{
			"type": "AUTO",
		},
	}

	relay.handleTelemetryFrame(frame)

	if mockSink.GetMessageCount() != 1 {
		t.Fatalf("Expected 1 message, got %d", mockSink.GetMessageCount())
	}

	msg := mockSink.GetMessages()[0]
	if msg.AgentID != "agent-2" {
		t.Errorf("Expected AgentID 'agent-2', got '%s'", msg.AgentID)
	}
	if msg.MsgName != "Heartbeat" {
		t.Errorf("Expected MsgName 'Heartbeat', got '%s'", msg.MsgName)
	}
}

func TestConcurrentTelemetryHandling(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	numMessages := 100
	var wg sync.WaitGroup
	wg.Add(numMessages)

	for i := 0; i < numMessages; i++ {
		go func(id int) {
			defer wg.Done()
			msg := telemetry.TelemetryEnvelope{
				AgentID:        "test-agent",
				Source:         "test-agent",
				TimestampRelay: time.Now().UTC(),
				MsgName:        fmt.Sprintf("Status-%d", id),
			}
			relay.handleTelemetryMessage(msg)
		}(i)
	}

	wg.Wait()

	mockSink := relay.sinks[0].(*mock.MockSink)
	if mockSink.GetMessageCount() != numMessages {
		t.Errorf("Expected %d messages, got %d", numMessages, mockSink.GetMessageCount())
	}
}

func TestRelayErrorHandling(t *testing.T) {
	failingSink := &failingSink{}
	relay := &Relay{
		sinks: []sinks.Sink{failingSink, mock.NewMockSink()},
	}

	msg := telemetry.TelemetryEnvelope{
		AgentID:        "test-agent",
		Source:         "test-agent",
		TimestampRelay: time.Now().UTC(),
		MsgName:        "Heartbeat",
	}

	relay.handleTelemetryMessage(msg)

	mockSink := relay.sinks[1].(*mock.MockSink)
	if mockSink.GetMessageCount() != 1 {
		t.Errorf("Expected 1 message in working sink, got %d", mockSink.GetMessageCount())
	}
}

type failingSink struct {
	closed bool
}

func (f *failingSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	if f.closed {
		return nil
	}
	return fmt.Errorf("simulated sink failure")
}

func (f *failingSink) Close(ctx context.Context) error {
	f.closed = true
	return nil
}
