/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestConfigLoad tests loading configuration from YAML.
func TestConfigLoad(t *testing.T) {
	configContent := `
registry:
  enabled: true
  address: "localhost:9090"
  relay_id: "relay-test"
  advertise_address: "relay-test.internal"
  heartbeat_interval: "7s"
  request_timeout: "3s"
  tls:
    enabled: true
    ca_file: "/run/secrets/registry-ca.pem"
    server_name: "registry.internal"

agent_auth:
  tokens:
    "agent-1": "test-agent-token"

control_auth:
  enabled: true
  client_ca_file: "/run/secrets/control-ca.pem"
  allowed_identities:
    - "spiffe://aero-arc/api"

telemetry:
  enabled: true
  backend: "influxdb3"
  relay_id: "relay-test"
  queue_capacity: 123
  workers: 3
  batch_size: 25
  flush_interval: "2s"
  enqueue_timeout: "50ms"
  write_timeout: "4s"
  influxdb:
    host: "http://localhost:8181"
    token: "test-token"
    database: "aero_arc_test"
  agent_mappings:
    "agent-1":
      operator_id: "operator-1"
      aircraft_id: "aircraft-1"

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
    include_messages:
      - "GlobalPositionInt"
  file:
    path: "/var/log/telemetry"
    prefix: "telemetry"
    format: "json"
    rotation_interval: "24h"
    include_messages:
      - "*"

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
	t.Cleanup(func() {
		if err := os.Remove(tmpFile.Name()); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove config fixture: %v", err)
		}
	})

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.Registry.Enabled {
		t.Error("Registry should be enabled")
	}
	if cfg.Registry.Address != "localhost:9090" {
		t.Errorf("Expected registry address 'localhost:9090', got '%s'", cfg.Registry.Address)
	}
	if cfg.Registry.RelayID != "relay-test" || cfg.Registry.AdvertiseAddress != "relay-test.internal" {
		t.Errorf("unexpected registry identity: %#v", cfg.Registry)
	}
	if cfg.Registry.HeartbeatInterval != 7*time.Second || cfg.Registry.RequestTimeout != 3*time.Second {
		t.Errorf("unexpected registry timing: %#v", cfg.Registry)
	}
	if !cfg.Registry.TLS.Enabled || cfg.Registry.TLS.CAFile == "" || cfg.Registry.TLS.ServerName != "registry.internal" {
		t.Errorf("unexpected registry TLS: %#v", cfg.Registry.TLS)
	}
	if cfg.AgentAuth.Tokens["agent-1"] != "test-agent-token" {
		t.Errorf("unexpected agent authentication config: %#v", cfg.AgentAuth)
	}
	if !cfg.ControlAuth.Enabled || cfg.ControlAuth.ClientCAFile != "/run/secrets/control-ca.pem" || len(cfg.ControlAuth.AllowedIdentities) != 1 {
		t.Errorf("unexpected control authentication config: %#v", cfg.ControlAuth)
	}
	if !cfg.Telemetry.Enabled {
		t.Error("Telemetry should be enabled")
	}
	if cfg.Telemetry.Backend != "influxdb3" {
		t.Errorf("Expected telemetry backend 'influxdb3', got '%s'", cfg.Telemetry.Backend)
	}
	if cfg.Telemetry.InfluxDB == nil || cfg.Telemetry.InfluxDB.Database != "aero_arc_test" {
		t.Fatalf("unexpected normalized InfluxDB config: %#v", cfg.Telemetry.InfluxDB)
	}
	if cfg.Telemetry.QueueCapacity != 123 || cfg.Telemetry.Workers != 3 || cfg.Telemetry.BatchSize != 25 {
		t.Errorf("unexpected telemetry writer sizing: %#v", cfg.Telemetry)
	}
	if cfg.Telemetry.MaxRetries == nil || *cfg.Telemetry.MaxRetries != 3 {
		t.Errorf("default telemetry max retries = %v, want 3", cfg.Telemetry.MaxRetries)
	}
	if cfg.Telemetry.AgentMappings["agent-1"].AircraftID != "aircraft-1" {
		t.Errorf("unexpected agent mapping: %#v", cfg.Telemetry.AgentMappings)
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
		if len(cfg.Sinks.Kafka.IncludeMessages) != 1 || cfg.Sinks.Kafka.IncludeMessages[0] != "GlobalPositionInt" {
			t.Errorf("Expected Kafka include_messages [GlobalPositionInt], got %#v", cfg.Sinks.Kafka.IncludeMessages)
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
		if len(cfg.Sinks.File.IncludeMessages) != 1 || cfg.Sinks.File.IncludeMessages[0] != "*" {
			t.Errorf("Expected file include_messages [*], got %#v", cfg.Sinks.File.IncludeMessages)
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

func TestRegistryConfigDefaultsAndTelemetryRelayIDFallback(t *testing.T) {
	configContent := `
registry:
  enabled: true
  address: "registry:50051"
  advertise_address: "relay.internal"
agent_auth:
  tokens:
    "agent-1": "test-agent-token"
telemetry:
  relay_id: "relay-from-telemetry"
sinks:
  file:
    path: "/tmp/test"
    format: "json"
`
	tmpFile, err := os.CreateTemp("", "test-config-registry-defaults-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(tmpFile.Name()); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove config fixture: %v", err)
		}
	})
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatal(err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Registry.RelayID != "relay-from-telemetry" {
		t.Fatalf("registry relay ID = %q", cfg.Registry.RelayID)
	}
	if cfg.Registry.HeartbeatInterval != 10*time.Second || cfg.Registry.RequestTimeout != 5*time.Second {
		t.Fatalf("registry defaults = %#v", cfg.Registry)
	}
}

func TestRegistryConfigRequiresRoutingIdentity(t *testing.T) {
	for name, registry := range map[string]string{
		"address":   "relay_id: relay-1\nadvertise_address: relay.internal",
		"relay ID":  "address: registry:50051\nadvertise_address: relay.internal",
		"advertise": "address: registry:50051\nrelay_id: relay-1",
	} {
		t.Run(name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "test-config-registry-invalid-*.yaml")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Remove(tmpFile.Name()); err != nil && !os.IsNotExist(err) {
					t.Errorf("remove config fixture: %v", err)
				}
			})
			content := "registry:\n  enabled: true\n  " + strings.ReplaceAll(registry, "\n", "\n  ") + "\nsinks:\n  file:\n    path: /tmp/test\n    format: json\n"
			if _, err := tmpFile.WriteString(content); err != nil {
				t.Fatal(err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(tmpFile.Name()); err == nil {
				t.Fatal("expected registry validation error")
			}
		})
	}
}

func TestRegistryConfigRequiresValidAgentCredentials(t *testing.T) {
	base := Config{
		Registry: RegistryConfig{
			Enabled: true, Address: "registry:50051", RelayID: "relay-1", AdvertiseAddress: "relay.internal",
		},
		AgentAuth: AgentAuthConfig{Tokens: map[string]string{"agent-1": "secret"}},
	}
	for name, tokens := range map[string]map[string]string{
		"missing":      nil,
		"empty token":  {"agent-1": ""},
		"padded token": {"agent-1": " secret "},
		"padded ID":    {" agent-1 ": "secret"},
	} {
		t.Run(name, func(t *testing.T) {
			config := base
			config.AgentAuth.Tokens = tokens
			if err := config.validateRegistry(); err == nil {
				t.Fatal("expected invalid Agent credential configuration")
			}
		})
	}
}

func TestControlAuthRequiresCAAndAllowedIdentity(t *testing.T) {
	valid := Config{ControlAuth: ControlAuthConfig{
		Enabled: true, ClientCAFile: "/run/secrets/control-ca.pem", AllowedIdentities: []string{"spiffe://aero-arc/api"},
	}}
	if err := valid.validateControlAuth(); err != nil {
		t.Fatalf("valid control authentication rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"missing CA":          func(c *Config) { c.ControlAuth.ClientCAFile = "" },
		"missing identities":  func(c *Config) { c.ControlAuth.AllowedIdentities = nil },
		"padded identity":     func(c *Config) { c.ControlAuth.AllowedIdentities = []string{" api "} },
		"duplicate identity":  func(c *Config) { c.ControlAuth.AllowedIdentities = []string{"api", "api"} },
		"padded CA file path": func(c *Config) { c.ControlAuth.ClientCAFile = " ca.pem " },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.ControlAuth.AllowedIdentities = append([]string(nil), valid.ControlAuth.AllowedIdentities...)
			mutate(&candidate)
			if err := candidate.validateControlAuth(); err == nil {
				t.Fatal("expected invalid control authentication configuration")
			}
		})
	}
}

func TestConfigPreservesExplicitZeroTelemetryRetries(t *testing.T) {
	configContent := `
telemetry:
  enabled: true
  backend: noop
  max_retries: 0
`
	tmpFile, err := os.CreateTemp("", "test-config-zero-retries-*.yaml")
	if err != nil {
		t.Fatalf("create temporary config: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(tmpFile.Name()); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove config fixture: %v", err)
		}
	})
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("write temporary config: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("close temporary config: %v", err)
	}

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Telemetry.MaxRetries == nil || *cfg.Telemetry.MaxRetries != 0 {
		t.Fatalf("telemetry max retries = %v, want explicit zero", cfg.Telemetry.MaxRetries)
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
	t.Cleanup(func() {
		if err := os.Remove(tmpFile.Name()); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove config fixture: %v", err)
		}
	})

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
	t.Cleanup(func() {
		if err := os.Remove(tmpFile.Name()); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove config fixture: %v", err)
		}
	})

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
