package relay

import (
	"context"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/mock"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// TestRelayCreation tests the creation of a new relay instance
func TestRelayCreation(t *testing.T) {
	cfg := &config.Config{
		Relay: config.RelayConfig{
			BufferSize: 1000,
		},
		MAVLink: config.MAVLinkConfig{
			Dialect: common.Dialect,
			Endpoints: []config.MAVLinkEndpoint{
				{
					Name:     "test-drone",
					AgentID:  "test-drone",
					Protocol: "udp",
					Port:     14550,
				},
			},
		},
		Sinks: config.SinksConfig{},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}

	// Test with no sinks (should fail)
	_, err := New(cfg)
	if err == nil {
		t.Error("Expected error when no sinks are configured")
	}

	// Test with mock sink
	cfg.Sinks.File = &config.FileConfig{
		Path:             "/tmp/test",
		Format:           "json",
		RotationInterval: 24 * time.Hour,
	}

	relay, err := New(cfg)
	if err != nil {
		t.Errorf("Failed to create relay: %v", err)
	}

	if relay == nil {
		t.Error("Relay should not be nil")
	}

	if len(relay.sinks) == 0 {
		t.Error("Relay should have sinks configured")
	}
}

// TestRelayWithMockSink tests relay functionality with a mock sink
func TestRelayWithMockSink(t *testing.T) {
	// Create a test configuration
	cfg := &config.Config{
		Relay: config.RelayConfig{
			BufferSize: 1000,
		},
		MAVLink: config.MAVLinkConfig{
			Dialect: common.Dialect,
			Endpoints: []config.MAVLinkEndpoint{
				{
					Name:     "test-drone",
					AgentID:  "test-drone",
					Protocol: "udp",
					Port:     14550,
				},
			},
		},
		Sinks: config.SinksConfig{},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}

	// Create relay with mock sink
	relay := &Relay{
		config: cfg,
		sinks:  []sinks.Sink{mock.NewMockSink()},
	}

	// Test message handling
	heartbeatMsg := telemetry.BuildHeartbeatEnvelope("test-drone", &common.MessageHeartbeat{
		CustomMode: 3,
	})

	relay.handleTelemetryMessage(heartbeatMsg)
	// Verify message was processed
	mockSink := relay.sinks[0].(*mock.MockSink)
	if mockSink.GetMessageCount() != 1 {
		t.Errorf("Expected 1 message, got %d", mockSink.GetMessageCount())
	}

	receivedMsg := mockSink.GetMessages()[0]
	if receivedMsg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", receivedMsg.GetSource())
	}

	if receivedMsg.GetMessageType() != "Heartbeat" {
		t.Errorf("Expected message type 'Heartbeat', got '%s'", receivedMsg.GetMessageType())
	}
}

// TestFlightModeConversion tests the flight mode conversion function
func TestFlightModeConversion(t *testing.T) {
	relay := &Relay{}

	testCases := []struct {
		mode     uint32
		expected string
	}{
		{0, "STABILIZE"},
		{1, "ACRO"},
		{2, "ALT_HOLD"},
		{3, "AUTO"},
		{4, "GUIDED"},
		{5, "LOITER"},
		{6, "RTL"},
		{7, "CIRCLE"},
		{8, "POSITION"},
		{9, "LAND"},
		{10, "OF_LOITER"},
		{11, "DRIFT"},
		{13, "SPORT"},
		{14, "FLIP"},
		{15, "AUTOTUNE"},
		{16, "POSHOLD"},
		{17, "BRAKE"},
		{18, "THROW"},
		{19, "AVOID_ADSB"},
		{20, "GUIDED_NOGPS"},
		{21, "SMART_RTL"},
		{22, "FLOWHOLD"},
		{23, "FOLLOW"},
		{24, "ZIGZAG"},
		{25, "SYSTEMID"},
		{26, "AUTOROTATE"},
		{27, "AUTO_RTL"},
		{999, "UNKNOWN"},
	}

	for _, tc := range testCases {
		result := relay.getFlightMode(tc.mode)
		if result != tc.expected {
			t.Errorf("For mode %d, expected '%s', got '%s'", tc.mode, tc.expected, result)
		}
	}
}

