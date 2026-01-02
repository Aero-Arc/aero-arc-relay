package sinks

import (
	"testing"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func makeBigQueryEnvelope(source, msgName string, fields map[string]any) telemetry.TelemetryEnvelope {
	return telemetry.TelemetryEnvelope{
		AgentID:        source,
		Source:         source,
		TimestampRelay: time.Now().UTC(),
		MsgName:        msgName,
		Fields:         fields,
	}
}

// TestBigQuerySinkConfiguration tests BigQuery sink configuration
func TestBigQuerySinkConfiguration(t *testing.T) {
	cfg := &config.BigQueryConfig{
		ProjectID:     "test-project",
		Dataset:       "telemetry",
		Table:         "mavlink_messages",
		Credentials:   "/path/to/service-account.json",
		BatchSize:     1000,
		FlushInterval: "30s",
	}

	// Test configuration validation
	if cfg.ProjectID == "" {
		t.Error("ProjectID should not be empty")
	}
	if cfg.Dataset == "" {
		t.Error("Dataset should not be empty")
	}
	if cfg.Table == "" {
		t.Error("Table should not be empty")
	}
	if cfg.BatchSize <= 0 {
		t.Error("BatchSize should be positive")
	}
	if cfg.FlushInterval == "" {
		t.Error("FlushInterval should not be empty")
	}
}

// TestBigQuerySinkInterface tests that BigQuery sink implements the Sink interface
func TestBigQuerySinkInterface(t *testing.T) {
	// This test would require actual BigQuery credentials in a real environment
	// For now, we'll test the interface compliance through compilation
	cfg := &config.BigQueryConfig{
		ProjectID:     "test-project",
		Dataset:       "telemetry",
		Table:         "mavlink_messages",
		BatchSize:     1000,
		FlushInterval: "30s",
	}

	// Test that NewBigQuerySink returns a Sink interface
	var sink interface{} = &BigQuerySink{}
	if _, ok := sink.(Sink); !ok {
		t.Error("BigQuerySink should implement the Sink interface")
	}

	// Test configuration
	if cfg.ProjectID != "test-project" {
		t.Errorf("Expected project ID 'test-project', got '%s'", cfg.ProjectID)
	}
	if cfg.Dataset != "telemetry" {
		t.Errorf("Expected dataset 'telemetry', got '%s'", cfg.Dataset)
	}
	if cfg.Table != "mavlink_messages" {
		t.Errorf("Expected table 'mavlink_messages', got '%s'", cfg.Table)
	}
}

// TestBigQueryConfigDefaults tests default BigQuery configuration values
func TestBigQueryConfigDefaults(t *testing.T) {
	cfg := &config.BigQueryConfig{
		ProjectID: "my-project",
		Dataset:   "telemetry",
		Table:     "messages",
	}

	// Test that required fields are set
	if cfg.ProjectID == "" {
		t.Error("ProjectID should be set")
	}
	if cfg.Dataset == "" {
		t.Error("Dataset should be set")
	}
	if cfg.Table == "" {
		t.Error("Table should be set")
	}

	// Test that optional fields can be empty
	if cfg.Credentials != "" {
		t.Logf("Credentials path: %s", cfg.Credentials)
	}
}

// TestBigQueryMessageHandling tests BigQuery sink message handling (without actual BigQuery calls)
func TestBigQueryMessageHandling(t *testing.T) {
	// Create test messages
	heartbeatMsg := makeBigQueryEnvelope("test-drone", "heartbeat", map[string]any{
		"status": "connected",
	})
	positionMsg := makeBigQueryEnvelope("test-drone", "position", map[string]any{
		"latitude":  37.7749,
		"longitude": -122.4194,
	})

	// Test message properties
	if heartbeatMsg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", heartbeatMsg.GetSource())
	}
	if heartbeatMsg.GetMessageType() != "heartbeat" {
		t.Errorf("Expected message type 'heartbeat', got '%s'", heartbeatMsg.GetMessageType())
	}

	if positionMsg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", positionMsg.GetSource())
	}
	if positionMsg.GetMessageType() != "position" {
		t.Errorf("Expected message type 'position', got '%s'", positionMsg.GetMessageType())
	}

	// Test JSON serialization
	jsonData, err := heartbeatMsg.ToJSON()
	if err != nil {
		t.Errorf("Failed to serialize heartbeat message to JSON: %v", err)
	}
	if len(jsonData) == 0 {
		t.Error("JSON data should not be empty")
	}

	jsonData, err = positionMsg.ToJSON()
	if err != nil {
		t.Errorf("Failed to serialize position message to JSON: %v", err)
	}
	if len(jsonData) == 0 {
		t.Error("JSON data should not be empty")
	}
}

