package relay

import (
	"fmt"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/mock"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// TestRelayIntegration tests the full relay functionality with multiple sinks
func TestRelayIntegration(t *testing.T) {
	// Create test configuration
	cfg := &config.Config{
		Relay: config.RelayConfig{
			BufferSize: 1000,
		},
		MAVLink: config.MAVLinkConfig{
			Dialect: common.Dialect,
			Endpoints: []config.MAVLinkEndpoint{
				{
					Name:     "drone-1",
					DroneID:  "drone-1",
					Protocol: "udp",
					Port:     14550,
				},
				{
					Name:     "drone-2",
					DroneID:  "drone-2",
					Protocol: "udp",
					Port:     14551,
				},
			},
		},
		Sinks: config.SinksConfig{
			File: &config.FileConfig{
				Path:             "/tmp/test-telemetry",
				Format:           "json",
				RotationInterval: 24 * time.Hour,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}

	// Create relay with mock sinks
	relay := &Relay{
		config: cfg,
		sinks:  []sinks.Sink{mock.NewMockSink(), mock.NewMockSink()},
	}

	// Test that relay can handle messages from multiple sources
	// Test drone-1 heartbeat
	relay.handleHeartbeat(&common.MessageHeartbeat{CustomMode: 3}, "drone-1")
	// Test drone-2 heartbeat
	relay.handleHeartbeat(&common.MessageHeartbeat{CustomMode: 4}, "drone-2")
	// Test drone-1 position
	relay.handleGlobalPosition(&common.MessageGlobalPositionInt{Lat: 377749000, Lon: -122419400, Alt: 100500}, "drone-1")
	// Test drone-2 position
	relay.handleGlobalPosition(&common.MessageGlobalPositionInt{Lat: 377750000, Lon: -122419500, Alt: 101000}, "drone-2")

	// Verify all sinks received all messages
	for i, sink := range relay.sinks {
		mockSink := sink.(*mock.MockSink)
		if mockSink.GetMessageCount() != 4 {
			t.Errorf("Sink %d: Expected 4 messages, got %d", i, mockSink.GetMessageCount())
		}
	}
}

// TestRelayWithRealMAVLinkMessages tests relay with realistic MAVLink message scenarios
func TestRelayWithRealMAVLinkMessages(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	// Simulate a complete flight sequence
	// Initial heartbeat
	relay.handleHeartbeat(&common.MessageHeartbeat{CustomMode: 0}, "test-drone") // STABILIZE
	// GPS lock
	relay.handleGlobalPosition(&common.MessageGlobalPositionInt{Lat: 377749000, Lon: -122419400, Alt: 100500}, "test-drone")
	// Attitude data
	relay.handleAttitude(&common.MessageAttitude{Roll: 0.1, Pitch: -0.2, Yaw: 3.14}, "test-drone")
	// VFR HUD data
	relay.handleVfrHud(&common.MessageVfrHud{Groundspeed: 15.2, Alt: 100.5, Heading: 180}, "test-drone")
	// System status
	relay.handleSysStatus(&common.MessageSysStatus{BatteryRemaining: 85, VoltageBattery: 12600}, "test-drone")
	// Mode change to AUTO
	relay.handleHeartbeat(&common.MessageHeartbeat{CustomMode: 3}, "test-drone") // AUTO
	// Mission waypoint
	relay.handleGlobalPosition(&common.MessageGlobalPositionInt{Lat: 377750000, Lon: -122419500, Alt: 101000}, "test-drone")
	// Return to launch
	relay.handleHeartbeat(&common.MessageHeartbeat{CustomMode: 6}, "test-drone") // RTL
	// Landing
	relay.handleHeartbeat(&common.MessageHeartbeat{CustomMode: 9}, "test-drone") // LAND

	expectedMessages := 9

	// Verify all messages were processed
	mockSink := relay.sinks[0].(*mock.MockSink)
	if mockSink.GetMessageCount() != expectedMessages {
		t.Errorf("Expected %d messages, got %d", expectedMessages, mockSink.GetMessageCount())
	}

	// Verify message types
	messages := mockSink.GetMessages()
	expectedTypes := []string{"Heartbeat", "GlobalPositionInt", "Attitude", "VFR_HUD", "SystemStatus", "Heartbeat", "GlobalPositionInt", "Heartbeat", "Heartbeat"}

	for i, msg := range messages {
		if i < len(expectedTypes) {
			expected := expectedTypes[i]
			if msg.MsgName != expected {
				t.Errorf("Message %d: Expected type '%s', got '%s'", i, expected, msg.MsgName)
			}
		}
	}
}

// TestRelayErrorHandling tests that relay handles errors gracefully
func TestRelayErrorHandling(t *testing.T) {
	// Create a relay with a failing sink
	failingSink := &FailingSink{}
	relay := &Relay{
		sinks: []sinks.Sink{failingSink, mock.NewMockSink()},
	}

	// Send a message - one sink should fail, one should succeed
	heartbeat := &common.MessageHeartbeat{CustomMode: 3}
	relay.handleHeartbeat(heartbeat, "test-drone")

	// The relay should continue to work despite one sink failing
	position := &common.MessageGlobalPositionInt{Lat: 377749000, Lon: -122419400, Alt: 100500}
	relay.handleGlobalPosition(position, "test-drone")

	// Verify the working sink received both messages
	mockSink := relay.sinks[1].(*mock.MockSink)
	if mockSink.GetMessageCount() != 2 {
		t.Errorf("Expected 2 messages in working sink, got %d", mockSink.GetMessageCount())
	}
}

// FailingSink is a sink that always fails to write messages
type FailingSink struct {
	closed bool
}

func (f *FailingSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	if f.closed {
		return nil
	}
	return fmt.Errorf("simulated sink failure")
}

func (f *FailingSink) Close() error {
	f.closed = true
	return nil
}

// TestRelayPerformance tests relay performance with many messages
func TestRelayPerformance(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	numMessages := 1000
	start := time.Now()

	// Send many messages
	for i := 0; i < numMessages; i++ {
		heartbeat := &common.MessageHeartbeat{CustomMode: uint32(i % 10)}
		relay.handleHeartbeat(heartbeat, "test-drone")
	}

	duration := time.Since(start)
	mockSink := relay.sinks[0].(*mock.MockSink)

	// Verify all messages were processed
	if mockSink.GetMessageCount() != numMessages {
		t.Errorf("Expected %d messages, got %d", numMessages, mockSink.GetMessageCount())
	}

	// Performance should be reasonable (less than 1 second for 1000 messages)
	if duration > time.Second {
		t.Errorf("Processing %d messages took %v, which is too slow", numMessages, duration)
	}

	t.Logf("Processed %d messages in %v (%.2f messages/second)",
		numMessages, duration, float64(numMessages)/duration.Seconds())
}

// TestRelayConcurrentSources tests relay with multiple concurrent sources
func TestRelayConcurrentSources(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	numSources := 10
	messagesPerSource := 50
	done := make(chan bool, numSources)

	// Start concurrent message senders for different sources
	for sourceID := 0; sourceID < numSources; sourceID++ {
		go func(id int) {
			source := fmt.Sprintf("drone-%d", id)
			for i := 0; i < messagesPerSource; i++ {
				heartbeat := &common.MessageHeartbeat{CustomMode: uint32(i % 10)}
				relay.handleHeartbeat(heartbeat, source)
			}
			done <- true
		}(sourceID)
	}

	// Wait for all sources to finish
	for i := 0; i < numSources; i++ {
		<-done
	}

	// Verify all messages were processed
	mockSink := relay.sinks[0].(*mock.MockSink)
	expectedMessages := numSources * messagesPerSource
	if mockSink.GetMessageCount() != expectedMessages {
		t.Errorf("Expected %d messages, got %d", expectedMessages, mockSink.GetMessageCount())
	}

	// Verify messages from all sources
	messages := mockSink.GetMessages()
	sourceCounts := make(map[string]int)
	for _, msg := range messages {
		sourceCounts[msg.Source]++
	}

	if len(sourceCounts) != numSources {
		t.Errorf("Expected messages from %d sources, got %d", numSources, len(sourceCounts))
	}

	for source, count := range sourceCounts {
		if count != messagesPerSource {
			t.Errorf("Source %s: Expected %d messages, got %d", source, messagesPerSource, count)
		}
	}
}

// TestRelayMessageOrdering tests that messages maintain proper ordering
func TestRelayMessageOrdering(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	// Send messages with specific timestamps
	messages := []struct {
		timestamp time.Time
		mode      uint32
	}{
		{time.Now().Add(-5 * time.Second), 0}, // STABILIZE
		{time.Now().Add(-4 * time.Second), 3}, // AUTO
		{time.Now().Add(-3 * time.Second), 6}, // RTL
		{time.Now().Add(-2 * time.Second), 9}, // LAND
		{time.Now().Add(-1 * time.Second), 0}, // STABILIZE
	}

	for _, msg := range messages {
		heartbeat := &common.MessageHeartbeat{CustomMode: msg.mode}
		relay.handleHeartbeat(heartbeat, "test-drone")

		// Small delay to ensure different timestamps
		time.Sleep(1 * time.Millisecond)
	}

	// Verify messages were received in order
	mockSink := relay.sinks[0].(*mock.MockSink)
	if mockSink.GetMessageCount() != len(messages) {
		t.Errorf("Expected %d messages, got %d", len(messages), mockSink.GetMessageCount())
	}

	receivedMessages := mockSink.GetMessages()
	for i := 1; i < len(receivedMessages); i++ {
		prev := receivedMessages[i-1].TimestampRelay
		curr := receivedMessages[i].TimestampRelay
		if curr.Before(prev) {
			t.Errorf("Message %d timestamp %v is before previous message timestamp %v", i, curr, prev)
		}
		if receivedMessages[i].MsgName != "Heartbeat" {
			t.Errorf("Message %d: Expected Heartbeat message, got %s", i, receivedMessages[i].MsgName)
		}
	}
}
