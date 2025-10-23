package sinks

import (
	"testing"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/mock"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// TestSinkInterface tests the basic sink interface functionality
func TestSinkInterface(t *testing.T) {
	mockSink := mock.NewMockSink()

	// Test WriteMessage
	heartbeatMsg := telemetry.NewHeartbeatMessage("test-drone")
	heartbeatMsg.Status = "connected"
	heartbeatMsg.Mode = "AUTO"

	err := mockSink.WriteMessage(heartbeatMsg)
	if err != nil {
		t.Errorf("Failed to write message: %v", err)
	}

	if mockSink.GetMessageCount() != 1 {
		t.Errorf("Expected 1 message, got %d", mockSink.GetMessageCount())
	}

	// Test Close
	err = mockSink.Close()
	if err != nil {
		t.Errorf("Failed to close sink: %v", err)
	}

	if !mockSink.IsClosed() {
		t.Error("Sink should be closed")
	}
}

// TestMultipleMessageTypes tests writing different message types to sink
func TestMultipleMessageTypes(t *testing.T) {
	mockSink := mock.NewMockSink()

	// Test HeartbeatMessage
	heartbeat := telemetry.NewHeartbeatMessage("drone-1")
	heartbeat.Status = "connected"
	heartbeat.Mode = "AUTO"
	err := mockSink.WriteMessage(heartbeat)
	if err != nil {
		t.Errorf("Failed to write heartbeat: %v", err)
	}

	// Test PositionMessage
	position := telemetry.NewPositionMessage("drone-1")
	position.Latitude = 37.7749
	position.Longitude = -122.4194
	position.Altitude = 100.5
	err = mockSink.WriteMessage(position)
	if err != nil {
		t.Errorf("Failed to write position: %v", err)
	}

	// Test AttitudeMessage
	attitude := telemetry.NewAttitudeMessage("drone-1")
	attitude.Roll = 10.5
	attitude.Pitch = -5.2
	attitude.Yaw = 180.0
	err = mockSink.WriteMessage(attitude)
	if err != nil {
		t.Errorf("Failed to write attitude: %v", err)
	}

	// Test VfrHudMessage
	vfrHud := telemetry.NewVfrHudMessage("drone-1")
	vfrHud.Speed = 15.2
	vfrHud.Altitude = 100.5
	vfrHud.Heading = 180.0
	err = mockSink.WriteMessage(vfrHud)
	if err != nil {
		t.Errorf("Failed to write VFR HUD: %v", err)
	}

	// Test BatteryMessage
	battery := telemetry.NewBatteryMessage("drone-1")
	battery.Battery = 85.5
	battery.Voltage = 12.6
	err = mockSink.WriteMessage(battery)
	if err != nil {
		t.Errorf("Failed to write battery: %v", err)
	}

	// Verify all messages were received
	if mockSink.GetMessageCount() != 5 {
		t.Errorf("Expected 5 messages, got %d", mockSink.GetMessageCount())
	}

	// Verify message types
	messages := mockSink.GetMessages()
	expectedTypes := []string{"heartbeat", "position", "attitude", "vfr_hud", "battery"}

	for i, msg := range messages {
		if msg.GetMessageType() != expectedTypes[i] {
			t.Errorf("Message %d: Expected type '%s', got '%s'", i, expectedTypes[i], msg.GetMessageType())
		}
	}
}

// TestConcurrentWrites tests that sinks can handle concurrent writes
func TestConcurrentWrites(t *testing.T) {
	mockSink := mock.NewMockSink()
	numMessages := 100
	done := make(chan bool, numMessages)

	// Send messages concurrently
	for i := 0; i < numMessages; i++ {
		go func(id int) {
			heartbeat := telemetry.NewHeartbeatMessage("test-drone")
			heartbeat.Status = "connected"
			heartbeat.Mode = "AUTO"

			err := mockSink.WriteMessage(heartbeat)
			if err != nil {
				t.Errorf("Failed to write message %d: %v", id, err)
			}
			done <- true
		}(i)
	}

	// Wait for all messages to be processed
	for i := 0; i < numMessages; i++ {
		<-done
	}

	if mockSink.GetMessageCount() != numMessages {
		t.Errorf("Expected %d messages, got %d", numMessages, mockSink.GetMessageCount())
	}
}

// TestSinkAfterClose tests that sinks handle writes after being closed
func TestSinkAfterClose(t *testing.T) {
	mockSink := mock.NewMockSink()

	// Write a message before closing
	heartbeat := telemetry.NewHeartbeatMessage("test-drone")
	err := mockSink.WriteMessage(heartbeat)
	if err != nil {
		t.Errorf("Failed to write message before close: %v", err)
	}

	// Close the sink
	err = mockSink.Close()
	if err != nil {
		t.Errorf("Failed to close sink: %v", err)
	}

	// Try to write after closing (should not error but also not store)
	position := telemetry.NewPositionMessage("test-drone")
	err = mockSink.WriteMessage(position)
	if err != nil {
		t.Errorf("Write after close should not error: %v", err)
	}

	// Should still only have 1 message (the one before close)
	if mockSink.GetMessageCount() != 1 {
		t.Errorf("Expected 1 message after close, got %d", mockSink.GetMessageCount())
	}
}

// TestFileSinkConfiguration tests file sink configuration
func TestFileSinkConfiguration(t *testing.T) {
	cfg := &config.FileConfig{
		Path:     "/tmp/test-telemetry",
		Format:   "json",
		Rotation: "daily",
	}

	// This test would require actual file system access
	// For now, we'll just test the configuration structure
	if cfg.Path != "/tmp/test-telemetry" {
		t.Errorf("Expected path '/tmp/test-telemetry', got '%s'", cfg.Path)
	}
	if cfg.Format != "json" {
		t.Errorf("Expected format 'json', got '%s'", cfg.Format)
	}
	if cfg.Rotation != "daily" {
		t.Errorf("Expected rotation 'daily', got '%s'", cfg.Rotation)
	}
}

// TestS3SinkConfiguration tests S3 sink configuration
func TestS3SinkConfiguration(t *testing.T) {
	cfg := &config.S3Config{
		Bucket:    "test-bucket",
		Region:    "us-west-2",
		AccessKey: "test-key",
		SecretKey: "test-secret",
		Prefix:    "telemetry",
	}

	if cfg.Bucket != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", cfg.Bucket)
	}
	if cfg.Region != "us-west-2" {
		t.Errorf("Expected region 'us-west-2', got '%s'", cfg.Region)
	}
	if cfg.Prefix != "telemetry" {
		t.Errorf("Expected prefix 'telemetry', got '%s'", cfg.Prefix)
	}
}

