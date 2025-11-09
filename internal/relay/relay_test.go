package relay

import (
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
					Protocol: "udp",
					Address:  "127.0.0.1",
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
					Protocol: "udp",
					Address:  "127.0.0.1",
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
	heartbeatMsg := telemetry.NewHeartbeatMessage("test-drone")
	heartbeatMsg.Status = "connected"
	heartbeatMsg.Mode = "AUTO"

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

	if receivedMsg.GetMessageType() != "heartbeat" {
		t.Errorf("Expected message type 'heartbeat', got '%s'", receivedMsg.GetMessageType())
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
	if msg.GetMessageType() != "heartbeat" {
		t.Errorf("Expected heartbeat message type, got %s", msg.GetMessageType())
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

	// Test system status handler
	sysStatus := &common.MessageSysStatus{
		BatteryRemaining: 85,
		VoltageBattery:   12600, // 12.6V in mV
	}
	relay.handleSysStatus(sysStatus, "test-drone")

	if mockSink.GetMessageCount() != 5 {
		t.Errorf("Expected 5 messages after sys status, got %d", mockSink.GetMessageCount())
	}
}

// TestMessageTypeSpecificData tests that message handlers create correct message types
func TestMessageTypeSpecificData(t *testing.T) {
	relay := &Relay{
		sinks: []sinks.Sink{mock.NewMockSink()},
	}

	// Test heartbeat creates HeartbeatMessage
	heartbeat := &common.MessageHeartbeat{
		CustomMode: 3,
	}
	relay.handleHeartbeat(heartbeat, "test-drone")

	mockSink := relay.sinks[0].(*mock.MockSink)
	msg := mockSink.GetMessages()[0]

	if heartbeatMsg, ok := msg.(*telemetry.HeartbeatMessage); ok {
		if heartbeatMsg.Status != "connected" {
			t.Errorf("Expected status 'connected', got '%s'", heartbeatMsg.Status)
		}
		if heartbeatMsg.Mode != "AUTO" {
			t.Errorf("Expected mode 'AUTO', got '%s'", heartbeatMsg.Mode)
		}
	} else {
		t.Error("Expected HeartbeatMessage type")
	}

	// Test position creates PositionMessage
	position := &common.MessageGlobalPositionInt{
		Lat: 377749000,
		Lon: -1224194000,
		Alt: 100500,
	}
	relay.handleGlobalPosition(position, "test-drone")

	msg = mockSink.GetMessages()[1]
	if positionMsg, ok := msg.(*telemetry.PositionMessage); ok {
		if positionMsg.Latitude != 37.7749 {
			t.Errorf("Expected latitude 37.7749, got %f", positionMsg.Latitude)
		}
		if positionMsg.Longitude != -122.4194 {
			t.Errorf("Expected longitude -122.4194, got %f", positionMsg.Longitude)
		}
		if positionMsg.Altitude != 100.5 {
			t.Errorf("Expected altitude 100.5, got %f", positionMsg.Altitude)
		}
	} else {
		t.Error("Expected PositionMessage type")
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
		sink.Close()
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
