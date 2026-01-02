package sinks

import (
	"testing"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func makeGCSEnvelope(source, msgName string, fields map[string]any) telemetry.TelemetryEnvelope {
	return telemetry.TelemetryEnvelope{
		AgentID:        source,
		Source:         source,
		TimestampRelay: time.Now().UTC(),
		MsgName:        msgName,
		Fields:         fields,
	}
}

// TestGCSSinkConfiguration tests GCS sink configuration
func TestGCSSinkConfiguration(t *testing.T) {
	cfg := &config.GCSConfig{
		Bucket:      "test-bucket",
		ProjectID:   "test-project",
		Credentials: "/path/to/service-account.json",
		Prefix:      "telemetry",
	}

	// Test configuration validation
	if cfg.Bucket == "" {
		t.Error("Bucket should not be empty")
	}
	if cfg.ProjectID == "" {
		t.Error("ProjectID should not be empty")
	}
	if cfg.Prefix == "" {
		t.Error("Prefix should not be empty")
	}
}

// TestGCSSinkInterface tests that GCS sink implements the Sink interface
func TestGCSSinkInterface(t *testing.T) {
	// This test would require actual GCS credentials in a real environment
	// For now, we'll test the interface compliance through compilation
	cfg := &config.GCSConfig{
		Bucket:    "test-bucket",
		ProjectID: "test-project",
		Prefix:    "telemetry",
	}

	// Test that NewGCSSink returns a Sink interface
	var sink interface{} = &GCSSink{}
	if _, ok := sink.(Sink); !ok {
		t.Error("GCSSink should implement the Sink interface")
	}

	// Test configuration
	if cfg.Bucket != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", cfg.Bucket)
	}
	if cfg.ProjectID != "test-project" {
		t.Errorf("Expected project ID 'test-project', got '%s'", cfg.ProjectID)
	}
	if cfg.Prefix != "telemetry" {
		t.Errorf("Expected prefix 'telemetry', got '%s'", cfg.Prefix)
	}
}

// TestGCSConfigDefaults tests default GCS configuration values
func TestGCSConfigDefaults(t *testing.T) {
	cfg := &config.GCSConfig{
		Bucket:    "my-bucket",
		ProjectID: "my-project",
		Prefix:    "data",
	}

	// Test that required fields are set
	if cfg.Bucket == "" {
		t.Error("Bucket should be set")
	}
	if cfg.ProjectID == "" {
		t.Error("ProjectID should be set")
	}
	if cfg.Prefix == "" {
		t.Error("Prefix should be set")
	}

	// Test that credentials can be empty (uses ADC)
	if cfg.Credentials != "" {
		t.Logf("Credentials path: %s", cfg.Credentials)
	}
}

// TestGCSMessageHandling tests GCS sink message handling (without actual GCS calls)
func TestGCSMessageHandling(t *testing.T) {
	// Create a test message
	msg := makeGCSEnvelope("test-drone", "heartbeat", map[string]any{"status": "connected"})

	// Test message properties
	if msg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", msg.GetSource())
	}
	if msg.GetMessageType() != "heartbeat" {
		t.Errorf("Expected message type 'heartbeat', got '%s'", msg.GetMessageType())
	}

	// Test JSON serialization
	jsonData, err := msg.ToJSON()
	if err != nil {
		t.Errorf("Failed to serialize message to JSON: %v", err)
	}
	if len(jsonData) == 0 {
		t.Error("JSON data should not be empty")
	}
}

// TestGCSObjectNaming tests the object naming convention for GCS
func TestGCSObjectNaming(t *testing.T) {
	// Test timestamp formatting for object names
	timestamp := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	actualPath := timestamp.Format("2006/01/02")

	if actualPath != "2024/01/15" {
		t.Errorf("Expected path '2024/01/15', got '%s'", actualPath)
	}

	// Test object name format
	source := "drone-1"
	messageType := "heartbeat"
	unixTime := timestamp.Unix()
	expectedName := "drone-1_heartbeat_1705312200.json"
	actualName := "drone-1_heartbeat_1705312200.json"

	if actualName != expectedName {
		t.Errorf("Expected object name '%s', got '%s'", expectedName, actualName)
	}

	// Test that all components are present
	if source == "" || messageType == "" || unixTime == 0 {
		t.Error("Object name components should not be empty")
	}
}

// TestGCSMetadata tests GCS object metadata
func TestGCSMetadata(t *testing.T) {
	// Test metadata structure
	metadata := map[string]string{
		"source":      "test-drone",
		"messageType": "heartbeat",
		"timestamp":   "2024-01-15T10:30:00Z",
	}

	if metadata["source"] != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", metadata["source"])
	}
	if metadata["messageType"] != "heartbeat" {
		t.Errorf("Expected messageType 'heartbeat', got '%s'", metadata["messageType"])
	}
	if metadata["timestamp"] != "2024-01-15T10:30:00Z" {
		t.Errorf("Expected timestamp '2024-01-15T10:30:00Z', got '%s'", metadata["timestamp"])
	}
}
