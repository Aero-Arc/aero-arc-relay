package config

import (
	"os"
	"testing"
	"time"
)

// TestConfigLoad tests loading configuration from YAML
func TestConfigLoad(t *testing.T) {
	// Create a temporary config file
	configContent := `
relay:
  buffer_size: 2000

mavlink:
  endpoints:
    - name: "drone-1"
      drone_id: "drone-1"
      protocol: "udp"
      mode: "1:1"
      port: 14550
    - name: "drone-2"
      drone_id: "drone-2"
      protocol: "tcp"
      mode: "1:1"
      port: 5760
    - name: "ground-station"
      drone_id: "ground-station"
      protocol: "serial"
      mode: "1:1"
      baud_rate: 57600

sinks:
  s3:
    bucket: "test-bucket"
    region: "us-west-2"
    access_key: "test-key"
    secret_key: "test-secret"
    prefix: "telemetry"
  
  kafka:
    brokers:
      - "localhost:9092"
      - "localhost:9093"
    topic: "telemetry-data"
  
  file:
    path: "/var/log/telemetry"
    format: "json"
    rotation_interval: "24h"

logging:
  level: "debug"
  format: "json"
  output: "file"
  file: "/var/log/aero-arc-relay/app.log"
`

	// Write config to temporary file
	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	// Load configuration
	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test relay configuration
	if cfg.Relay.BufferSize != 2000 {
		t.Errorf("Expected buffer size 2000, got %d", cfg.Relay.BufferSize)
	}

	// Test MAVLink configuration
	if cfg.MAVLink.Dialect == nil {
		t.Error("MAVLink dialect should be set")
	}

	if len(cfg.MAVLink.Endpoints) != 3 {
		t.Errorf("Expected 3 endpoints, got %d", len(cfg.MAVLink.Endpoints))
	}

	// Test endpoints
	expectedEndpoints := []struct {
		name     string
		droneID  string
		protocol MAVLinkEndpointProtocol
		mode     MAVLinkMode
		port     int
		baudRate int
	}{
		{"drone-1", "drone-1", MAVLinkEndpointProtocolUDP, MAVLinkMode1To1, 14550, 0},
		{"drone-2", "drone-2", MAVLinkEndpointProtocolTCP, MAVLinkMode1To1, 5760, 0},
		{"ground-station", "ground-station", MAVLinkEndpointProtocolSerial, MAVLinkMode1To1, 0, 57600},
	}

	for i, expected := range expectedEndpoints {
		endpoint := cfg.MAVLink.Endpoints[i]
		if endpoint.Name != expected.name {
			t.Errorf("Endpoint %d: Expected name '%s', got '%s'", i, expected.name, endpoint.Name)
		}
		if endpoint.DroneID != expected.droneID {
			t.Errorf("Endpoint %d: Expected drone ID '%s', got '%s'", i, expected.droneID, endpoint.DroneID)
		}
		if endpoint.Protocol != expected.protocol {
			t.Errorf("Endpoint %d: Expected protocol '%s', got '%s'", i, expected.protocol, endpoint.Protocol)
		}
		if endpoint.Mode != expected.mode {
			t.Errorf("Endpoint %d: Expected mode '%s', got '%s'", i, expected.mode, endpoint.Mode)
		}
		if endpoint.Port != expected.port {
			t.Errorf("Endpoint %d: Expected port %d, got %d", i, expected.port, endpoint.Port)
		}
		if endpoint.BaudRate != expected.baudRate {
			t.Errorf("Endpoint %d: Expected baud rate %d, got %d", i, expected.baudRate, endpoint.BaudRate)
		}
	}

	// Test S3 configuration
	if cfg.Sinks.S3 == nil {
		t.Error("S3 sink should be configured")
	} else {
		if cfg.Sinks.S3.Bucket != "test-bucket" {
			t.Errorf("Expected S3 bucket 'test-bucket', got '%s'", cfg.Sinks.S3.Bucket)
		}
		if cfg.Sinks.S3.Region != "us-west-2" {
			t.Errorf("Expected S3 region 'us-west-2', got '%s'", cfg.Sinks.S3.Region)
		}
		if cfg.Sinks.S3.Prefix != "telemetry" {
			t.Errorf("Expected S3 prefix 'telemetry', got '%s'", cfg.Sinks.S3.Prefix)
		}
	}

	// Test Kafka configuration
	if cfg.Sinks.Kafka == nil {
		t.Error("Kafka sink should be configured")
	} else {
		if len(cfg.Sinks.Kafka.Brokers) != 2 {
			t.Errorf("Expected 2 Kafka brokers, got %d", len(cfg.Sinks.Kafka.Brokers))
		}
		if cfg.Sinks.Kafka.Topic != "telemetry-data" {
			t.Errorf("Expected Kafka topic 'telemetry-data', got '%s'", cfg.Sinks.Kafka.Topic)
		}
	}

	// Test file configuration
	if cfg.Sinks.File == nil {
		t.Error("File sink should be configured")
	} else {
		if cfg.Sinks.File.Path != "/var/log/telemetry" {
			t.Errorf("Expected file path '/var/log/telemetry', got '%s'", cfg.Sinks.File.Path)
		}
		if cfg.Sinks.File.Format != "json" {
			t.Errorf("Expected file format 'json', got '%s'", cfg.Sinks.File.Format)
		}
		if cfg.Sinks.File.RotationInterval != 24*time.Hour {
			t.Errorf("Expected file rotation '24h', got '%s'", cfg.Sinks.File.RotationInterval)
		}
	}

	// Test logging configuration
	if cfg.Logging.Level != "debug" {
		t.Errorf("Expected log level 'debug', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Expected log format 'json', got '%s'", cfg.Logging.Format)
	}
	if cfg.Logging.Output != "file" {
		t.Errorf("Expected log output 'file', got '%s'", cfg.Logging.Output)
	}
	if cfg.Logging.File != "/var/log/aero-arc-relay/app.log" {
		t.Errorf("Expected log file '/var/log/aero-arc-relay/app.log', got '%s'", cfg.Logging.File)
	}
}

