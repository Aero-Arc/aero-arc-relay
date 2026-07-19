// Package config loads YAML configuration and defines relay settings for
// logging, sinks, and runtime options.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Sinks       SinksConfig     `yaml:"sinks"`
	Registry    RegistryConfig  `yaml:"registry"`
	Telemetry   TelemetryConfig `yaml:"telemetry"`
	Logging     LoggingConfig   `yaml:"logging"`
	Debug       bool
	TLSCertPath string
	TLSKeyPath  string
	GrpcPort    int
	BufferSize  int
}

// SinksConfig contains configuration for all data sinks
type SinksConfig struct {
	S3            *S3Config            `yaml:"s3,omitempty"`
	GCS           *GCSConfig           `yaml:"gcs,omitempty"`
	BigQuery      *BigQueryConfig      `yaml:"bigquery,omitempty"`
	Timestream    *TimestreamConfig    `yaml:"timestream,omitempty"`
	InfluxDB      *InfluxDBConfig      `yaml:"influxdb,omitempty"`
	Prometheus    *PrometheusConfig    `yaml:"prometheus,omitempty"`
	Elasticsearch *ElasticsearchConfig `yaml:"elasticsearch,omitempty"`
	Kafka         *KafkaConfig         `yaml:"kafka,omitempty"`
	File          *FileConfig          `yaml:"file,omitempty"`
}

// MessageFilterConfig controls which MAVLink message names an output receives.
// Empty or omitted includes match nothing; use "*" to include all messages.
// Excludes take precedence over includes.
type MessageFilterConfig struct {
	IncludeMessages []string `yaml:"include_messages,omitempty"`
	ExcludeMessages []string `yaml:"exclude_messages,omitempty"`
}

// RegistryConfig contains control-plane registry reporting configuration.
type RegistryConfig struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
}

// TelemetryConfig contains normalized hot telemetry writer configuration.
type TelemetryConfig struct {
	Enabled        bool                      `yaml:"enabled"`
	Backend        string                    `yaml:"backend"`
	QueueCapacity  int                       `yaml:"queue_capacity"`
	Workers        int                       `yaml:"workers"`
	BatchSize      int                       `yaml:"batch_size"`
	FlushInterval  time.Duration             `yaml:"flush_interval"`
	EnqueueTimeout time.Duration             `yaml:"enqueue_timeout"`
	WriteTimeout   time.Duration             `yaml:"write_timeout"`
	MaxRetries     *int                      `yaml:"max_retries"`
	RetryBackoff   time.Duration             `yaml:"retry_backoff"`
	RelayID        string                    `yaml:"relay_id"`
	AgentMappings  map[string]AgentMapping   `yaml:"agent_mappings,omitempty"`
	InfluxDB       *NormalizedInfluxDBConfig `yaml:"influxdb,omitempty"`
}

type AgentMapping struct {
	OperatorID string `yaml:"operator_id"`
	AircraftID string `yaml:"aircraft_id"`
}

// NormalizedInfluxDBConfig configures the official InfluxDB 3 Core telemetry
// backend. It is intentionally separate from the generic InfluxDB sink.
type NormalizedInfluxDBConfig struct {
	Host     string `yaml:"host"`
	Token    string `yaml:"token"`
	Database string `yaml:"database"`
}

// S3Config contains S3 sink configuration
type S3Config struct {
	Bucket              string        `yaml:"bucket"`
	Region              string        `yaml:"region"`
	AccessKey           string        `yaml:"access_key"`
	SecretKey           string        `yaml:"secret_key"`
	Prefix              string        `yaml:"prefix"`
	FlushInterval       time.Duration `yaml:"flush_interval"`
	QueueSize           int           `yaml:"queue_size"`
	BackpressurePolicy  string        `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
}

// GCSConfig contains Google Cloud Storage sink configuration
type GCSConfig struct {
	Bucket              string        `yaml:"bucket"`
	ProjectID           string        `yaml:"project_id"`
	Credentials         string        `yaml:"credentials"` // Path to service account JSON file
	Prefix              string        `yaml:"prefix"`
	FlushInterval       time.Duration `yaml:"flush_interval"` // How often to flush buffered data (e.g., "30s")
	QueueSize           int           `yaml:"queue_size"`
	BackpressurePolicy  string        `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
}