// TestKafkaSinkConfiguration tests Kafka sink configuration
func TestKafkaSinkConfiguration(t *testing.T) {
	cfg := &config.KafkaConfig{
		Brokers: []string{"localhost:9092", "localhost:9093"},
		Topic:   "telemetry-data",
	}

	if len(cfg.Brokers) != 2 {
		t.Errorf("Expected 2 brokers, got %d", len(cfg.Brokers))
	}
	if cfg.Brokers[0] != "localhost:9092" {
		t.Errorf("Expected first broker 'localhost:9092', got '%s'", cfg.Brokers[0])
	}
	if cfg.Topic != "telemetry-data" {
		t.Errorf("Expected topic 'telemetry-data', got '%s'", cfg.Topic)
	}
}

// TestMessageSerialization tests that messages can be serialized to JSON
func TestMessageSerialization(t *testing.T) {
	heartbeat := telemetry.NewHeartbeatMessage("test-drone")
	heartbeat.Status = "connected"
	heartbeat.Mode = "AUTO"

	jsonData, err := heartbeat.ToJSON()
	if err != nil {
		t.Errorf("Failed to serialize heartbeat to JSON: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("JSON data should not be empty")
	}

	// Test that JSON contains expected fields
	jsonStr := string(jsonData)
	if !contains(jsonStr, "test-drone") {
		t.Error("JSON should contain source 'test-drone'")
	}
	if !contains(jsonStr, "connected") {
		t.Error("JSON should contain status 'connected'")
	}
	if !contains(jsonStr, "AUTO") {
		t.Error("JSON should contain mode 'AUTO'")
	}
}

// TestMessageBinarySerialization tests that messages can be serialized to binary
func TestMessageBinarySerialization(t *testing.T) {
	position := telemetry.NewPositionMessage("test-drone")
	position.Latitude = 37.7749
	position.Longitude = -122.4194
	position.Altitude = 100.5

	binaryData, err := position.ToBinary()
	if err != nil {
		t.Errorf("Failed to serialize position to binary: %v", err)
	}

	if len(binaryData) == 0 {
		t.Error("Binary data should not be empty")
	}

	// Binary data should be larger than just the source string
	if len(binaryData) <= len("test-drone") {
		t.Error("Binary data should be larger than source string")
	}
}

// TestMessageTimestampConsistency tests that message timestamps are consistent
func TestMessageTimestampConsistency(t *testing.T) {
	before := time.Now()
	heartbeat := telemetry.NewHeartbeatMessage("test-drone")
	after := time.Now()

	timestamp := heartbeat.GetTimestamp()
	if timestamp.Before(before) || timestamp.After(after) {
		t.Errorf("Message timestamp %v is not within expected range [%v, %v]", timestamp, before, after)
	}
}

// TestMessageSourceConsistency tests that message sources are consistent
func TestMessageSourceConsistency(t *testing.T) {
	source := "test-drone"
	heartbeat := telemetry.NewHeartbeatMessage(source)

	if heartbeat.GetSource() != source {
		t.Errorf("Expected source '%s', got '%s'", source, heartbeat.GetSource())
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && contains(s[1:], substr)
}

// TestSinkTypeConstants tests that sink type constants are defined correctly
func TestSinkTypeConstants(t *testing.T) {
	if SinkTypeS3 != "s3" {
		t.Errorf("Expected SinkTypeS3 to be 's3', got '%s'", SinkTypeS3)
	}
	if SinkTypeKafka != "kafka" {
		t.Errorf("Expected SinkTypeKafka to be 'kafka', got '%s'", SinkTypeKafka)
	}
	if SinkTypeFile != "file" {
		t.Errorf("Expected SinkTypeFile to be 'file', got '%s'", SinkTypeFile)
	}
}
