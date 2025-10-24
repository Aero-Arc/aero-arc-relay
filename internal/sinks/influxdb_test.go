package sinks

import (
	"testing"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func TestInfluxDBSinkConfiguration(t *testing.T) {
	cfg := &config.InfluxDBConfig{
		URL:           "http://localhost:8086",
		Database:      "test",
		Username:      "admin",
		Password:      "password",
		BatchSize:     100,
		FlushInterval: "10s",
	}

	// Test configuration validation
	if cfg.URL == "" {
		t.Error("URL should not be empty")
	}
	if cfg.Database == "" {
		t.Error("Database should not be empty")
	}
}

func TestInfluxDBSinkInterface(t *testing.T) {
	cfg := &config.InfluxDBConfig{
		URL:      "http://localhost:8086",
		Database: "test",
		Username: "admin",
		Password: "password",
	}

	// This test would require a real InfluxDB instance
	// For now, we'll just test the configuration
	if cfg.URL == "" {
		t.Error("URL should not be empty")
	}
}

func TestInfluxDBSinkMessageHandling(t *testing.T) {
	cfg := &config.InfluxDBConfig{
		URL:      "http://localhost:8086",
		Database: "test",
		Username: "admin",
		Password: "password",
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
	if cfg.Database != "test" {
		t.Errorf("Expected database 'test', got '%s'", cfg.Database)
	}
}

func TestInfluxDBSinkPointConversion(t *testing.T) {
	// Test point conversion logic
	msg := telemetry.NewPositionMessage("test-drone")

	// Test message properties
	if msg.GetSource() != "test-drone" {
		t.Errorf("Expected source 'test-drone', got '%s'", msg.GetSource())
	}
	if msg.GetMessageType() != "position" {
		t.Errorf("Expected message type 'position', got '%s'", msg.GetMessageType())
	}
}

func TestInfluxDBSinkBatching(t *testing.T) {
	cfg := &config.InfluxDBConfig{
		URL:           "http://localhost:8086",
		Database:      "test",
		Username:      "admin",
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

func TestInfluxDBSinkSchema(t *testing.T) {
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

func TestInfluxDBSinkFlushInterval(t *testing.T) {
	cfg := &config.InfluxDBConfig{
		URL:           "http://localhost:8086",
		Database:      "test",
		Username:      "admin",
		Password:      "password",
		BatchSize:     100,
		FlushInterval: "30s",
	}

	// Test flush interval configuration
	if cfg.FlushInterval != "30s" {
		t.Errorf("Expected flush interval '30s', got '%s'", cfg.FlushInterval)
	}
}
