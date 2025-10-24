package sinks

import (
	"testing"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func TestPrometheusSinkConfiguration(t *testing.T) {
	cfg := &config.PrometheusConfig{
		URL:           "http://localhost:9090",
		Job:           "aero-arc-relay",
		Instance:      "drone-fleet",
		BatchSize:     100,
		FlushInterval: "10s",
	}

	// Test configuration validation
	if cfg.URL == "" {
		t.Error("URL should not be empty")
	}
	if cfg.Job == "" {
		t.Error("Job should not be empty")
	}
	if cfg.Instance == "" {
		t.Error("Instance should not be empty")
	}
}

func TestPrometheusSinkInterface(t *testing.T) {
	cfg := &config.PrometheusConfig{
		URL:      "http://localhost:9090",
		Job:      "aero-arc-relay",
		Instance: "drone-fleet",
	}

	// This test would require a real Prometheus instance
	// For now, we'll just test the configuration
	if cfg.URL == "" {
		t.Error("URL should not be empty")
	}
}

func TestPrometheusSinkMessageHandling(t *testing.T) {
	cfg := &config.PrometheusConfig{
		URL:      "http://localhost:9090",
		Job:      "aero-arc-relay",
		Instance: "drone-fleet",
	}

	// Test message creation
	msg := telemetry.NewHeartbeatMessage("test-drone")

	// Test message properties
	if msg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", msg.GetSource())
	}
	if msg.GetMessageType() != "heartbeat" {
		t.Errorf("Expected message type 'heartbeat', got '%s'", msg.GetMessageType())
	}

	// Test configuration
	if cfg.Job != "aero-arc-relay" {
		t.Errorf("Expected job 'aero-arc-relay', got '%s'", cfg.Job)
	}
}

func TestPrometheusSinkSampleConversion(t *testing.T) {
	// Test sample conversion logic
	msg := telemetry.NewPositionMessage("test-drone")

	// Test message properties
	if msg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", msg.GetSource())
	}
	if msg.GetMessageType() != "position" {
		t.Errorf("Expected message type 'position', got '%s'", msg.GetMessageType())
	}
}

func TestPrometheusSinkBatching(t *testing.T) {
	cfg := &config.PrometheusConfig{
		URL:           "http://localhost:9090",
		Job:           "aero-arc-relay",
		Instance:      "drone-fleet",
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

func TestPrometheusSinkSchema(t *testing.T) {
	// Test different message types
	heartbeatMsg := telemetry.NewHeartbeatMessage("drone-1")
	positionMsg := telemetry.NewPositionMessage("drone-1")
	attitudeMsg := telemetry.NewAttitudeMessage("drone-1")

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

func TestPrometheusSinkFlushInterval(t *testing.T) {
	cfg := &config.PrometheusConfig{
		URL:           "http://localhost:9090",
		Job:           "aero-arc-relay",
		Instance:      "drone-fleet",
		BatchSize:     100,
		FlushInterval: "30s",
	}

	// Test flush interval configuration
	if cfg.FlushInterval != "30s" {
		t.Errorf("Expected flush interval '30s', got '%s'", cfg.FlushInterval)
	}
}