// BigQueryConfig contains BigQuery sink configuration
type BigQueryConfig struct {
	ProjectID           string `yaml:"project_id"`
	Dataset             string `yaml:"dataset"`
	Table               string `yaml:"table"`
	Credentials         string `yaml:"credentials"`    // Path to service account JSON file
	BatchSize           int    `yaml:"batch_size"`     // Number of messages to batch before insert
	FlushInterval       string `yaml:"flush_interval"` // How often to flush (e.g., "30s", "1m")
	QueueSize           int    `yaml:"queue_size"`
	BackpressurePolicy  string `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
}

// TimestreamConfig contains AWS Timestream sink configuration
type TimestreamConfig struct {
	Database            string `yaml:"database"`
	Table               string `yaml:"table"`
	Region              string `yaml:"region"`
	AccessKey           string `yaml:"access_key"`
	SecretKey           string `yaml:"secret_key"`
	SessionToken        string `yaml:"session_token,omitempty"` // For temporary credentials
	BatchSize           int    `yaml:"batch_size"`              // Number of records to batch
	FlushInterval       string `yaml:"flush_interval"`          // How often to flush (e.g., "30s", "1m")
	QueueSize           int    `yaml:"queue_size"`
	BackpressurePolicy  string `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
}

// InfluxDBConfig contains InfluxDB sink configuration
type InfluxDBConfig struct {
	URL                 string `yaml:"url"`
	Database            string `yaml:"database"`
	Username            string `yaml:"username"`
	Password            string `yaml:"password"`
	Token               string `yaml:"token"`        // For InfluxDB 2.x
	Organization        string `yaml:"organization"` // For InfluxDB 2.x
	Bucket              string `yaml:"bucket"`       // For InfluxDB 2.x
	BatchSize           int    `yaml:"batch_size"`
	FlushInterval       string `yaml:"flush_interval"`
	QueueSize           int    `yaml:"queue_size"`
	BackpressurePolicy  string `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
}

// PrometheusConfig contains Prometheus sink configuration
type PrometheusConfig struct {
	URL                 string `yaml:"url"`
	Job                 string `yaml:"job"`
	Instance            string `yaml:"instance"`
	BatchSize           int    `yaml:"batch_size"`
	FlushInterval       string `yaml:"flush_interval"`
	QueueSize           int    `yaml:"queue_size"`
	BackpressurePolicy  string `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
}

// ElasticsearchConfig contains Elasticsearch sink configuration
type ElasticsearchConfig struct {
	URLs                []string `yaml:"urls"`
	Index               string   `yaml:"index"`
	Username            string   `yaml:"username"`
	Password            string   `yaml:"password"`
	APIKey              string   `yaml:"api_key"`
	BatchSize           int      `yaml:"batch_size"`
	FlushInterval       string   `yaml:"flush_interval"`
	QueueSize           int      `yaml:"queue_size"`
	BackpressurePolicy  string   `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
}

// KafkaConfig contains Kafka sink configuration
type KafkaConfig struct {
	Brokers             []string `yaml:"brokers"`
	Topic               string   `yaml:"topic"`
	QueueSize           int      `yaml:"queue_size"`
	BackpressurePolicy  string   `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
}

// FileConfig contains file-based sink configuration
type FileConfig struct {
	Path                string        `yaml:"path"`              // Path to the file, without the filename
	Prefix              string        `yaml:"prefix"`            // Prefix for the filename, will be appended to the path
	Format              string        `yaml:"format"`            // json, csv, binary
	RotationInterval    time.Duration `yaml:"rotation_interval"` // 24h, 1h, 10m, etc.
	QueueSize           int           `yaml:"queue_size"`
	BackpressurePolicy  string        `yaml:"backpressure_policy"`
	MessageFilterConfig `yaml:",inline"`
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
		return nil, fmt.Errorf("%w: %w", ErrFailedToReadConfigFile, err)
	}

	dataStr := os.ExpandEnv(string(data))

	var config Config
	if err := yaml.Unmarshal([]byte(dataStr), &config); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToParseConfigFile, err)
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
	if config.Telemetry.Enabled {
		if config.Telemetry.Backend == "" {
			config.Telemetry.Backend = "influxdb3"
		}
		if config.Telemetry.QueueCapacity <= 0 {
			config.Telemetry.QueueCapacity = 10_000
		}
		if config.Telemetry.Workers <= 0 {
			config.Telemetry.Workers = 2
		}
		if config.Telemetry.BatchSize <= 0 {
			config.Telemetry.BatchSize = 500
		}
		if config.Telemetry.FlushInterval <= 0 {
			config.Telemetry.FlushInterval = time.Second
		}
		if config.Telemetry.EnqueueTimeout <= 0 {
			config.Telemetry.EnqueueTimeout = 100 * time.Millisecond
		}
		if config.Telemetry.WriteTimeout <= 0 {
			config.Telemetry.WriteTimeout = 5 * time.Second
		}
		if config.Telemetry.MaxRetries == nil {
			defaultMaxRetries := 3
			config.Telemetry.MaxRetries = &defaultMaxRetries
		}
		if config.Telemetry.RetryBackoff <= 0 {
			config.Telemetry.RetryBackoff = 200 * time.Millisecond
		}
	}

	return &config, nil
}