// TestConfigDefaults tests that default values are applied correctly
func TestConfigDefaults(t *testing.T) {
	// Create a minimal config file
	configContent := `
mavlink:
  endpoints:
    - name: "drone-1"
      drone_id: "drone-1"
      protocol: "udp"
      mode: "1:1"
      port: 14550

sinks:
  file:
    path: "/tmp/test"
    format: "json"
`

	// Write config to temporary file
	tmpFile, err := os.CreateTemp("", "test-config-minimal-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	// Load configuration
	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test default values
	if cfg.Relay.BufferSize != 1000 {
		t.Errorf("Expected default buffer size 1000, got %d", cfg.Relay.BufferSize)
	}

	if cfg.Logging.Level != "info" {
		t.Errorf("Expected default log level 'info', got '%s'", cfg.Logging.Level)
	}

	if cfg.Logging.Format != "text" {
		t.Errorf("Expected default log format 'text', got '%s'", cfg.Logging.Format)
	}

	if cfg.Logging.Output != "stdout" {
		t.Errorf("Expected default log output 'stdout', got '%s'", cfg.Logging.Output)
	}
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	// Test empty endpoints
	configContent := `
mavlink:
  endpoints: []

sinks:
  file:
    path: "/tmp/test"
    format: "json"
`

	tmpFile, err := os.CreateTemp("", "test-config-empty-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	if err == nil {
		t.Fatalf("Expected error for empty endpoint list")
	}
	if err != ErrNoEndpoints {
		t.Fatalf("Expected ErrNoEndpoints, got %v", err)
	}
}

// TestConfigFileNotFound tests handling of missing config file
func TestConfigFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Expected error for missing config file")
	}
}