// TestMessageHandlers tests individual message handlers
func TestMessageHandlers(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	// Test heartbeat handler
	heartbeat := &common.MessageHeartbeat{
		CustomMode: 3, // AUTO mode
	}
	relay.handleHeartbeat(heartbeat, "test-drone")

	mockSink := relay.sinks[0].(*mock.MockSink)
	if mockSink.GetMessageCount() != 1 {
		t.Errorf("Expected 1 message after heartbeat, got %d", mockSink.GetMessageCount())
	}

	msg := mockSink.GetMessages()[0]
	if msg.GetMessageType() != "Heartbeat" {
		t.Errorf("Expected heartbeat message type, got %s", msg.GetMessageType())
	}
	if _, ok := msg.Fields["type"]; !ok {
		t.Error("Expected heartbeat envelope to include type field")
	}

	// Test position handler
	position := &common.MessageGlobalPositionInt{
		Lat: 377749000,  // 37.7749 degrees
		Lon: -122419400, // -122.4194 degrees
		Alt: 100500,     // 100.5 meters
	}
	relay.handleGlobalPosition(position, "test-drone")

	if mockSink.GetMessageCount() != 2 {
		t.Errorf("Expected 2 messages after position, got %d", mockSink.GetMessageCount())
	}

	msg = mockSink.GetMessages()[mockSink.GetMessageCount()-1]
	if msg.MsgName != "GlobalPositionInt" {
		t.Errorf("Expected GlobalPositionInt message, got %s", msg.MsgName)
	}
	if _, ok := msg.Fields["latitude"]; !ok {
		t.Error("Expected position envelope to include latitude field")
	}

	// Test attitude handler
	attitude := &common.MessageAttitude{
		Roll:  0.1,  // ~5.7 degrees
		Pitch: -0.2, // ~-11.5 degrees
		Yaw:   3.14, // ~180 degrees
	}
	relay.handleAttitude(attitude, "test-drone")

	if mockSink.GetMessageCount() != 3 {
		t.Errorf("Expected 3 messages after attitude, got %d", mockSink.GetMessageCount())
	}

	msg = mockSink.GetMessages()[mockSink.GetMessageCount()-1]
	if msg.MsgName != "Attitude" {
		t.Errorf("Expected Attitude message, got %s", msg.MsgName)
	}
	if _, ok := msg.Fields["roll"]; !ok {
		t.Error("Expected attitude envelope to include roll field")
	}

	// Test VFR HUD handler
	vfrHud := &common.MessageVfrHud{
		Groundspeed: 15.2,
		Alt:         100.5,
		Heading:     180,
	}
	relay.handleVfrHud(vfrHud, "test-drone")

	if mockSink.GetMessageCount() != 4 {
		t.Errorf("Expected 4 messages after VFR HUD, got %d", mockSink.GetMessageCount())
	}

	msg = mockSink.GetMessages()[mockSink.GetMessageCount()-1]
	if msg.MsgName != "VFR_HUD" {
		t.Errorf("Expected VFR_HUD message, got %s", msg.MsgName)
	}
	if _, ok := msg.Fields["ground_speed"]; !ok {
		t.Error("Expected VFR_HUD envelope to include ground_speed field")
	}

	// Test system status handler
	sysStatus := &common.MessageSysStatus{
		BatteryRemaining: 85,
		VoltageBattery:   12600, // 12.6V in mV
	}
	relay.handleSysStatus(sysStatus, "test-drone")

	if mockSink.GetMessageCount() != 5 {
		t.Errorf("Expected 5 messages after sys status, got %d", mockSink.GetMessageCount())
	}

	msg = mockSink.GetMessages()[mockSink.GetMessageCount()-1]
	if msg.MsgName != "SystemStatus" {
		t.Errorf("Expected SystemStatus message, got %s", msg.MsgName)
	}
	if _, ok := msg.Fields["battery_remaining"]; !ok {
		t.Error("Expected system status envelope to include battery_remaining field")
	}
}

