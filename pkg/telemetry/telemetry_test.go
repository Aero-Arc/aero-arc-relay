package telemetry

import (
	"testing"
)

func TestTelemetryMessageInterface(t *testing.T) {
	// Test HeartbeatMessage
	heartbeat := NewHeartbeatMessage("drone-1")
	heartbeat.Status = "connected"
	heartbeat.Mode = "AUTO"

	// Test interface methods
	if heartbeat.GetSource() != "drone-1" {
		t.Errorf("Expected source 'drone-1', got '%s'", heartbeat.GetSource())
	}

	if heartbeat.GetMessageType() != "heartbeat" {
		t.Errorf("Expected message type 'heartbeat', got '%s'", heartbeat.GetMessageType())
	}

	// Test JSON serialization
	jsonData, err := heartbeat.ToJSON()
	if err != nil {
		t.Errorf("Failed to serialize heartbeat to JSON: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("JSON data is empty")
	}

	// Test PositionMessage
	position := NewPositionMessage("drone-1")
	position.Latitude = 37.7749
	position.Longitude = -122.4194
	position.Altitude = 100.5

	if position.GetSource() != "drone-1" {
		t.Errorf("Expected source 'drone-1', got '%s'", position.GetSource())
	}

	if position.GetMessageType() != "position" {
		t.Errorf("Expected message type 'position', got '%s'", position.GetMessageType())
	}

	// Test JSON serialization
	jsonData, err = position.ToJSON()
	if err != nil {
		t.Errorf("Failed to serialize position to JSON: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("JSON data is empty")
	}
}

func TestMessageTypes(t *testing.T) {
	// Test all message types implement the interface
	var messages []TelemetryMessage

	heartbeat := NewHeartbeatMessage("test")
	heartbeat.Status = "connected"
	heartbeat.Mode = "AUTO"
	messages = append(messages, heartbeat)

	position := NewPositionMessage("test")
	position.Latitude = 37.7749
	position.Longitude = -122.4194
	position.Altitude = 100.5
	messages = append(messages, position)

	attitude := NewAttitudeMessage("test")
	attitude.Roll = 10.5
	attitude.Pitch = -5.2
	attitude.Yaw = 180.0
	messages = append(messages, attitude)

	vfrHud := NewVfrHudMessage("test")
	vfrHud.Speed = 15.2
	vfrHud.Altitude = 100.5
	vfrHud.Heading = 180.0
	messages = append(messages, vfrHud)

	battery := NewBatteryMessage("test")
	battery.Battery = 85.5
	battery.Voltage = 12.6
	messages = append(messages, battery)

	// Test that all messages implement the interface correctly
	for i, msg := range messages {
		if msg.GetSource() != "test" {
			t.Errorf("Message %d: Expected source 'test', got '%s'", i, msg.GetSource())
		}

		if msg.GetTimestamp().IsZero() {
			t.Errorf("Message %d: Timestamp is zero", i)
		}

		if msg.GetMessageType() == "" {
			t.Errorf("Message %d: Message type is empty", i)
		}

		// Test JSON serialization
		jsonData, err := msg.ToJSON()
		if err != nil {
			t.Errorf("Message %d: Failed to serialize to JSON: %v", i, err)
		}

		if len(jsonData) == 0 {
			t.Errorf("Message %d: JSON data is empty", i)
		}

		// Test binary serialization
		binaryData, err := msg.ToBinary()
		if err != nil {
			t.Errorf("Message %d: Failed to serialize to binary: %v", i, err)
		}

		if len(binaryData) == 0 {
			t.Errorf("Message %d: Binary data is empty", i)
		}
	}
}