// TestConfigInvalidYAML tests handling of invalid YAML
func TestConfigInvalidYAML(t *testing.T) {
	// Create a file with invalid YAML
	tmpFile, err := os.CreateTemp("", "test-config-invalid-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	invalidYAML := `
relay:
  buffer_size: 1000
mavlink:
  endpoints:
    - name: "drone-1"
      protocol: "udp"
      address: "192.168.1.100"
      port: 14550
invalid: yaml: content: [unclosed
`

	if _, err := tmpFile.WriteString(invalidYAML); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

// TestConfigEndpointTypes tests different endpoint configurations
func TestConfigEndpointTypes(t *testing.T) {
	configContent := `
mavlink:
  endpoints:
    - name: "udp-endpoint"
      drone_id: "udp-endpoint"
      protocol: "udp"
      mode: "1:1"
      port: 14550
    - name: "tcp-endpoint"
      drone_id: "tcp-endpoint"
      protocol: "tcp"
      mode: "1:1"
      port: 5760
    - name: "serial-endpoint"
      drone_id: "serial-endpoint"
      protocol: "serial"
      mode: "1:1"
      baud_rate: 57600

sinks:
  file:
    path: "/tmp/test"
    format: "json"
`

	tmpFile, err := os.CreateTemp("", "test-config-endpoints-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test UDP endpoint
	udpEndpoint := cfg.MAVLink.Endpoints[0]
	if udpEndpoint.Protocol != MAVLinkEndpointProtocolUDP {
		t.Errorf("Expected UDP protocol, got %s", udpEndpoint.Protocol)
	}
	if udpEndpoint.Mode != MAVLinkMode1To1 {
		t.Errorf("Expected UDP endpoint mode '%s', got '%s'", MAVLinkMode1To1, udpEndpoint.Mode)
	}
	if udpEndpoint.Port != 14550 {
		t.Errorf("Expected port 14550, got %d", udpEndpoint.Port)
	}

	// Test TCP endpoint
	tcpEndpoint := cfg.MAVLink.Endpoints[1]
	if tcpEndpoint.Protocol != MAVLinkEndpointProtocolTCP {
		t.Errorf("Expected TCP protocol, got %s", tcpEndpoint.Protocol)
	}
	if tcpEndpoint.Mode != MAVLinkMode1To1 {
		t.Errorf("Expected TCP endpoint mode '%s', got '%s'", MAVLinkMode1To1, tcpEndpoint.Mode)
	}
	if tcpEndpoint.Port != 5760 {
		t.Errorf("Expected port 5760, got %d", tcpEndpoint.Port)
	}

	// Test serial endpoint
	serialEndpoint := cfg.MAVLink.Endpoints[2]
	if serialEndpoint.Protocol != MAVLinkEndpointProtocolSerial {
		t.Errorf("Expected serial protocol, got %s", serialEndpoint.Protocol)
	}
	if serialEndpoint.Mode != MAVLinkMode1To1 {
		t.Errorf("Expected serial endpoint mode '%s', got '%s'", MAVLinkMode1To1, serialEndpoint.Mode)
	}
	if serialEndpoint.BaudRate != 57600 {
		t.Errorf("Expected baud rate 57600, got %d", serialEndpoint.BaudRate)
	}
}

// TestConfigDialects tests all supported MAVLink dialects
func TestConfigDialects(t *testing.T) {
	dialects := []string{
		"common",
		"minimal",
		"ardupilot",
		"ardupilotmega",
		"apm",
		"paparazzi",
		"standard",
		"all",
		"px4",
		"development",
	}

	for _, dialectName := range dialects {
		t.Run(dialectName, func(t *testing.T) {
			configContent := `
mavlink:
  dialect: "` + dialectName + `"
  endpoints:
    - name: "drone-1"
      drone_id: "drone-1"
      protocol: "udp"
      mode: "1:1"
      port: 14550

sinks:
  file:
    path: "/tmp/test"
    format: "json"
`
			tmpFile, err := os.CreateTemp("", "test-config-dialect-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(configContent); err != nil {
				t.Fatalf("Failed to write config: %v", err)
			}
			tmpFile.Close()

			cfg, err := Load(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to load config with dialect '%s': %v", dialectName, err)
			}

			if cfg.MAVLink.Dialect == nil {
				t.Errorf("Dialect '%s' should resolve to a non-nil Dialect", dialectName)
			}

			if cfg.MAVLink.DialectName != dialectName {
				t.Errorf("Expected dialect name '%s', got '%s'", dialectName, cfg.MAVLink.DialectName)
			}
		})
	}
}

// TestConfigInvalidDialect tests that invalid dialects are rejected
func TestConfigInvalidDialect(t *testing.T) {
	configContent := `
mavlink:
  dialect: "invalid-dialect"
  endpoints:
    - name: "drone-1"
      drone_id: "drone-1"
      protocol: "udp"
      mode: "1:1"
      port: 14550

sinks:
  file:
    path: "/tmp/test"
    format: "json"
`
	tmpFile, err := os.CreateTemp("", "test-config-invalid-dialect-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	if err == nil {
		t.Fatal("Expected error for invalid dialect")
	}
}
