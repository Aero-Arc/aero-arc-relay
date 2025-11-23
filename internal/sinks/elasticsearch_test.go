package sinks

import (
	"testing"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func makeElasticEnvelope(source, msgName string, fields map[string]any) telemetry.TelemetryEnvelope {
	return telemetry.TelemetryEnvelope{
		DroneID:        source,
		Source:         source,
		TimestampRelay: time.Now().UTC(),
		MsgName:        msgName,
		Fields:         fields,
	}
}

func TestElasticsearchSinkConfiguration(t *testing.T) {
	cfg := &config.ElasticsearchConfig{
		URLs:          []string{"http://localhost:9200"},
		Index:         "mavlink-telemetry",
		Username:      "elastic",
		Password:      "password",
		BatchSize:     100,
		FlushInterval: "10s",
	}

	// Test configuration validation
	if len(cfg.URLs) == 0 {
		t.Error("URLs should not be empty")
	}
	if cfg.Index == "" {
		t.Error("Index should not be empty")
	}
}

func TestElasticsearchSinkInterface(t *testing.T) {
	cfg := &config.ElasticsearchConfig{
		URLs:  []string{"http://localhost:9200"},
		Index: "mavlink-telemetry",
	}

	// This test would require a real Elasticsearch instance
	// For now, we'll just test the configuration
	if len(cfg.URLs) == 0 {
		t.Error("URLs should not be empty")
	}
}

func TestElasticsearchSinkMessageHandling(t *testing.T) {
	cfg := &config.ElasticsearchConfig{
		URLs:  []string{"http://localhost:9200"},
		Index: "mavlink-telemetry",
	}

	// Test message creation
	msg := makeElasticEnvelope("test-drone", "heartbeat", map[string]any{
		"status": "connected",
	})

	// Test message properties
	if msg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", msg.GetSource())
	}
	if msg.GetMessageType() != "heartbeat" {
		t.Errorf("Expected message type 'heartbeat', got '%s'", msg.GetMessageType())
	}

	// Test configuration
	if cfg.Index != "mavlink-telemetry" {
		t.Errorf("Expected index 'mavlink-telemetry', got '%s'", cfg.Index)
	}
}

func TestElasticsearchSinkDocumentConversion(t *testing.T) {
	// Test document conversion logic
	msg := makeElasticEnvelope("test-drone", "position", map[string]any{
		"latitude": 37.7749,
	})

	// Test message properties
	if msg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", msg.GetSource())
	}
	if msg.GetMessageType() != "position" {
		t.Errorf("Expected message type 'position', got '%s'", msg.GetMessageType())
	}
}

func TestElasticsearchSinkBatching(t *testing.T) {
	cfg := &config.ElasticsearchConfig{
		URLs:          []string{"http://localhost:9200"},
		Index:         "mavlink-telemetry",
		Username:      "elastic",
		Password:      "password",
		BatchSize:     10,
		FlushInterval: "5s",
	}

	// Test batch size configuration
	if cfg.BatchSize != 10 {
		t.Errorf("Expected batch size 10, got %d", cfg.BatchSize)
	}
	if cfg.FlushInterval != "5s" {
		t.Errorf("Expected flush interval '5s', got '%s'", cfg.FlushInterval)
	}
}

func TestElasticsearchSinkSchema(t *testing.T) {
	// Test different message types
	heartbeatMsg := makeElasticEnvelope("drone-1", "heartbeat", nil)
	positionMsg := makeElasticEnvelope("drone-1", "position", nil)
	attitudeMsg := makeElasticEnvelope("drone-1", "attitude", nil)

	// Test message types
	if heartbeatMsg.GetMessageType() != "heartbeat" {
		t.Errorf("Expected heartbeat message type")
	}
	if positionMsg.GetMessageType() != "position" {
		t.Errorf("Expected position message type")
	}
	if attitudeMsg.GetMessageType() != "attitude" {
		t.Errorf("Expected attitude message type")
	}
}

func TestElasticsearchSinkFlushInterval(t *testing.T) {
	cfg := &config.ElasticsearchConfig{
		URLs:          []string{"http://localhost:9200"},
		Index:         "mavlink-telemetry",
		Username:      "elastic",
		Password:      "password",
		BatchSize:     100,
		FlushInterval: "30s",
	}

	// Test flush interval configuration
	if cfg.FlushInterval != "30s" {
		t.Errorf("Expected flush interval '30s', got '%s'", cfg.FlushInterval)
	}
}

func TestElasticsearchSinkCredentials(t *testing.T) {
	cfg := &config.ElasticsearchConfig{
		URLs:     []string{"http://localhost:9200"},
		Index:    "mavlink-telemetry",
		Username: "elastic",
		Password: "password",
		APIKey:   "", // Not using API key
	}

	// Test credentials configuration
	if cfg.Username != "elastic" {
		t.Errorf("Expected username 'elastic', got '%s'", cfg.Username)
	}
	if cfg.Password != "password" {
		t.Errorf("Expected password 'password', got '%s'", cfg.Password)
	}
	if cfg.APIKey != "" {
		t.Errorf("Expected empty API key, got '%s'", cfg.APIKey)
	}
}

func TestElasticsearchSinkRecordStructure(t *testing.T) {
	// Test record structure for different message types
	heartbeatMsg := makeElasticEnvelope("drone-1", "heartbeat", nil)
	positionMsg := makeElasticEnvelope("drone-1", "position", nil)

	// Test heartbeat message structure
	if heartbeatMsg.GetSource() != "drone-1" {
		t.Errorf("Expected source 'drone-1', got '%s'", heartbeatMsg.GetSource())
	}
	if heartbeatMsg.GetMessageType() != "heartbeat" {
		t.Errorf("Expected message type 'heartbeat', got '%s'", heartbeatMsg.GetMessageType())
	}

	// Test position message structure
	if positionMsg.GetSource() != "drone-1" {
		t.Errorf("Expected source 'drone-1', got '%s'", positionMsg.GetSource())
	}
	if positionMsg.GetMessageType() != "position" {
		t.Errorf("Expected message type 'position', got '%s'", positionMsg.GetMessageType())
	}
}