// TestMessageTypeSpecificData tests that message handlers create correct message types
func TestMessageTypeSpecificData(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	// Test heartbeat creates Heartbeat envelope with basic metadata
	heartbeat := &common.MessageHeartbeat{
		CustomMode: 3,
	}
	relay.handleHeartbeat(heartbeat, "test-drone")

	mockSink := relay.sinks[0].(*mock.MockSink)
	msg := mockSink.GetMessages()[0]

	if msg.MsgName != "Heartbeat" {
		t.Fatalf("Expected Heartbeat message, got %s", msg.MsgName)
	}
	if msg.Source != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", msg.Source)
	}
	if _, ok := msg.Fields["type"]; !ok {
		t.Error("Expected heartbeat envelope to contain type field")
	}

	// Test position creates GlobalPositionInt envelope with raw values
	position := &common.MessageGlobalPositionInt{
		Lat: 377749000,
		Lon: -122419400,
		Alt: 100500,
	}
	relay.handleGlobalPosition(position, "test-drone")

	msg = mockSink.GetMessages()[1]
	if msg.MsgName != "GlobalPositionInt" {
		t.Fatalf("Expected GlobalPositionInt message, got %s", msg.MsgName)
	}
	if msg.Source != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", msg.Source)
	}

	if lat, ok := msg.Fields["latitude"].(int32); !ok || lat != position.Lat {
		t.Errorf("Expected latitude %d, got %v", position.Lat, msg.Fields["latitude"])
	}
	if lon, ok := msg.Fields["longitude"].(int32); !ok || lon != position.Lon {
		t.Errorf("Expected longitude %d, got %v", position.Lon, msg.Fields["longitude"])
	}
	if alt, ok := msg.Fields["altitude"].(int32); !ok || alt != position.Alt {
		t.Errorf("Expected altitude %d, got %v", position.Alt, msg.Fields["altitude"])
	}
}

// TestMultipleSinks tests that messages are sent to all configured sinks
func TestMultipleSinks(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink(), mock.NewMockSink(), mock.NewMockSink()},
	}

	heartbeat := &common.MessageHeartbeat{
		CustomMode: 3,
	}
	relay.handleHeartbeat(heartbeat, "test-drone")

	// Check that all sinks received the message
	for i, sink := range relay.sinks {
		mockSink := sink.(*mock.MockSink)
		if mockSink.GetMessageCount() != 1 {
			t.Errorf("Sink %d: Expected 1 message, got %d", i, mockSink.GetMessageCount())
		}
	}
}

// TestRelayShutdown tests that relay shuts down gracefully
func TestRelayShutdown(t *testing.T) {
	cfg := &config.Config{
		Relay: config.RelayConfig{
			BufferSize: 1000,
		},
		MAVLink: config.MAVLinkConfig{
			Dialect:   common.Dialect,
			Endpoints: []config.MAVLinkEndpoint{},
		},
		Sinks: config.SinksConfig{},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}

	relay := &Relay{
		config: cfg,
		sinks:  []sinks.Sink{mock.NewMockSink()},
	}

	// Test that sinks are closed on shutdown
	mockSink := relay.sinks[0].(*mock.MockSink)
	if mockSink.IsClosed() {
		t.Error("Sink should not be closed initially")
	}

	relay.Close()

	if !mockSink.IsClosed() {
		t.Error("Sink should be closed after relay shutdown")
	}
}

// TestRelayClose tests the Close method
func (r *Relay) Close() {
	// Close all sinks
	for _, sink := range r.sinks {
		sink.Close(context.Background())
	}
}

// TestConcurrentMessageHandling tests that the relay can handle messages concurrently
func TestConcurrentMessageHandling(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	// Send multiple messages concurrently
	numMessages := 100
	done := make(chan bool, numMessages)

	for i := 0; i < numMessages; i++ {
		go func(id int) {
			heartbeat := &common.MessageHeartbeat{
				CustomMode: uint32(id % 10),
			}
			relay.handleHeartbeat(heartbeat, "test-drone")
			done <- true
		}(i)
	}

	// Wait for all messages to be processed
	for i := 0; i < numMessages; i++ {
		<-done
	}

	mockSink := relay.sinks[0].(*mock.MockSink)
	if mockSink.GetMessageCount() != numMessages {
		t.Errorf("Expected %d messages, got %d", numMessages, mockSink.GetMessageCount())
	}
}

// TestMessageTimestamp tests that messages have correct timestamps
func TestMessageTimestamp(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	before := time.Now()
	heartbeat := &common.MessageHeartbeat{
		CustomMode: 3,
	}
	relay.handleHeartbeat(heartbeat, "test-drone")
	after := time.Now()

	mockSink := relay.sinks[0].(*mock.MockSink)
	msg := mockSink.GetMessages()[0]
	timestamp := msg.GetTimestamp()

	if timestamp.Before(before) || timestamp.After(after) {
		t.Errorf("Message timestamp %v is not within expected range [%v, %v]", timestamp, before, after)
	}
}