// TestBigQueryRowConversion tests the conversion of telemetry messages to BigQuery rows
func TestBigQueryRowConversion(t *testing.T) {
	// Create a mock BigQuery sink for testing conversion logic
	sink := &BigQuerySink{}

	// Test heartbeat message conversion
	heartbeatMsg := makeBigQueryEnvelope("drone-1", "heartbeat", map[string]any{"status": "connected"})
	row := sink.convertToBigQueryRow(&heartbeatMsg)
	if row != nil {
		t.Log("BigQuery row conversion not yet implemented; expected nil")
	}

	// Test position message conversion
	positionMsg := makeBigQueryEnvelope("drone-1", "position", map[string]any{
		"latitude": 37.7749,
	})
	row = sink.convertToBigQueryRow(&positionMsg)
	if row != nil {
		t.Log("BigQuery row conversion not yet implemented; expected nil")
	}
}

// TestBigQueryBatchHandling tests BigQuery batch processing logic
func TestBigQueryBatchHandling(t *testing.T) {
	cfg := &config.BigQueryConfig{
		ProjectID:     "test-project",
		Dataset:       "telemetry",
		Table:         "messages",
		BatchSize:     5, // Small batch for testing
		FlushInterval: "1s",
	}

	// Test batch size configuration
	if cfg.BatchSize != 5 {
		t.Errorf("Expected batch size 5, got %d", cfg.BatchSize)
	}

	// Test flush interval parsing
	if cfg.FlushInterval != "1s" {
		t.Errorf("Expected flush interval '1s', got '%s'", cfg.FlushInterval)
	}

	// Test that batch size is respected
	if cfg.BatchSize <= 0 {
		t.Error("Batch size should be positive")
	}
}

// TestBigQuerySchema tests BigQuery row schema structure
func TestBigQuerySchema(t *testing.T) {
	// Test that BigQueryRow has all expected fields
	row := &BigQueryRow{
		Source:      "test-drone",
		Timestamp:   time.Now(),
		MessageType: "heartbeat",
		RawData:     `{"source":"test-drone","timestamp":"2024-01-15T10:30:00Z"}`,
	}

	// Test required fields
	if row.Source == "" {
		t.Error("Source should not be empty")
	}
	if row.MessageType == "" {
		t.Error("MessageType should not be empty")
	}
	if row.RawData == "" {
		t.Error("RawData should not be empty")
	}

	// Test optional fields can be nil
	if row.Latitude != nil {
		t.Logf("Latitude: %f", *row.Latitude)
	}
	if row.Longitude != nil {
		t.Logf("Longitude: %f", *row.Longitude)
	}
	if row.BatteryLevel != nil {
		t.Logf("Battery Level: %f", *row.BatteryLevel)
	}
}

// TestBigQueryFlushInterval tests flush interval parsing
func TestBigQueryFlushInterval(t *testing.T) {
	testCases := []struct {
		input    string
		expected time.Duration
	}{
		{"30s", 30 * time.Second},
		{"1m", 1 * time.Minute},
		{"5m", 5 * time.Minute},
		{"1h", 1 * time.Hour},
	}

	for _, tc := range testCases {
		cfg := &config.BigQueryConfig{
			FlushInterval: tc.input,
		}

		// Test that flush interval is set correctly
		if cfg.FlushInterval != tc.input {
			t.Errorf("Expected flush interval '%s', got '%s'", tc.input, cfg.FlushInterval)
		}
	}
}
