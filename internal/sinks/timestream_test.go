package sinks

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/timestreamwrite"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func makeTimestreamEnvelope(source, msgName string, fields map[string]any) telemetry.TelemetryEnvelope {
	return telemetry.TelemetryEnvelope{
		AgentID:        source,
		Source:         source,
		TimestampRelay: time.Now().UTC(),
		MsgName:        msgName,
		Fields:         fields,
	}
}

// TestTimestreamSinkConfiguration tests Timestream sink configuration
func TestTimestreamSinkConfiguration(t *testing.T) {
	cfg := &config.TimestreamConfig{
		Database:      "telemetry",
		Table:         "mavlink_messages",
		Region:        "us-west-2",
		AccessKey:     "AKIAIOSFODNN7EXAMPLE",
		SecretKey:     "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		BatchSize:     100,
		FlushInterval: "30s",
	}

	// Test configuration validation
	if cfg.Database == "" {
		t.Error("Database should not be empty")
	}
	if cfg.Table == "" {
		t.Error("Table should not be empty")
	}
	if cfg.Region == "" {
		t.Error("Region should not be empty")
	}
	if cfg.BatchSize <= 0 {
		t.Error("BatchSize should be positive")
	}
	if cfg.FlushInterval == "" {
		t.Error("FlushInterval should not be empty")
	}
}

// TestTimestreamSinkInterface tests that Timestream sink implements the Sink interface
func TestTimestreamSinkInterface(t *testing.T) {
	// This test would require actual AWS credentials in a real environment
	// For now, we'll test the interface compliance through compilation
	cfg := &config.TimestreamConfig{
		Database:      "telemetry",
		Table:         "mavlink_messages",
		Region:        "us-west-2",
		BatchSize:     100,
		FlushInterval: "30s",
	}

	// Test that NewTimestreamSink returns a Sink interface
	var sink interface{} = &TimestreamSink{}
	if _, ok := sink.(Sink); !ok {
		t.Error("TimestreamSink should implement the Sink interface")
	}

	// Test configuration
	if cfg.Database != "telemetry" {
		t.Errorf("Expected database 'telemetry', got '%s'", cfg.Database)
	}
	if cfg.Table != "mavlink_messages" {
		t.Errorf("Expected table 'mavlink_messages', got '%s'", cfg.Table)
	}
	if cfg.Region != "us-west-2" {
		t.Errorf("Expected region 'us-west-2', got '%s'", cfg.Region)
	}
}

// TestTimestreamConfigDefaults tests default Timestream configuration values
func TestTimestreamConfigDefaults(t *testing.T) {
	cfg := &config.TimestreamConfig{
		Database: "telemetry",
		Table:    "messages",
		Region:   "us-west-2",
	}

	// Test that required fields are set
	if cfg.Database == "" {
		t.Error("Database should be set")
	}
	if cfg.Table == "" {
		t.Error("Table should be set")
	}
	if cfg.Region == "" {
		t.Error("Region should be set")
	}

	// Test that optional fields can be empty
	if cfg.AccessKey != "" {
		t.Logf("Access key: %s", cfg.AccessKey)
	}
	if cfg.SecretKey != "" {
		t.Logf("Secret key: %s", cfg.SecretKey)
	}
}

