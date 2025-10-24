package config

import (
	"fmt"
	"os"

	"github.com/bluenviron/gomavlib/v2/pkg/dialect"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Relay   RelayConfig   `yaml:"relay"`
	MAVLink MAVLinkConfig `yaml:"mavlink"`
	Sinks   SinksConfig   `yaml:"sinks"`
	Logging LoggingConfig `yaml:"logging"`
}

// RelayConfig contains relay-specific configuration
type RelayConfig struct {
	BufferSize int `yaml:"buffer_size"`
}

// MAVLinkConfig contains MAVLink connection settings
type MAVLinkConfig struct {
	Dialect   *dialect.Dialect
	dialect   string            `yaml:"dialect"` // common, ardupilot, px4, etc.
	Endpoints []MAVLinkEndpoint `yaml:"endpoints"`
}

// MAVLinkEndpoint represents a single MAVLink connection
type MAVLinkEndpoint struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"` // udp, tcp, serial
	Address  string `yaml:"address"`
	Port     int    `yaml:"port,omitempty"`
	BaudRate int    `yaml:"baud_rate,omitempty"`
}

// SinksConfig contains configuration for all data sinks
type SinksConfig struct {
	S3         *S3Config         `yaml:"s3,omitempty"`
	GCS        *GCSConfig        `yaml:"gcs,omitempty"`
	BigQuery   *BigQueryConfig   `yaml:"bigquery,omitempty"`
	Timestream *TimestreamConfig `yaml:"timestream,omitempty"`
	Kafka      *KafkaConfig      `yaml:"kafka,omitempty"`
	File       *FileConfig       `yaml:"file,omitempty"`
}

// S3Config contains S3 sink configuration
type S3Config struct {
	Bucket    string `yaml:"bucket"`
	Region    string `yaml:"region"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Prefix    string `yaml:"prefix"`
}

// GCSConfig contains Google Cloud Storage sink configuration
type GCSConfig struct {
	Bucket      string `yaml:"bucket"`
	ProjectID   string `yaml:"project_id"`
	Credentials string `yaml:"credentials"` // Path to service account JSON file
	Prefix      string `yaml:"prefix"`
}

// BigQueryConfig contains BigQuery sink configuration
type BigQueryConfig struct {
	ProjectID     string `yaml:"project_id"`
	Dataset       string `yaml:"dataset"`
	Table         string `yaml:"table"`
	Credentials   string `yaml:"credentials"`    // Path to service account JSON file
	BatchSize     int    `yaml:"batch_size"`     // Number of messages to batch before insert
	FlushInterval string `yaml:"flush_interval"` // How often to flush (e.g., "30s", "1m")
}

// TimestreamConfig contains AWS Timestream sink configuration
type TimestreamConfig struct {
	Database      string `yaml:"database"`
	Table         string `yaml:"table"`
	Region        string `yaml:"region"`
	AccessKey     string `yaml:"access_key"`
	SecretKey     string `yaml:"secret_key"`
	SessionToken  string `yaml:"session_token,omitempty"` // For temporary credentials
	BatchSize     int    `yaml:"batch_size"`              // Number of records to batch
	FlushInterval string `yaml:"flush_interval"`          // How often to flush (e.g., "30s", "1m")
}

// KafkaConfig contains Kafka sink configuration
type KafkaConfig struct {
	Brokers []string `yaml:"brokers"`
	Topic   string   `yaml:"topic"`
}

// FileConfig contains file-based sink configuration
type FileConfig struct {
	Path     string `yaml:"path"`
	Format   string `yaml:"format"`   // json, csv, binary
	Rotation string `yaml:"rotation"` // daily, hourly, size-based
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"` // json, text
	Output string `yaml:"output"` // stdout, file
	File   string `yaml:"file,omitempty"`
}

// Load loads configuration from a YAML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if config.Relay.BufferSize == 0 {
		config.Relay.BufferSize = 1000
	}
	if config.MAVLink.dialect == "" {
		config.MAVLink.Dialect = common.Dialect
	}
	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	if config.Logging.Format == "" {
		config.Logging.Format = "text"
	}
	if config.Logging.Output == "" {
		config.Logging.Output = "stdout"
	}

	return &config, nil
}
