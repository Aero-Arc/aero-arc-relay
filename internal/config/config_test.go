package config

import (
	"os"
	"testing"
	"time"
)

// TestConfigLoad tests loading configuration from YAML.
func TestConfigLoad(t *testing.T) {
	configContent := `
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
    prefix: "telemetry"
    format: "json"
    rotation_interval: "24h"

logging:
  level: "debug"
  format: "json"
  output: "file"
  file: "/var/log/aero-arc-relay/app.log"
`

	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
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

// TestConfigDefaults tests that default values are applied correctly.
func TestConfigDefaults(t *testing.T) {
	configContent := `
sinks:
  file:
    path: "/tmp/test"
    format: "json"
`

	tmpFile, err := os.CreateTemp("", "test-config-minimal-*.yaml")
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

// TestConfigFileNotFound tests handling of missing config file.
func TestConfigFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Expected error for missing config file")
	}
}

// TestConfigInvalidYAML tests handling of invalid YAML.
func TestConfigInvalidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-config-invalid-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	invalidYAML := `
sinks:
  file:
    path: "/tmp/test"
    format: "json"
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