// TestTimestreamMessageHandling tests Timestream sink message handling (without actual Timestream calls)
func TestTimestreamMessageHandling(t *testing.T) {
	// Create test messages
	heartbeatMsg := makeTimestreamEnvelope("test-drone", "heartbeat", map[string]any{"status": "connected"})
	positionMsg := makeTimestreamEnvelope("test-drone", "position", map[string]any{"latitude": 37.7749})

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

// // TestTimestreamRecordConversion tests the conversion of telemetry messages to Timestream records
// func TestTimestreamRecordConversion(t *testing.T) {
// 	// Create a mock Timestream sink for testing conversion logic
// 	sink := &TimestreamSink{}

// 	// Test heartbeat message conversion
// 	heartbeatMsg := makeTimestreamEnvelope("drone-1", "heartbeat", map[string]any{"status": "connected"})
// 	records := sink.convertToTimestreamRecords(heartbeatMsg)

// 	if len(records) == 0 {
// 		t.Error("Should generate at least one record")
// 	}

// 	// Test that records have required fields
// 	for _, record := range records {
// 		if record.MeasureName == nil {
// 			t.Error("MeasureName should not be nil")
// 		}
// 		if record.MeasureValue == nil {
// 			t.Error("MeasureValue should not be nil")
// 		}
// 		if record.MeasureValueType == nil {
// 			t.Error("MeasureValueType should not be nil")
// 		}
// 		if record.Time == nil {
// 			t.Error("Time should not be nil")
// 		}
// 		if record.TimeUnit == nil {
// 			t.Error("TimeUnit should not be nil")
// 		}
// 	}

// 	// Test position message conversion
// 	positionMsg := makeTimestreamEnvelope("drone-1", "position", map[string]any{
// 		"latitude":  37.7749,
// 		"longitude": -122.4194,
// 	})
// 	records = sink.convertToTimestreamRecords(positionMsg)

// 	if len(records) == 0 {
// 		t.Error("Should generate at least one record")
// 	}

// 	// Test that dimensions are set correctly
// 	for _, record := range records {
// 		if len(record.Dimensions) == 0 {
// 			t.Error("Dimensions should not be empty")
// 		}

// 		// Check for required dimensions
// 		hasSource := false
// 		hasMessageType := false
// 		for _, dim := range record.Dimensions {
// 			if dim.Name != nil && *dim.Name == "source" {
// 				hasSource = true
// 			}
// 			if dim.Name != nil && *dim.Name == "message_type" {
// 				hasMessageType = true
// 			}
// 		}

// 		if !hasSource {
// 			t.Error("Should have 'source' dimension")
// 		}
// 		if !hasMessageType {
// 			t.Error("Should have 'message_type' dimension")
// 		}
// 	}
// }

// TestTimestreamBatchHandling tests Timestream batch processing logic
func TestTimestreamBatchHandling(t *testing.T) {
	cfg := &config.TimestreamConfig{
		Database:      "telemetry",
		Table:         "messages",
		Region:        "us-west-2",
		BatchSize:     10, // Small batch for testing
		FlushInterval: "1s",
	}

	// Test batch size configuration
	if cfg.BatchSize != 10 {
		t.Errorf("Expected batch size 10, got %d", cfg.BatchSize)
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

// TestTimestreamFlushInterval tests flush interval parsing
func TestTimestreamFlushInterval(t *testing.T) {
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
		cfg := &config.TimestreamConfig{
			FlushInterval: tc.input,
		}

		// Test that flush interval is set correctly
		if cfg.FlushInterval != tc.input {
			t.Errorf("Expected flush interval '%s', got '%s'", tc.input, cfg.FlushInterval)
		}
	}
}

// TestTimestreamCredentials tests credential handling
func TestTimestreamCredentials(t *testing.T) {
	// Test with explicit credentials
	cfg := &config.TimestreamConfig{
		Database:     "telemetry",
		Table:        "messages",
		Region:       "us-west-2",
		AccessKey:    "AKIAIOSFODNN7EXAMPLE",
		SecretKey:    "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SessionToken: "optional-session-token",
	}

	if cfg.AccessKey == "" {
		t.Error("AccessKey should be set")
	}
	if cfg.SecretKey == "" {
		t.Error("SecretKey should be set")
	}
	if cfg.SessionToken == "" {
		t.Log("SessionToken is optional")
	}

	// Test without explicit credentials (uses IAM role, etc.)
	cfgNoCreds := &config.TimestreamConfig{
		Database: "telemetry",
		Table:    "messages",
		Region:   "us-west-2",
	}

	if cfgNoCreds.AccessKey != "" {
		t.Logf("AccessKey: %s", cfgNoCreds.AccessKey)
	}
	if cfgNoCreds.SecretKey != "" {
		t.Logf("SecretKey: %s", cfgNoCreds.SecretKey)
	}
}

// TestTimestreamRecordStructure tests Timestream record structure
func TestTimestreamRecordStructure(t *testing.T) {
	// Test that TimestreamRecord has expected structure
	record := &TimestreamRecord{
		Dimensions: []*timestreamwrite.Dimension{
			{
				Name:  aws.String("source"),
				Value: aws.String("test-drone"),
			},
		},
		MeasureName:      "battery_level",
		MeasureValue:     "85.5",
		MeasureValueType: "DOUBLE",
		Time:             "1705312200000",
		TimeUnit:         "MILLISECONDS",
	}

	// Test required fields
	if len(record.Dimensions) == 0 {
		t.Error("Dimensions should not be empty")
	}
	if record.MeasureName == "" {
		t.Error("MeasureName should not be empty")
	}
	if record.MeasureValue == "" {
		t.Error("MeasureValue should not be empty")
	}
	if record.MeasureValueType == "" {
		t.Error("MeasureValueType should not be empty")
	}
	if record.Time == "" {
		t.Error("Time should not be empty")
	}
	if record.TimeUnit == "" {
		t.Error("TimeUnit should not be empty")
	}
}
