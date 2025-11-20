package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bluenviron/gomavlib/v2/pkg/dialect"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/all"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/ardupilotmega"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/development"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/minimal"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/paparazzi"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/standard"
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
	DialectName string            `yaml:"dialect"` // common, ardupilot, px4, etc.
	Dialect     *dialect.Dialect  `yaml:"-"`       // resolved at load time
	Endpoints   []MAVLinkEndpoint `yaml:"endpoints"`
}

// MAVLinkEndpoint represents a single MAVLink connection
type MAVLinkEndpoint struct {
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol"` // udp, tcp, serial
	Mode     string `yaml:"mode,omitempty"`
	Address  string `yaml:"address"`
	Port     int    `yaml:"port,omitempty"`
	BaudRate int    `yaml:"baud_rate,omitempty"`
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

// S3Config contains S3 sink configuration
type S3Config struct {
	Bucket             string        `yaml:"bucket"`
	Region             string        `yaml:"region"`
	AccessKey          string        `yaml:"access_key"`
	SecretKey          string        `yaml:"secret_key"`
	Prefix             string        `yaml:"prefix"`
	FlushInterval      time.Duration `yaml:"flush_interval"`
	QueueSize          int           `yaml:"queue_size"`
	BackpressurePolicy string        `yaml:"backpressure_policy"`
}

// GCSConfig contains Google Cloud Storage sink configuration
type GCSConfig struct {
	Bucket             string        `yaml:"bucket"`
	ProjectID          string        `yaml:"project_id"`
	Credentials        string        `yaml:"credentials"` // Path to service account JSON file
	Prefix             string        `yaml:"prefix"`
	FlushInterval      time.Duration `yaml:"flush_interval"` // How often to flush buffered data (e.g., "30s")
	QueueSize          int           `yaml:"queue_size"`
	BackpressurePolicy string        `yaml:"backpressure_policy"`
}

// BigQueryConfig contains BigQuery sink configuration
type BigQueryConfig struct {
	ProjectID          string `yaml:"project_id"`
	Dataset            string `yaml:"dataset"`
	Table              string `yaml:"table"`
	Credentials        string `yaml:"credentials"`    // Path to service account JSON file
	BatchSize          int    `yaml:"batch_size"`     // Number of messages to batch before insert
	FlushInterval      string `yaml:"flush_interval"` // How often to flush (e.g., "30s", "1m")
	QueueSize          int    `yaml:"queue_size"`
	BackpressurePolicy string `yaml:"backpressure_policy"`
}

// TimestreamConfig contains AWS Timestream sink configuration
type TimestreamConfig struct {
	Database           string `yaml:"database"`
	Table              string `yaml:"table"`
	Region             string `yaml:"region"`
	AccessKey          string `yaml:"access_key"`
	SecretKey          string `yaml:"secret_key"`
	SessionToken       string `yaml:"session_token,omitempty"` // For temporary credentials
	BatchSize          int    `yaml:"batch_size"`              // Number of records to batch
	FlushInterval      string `yaml:"flush_interval"`          // How often to flush (e.g., "30s", "1m")
	QueueSize          int    `yaml:"queue_size"`
	BackpressurePolicy string `yaml:"backpressure_policy"`
}

// InfluxDBConfig contains InfluxDB sink configuration
type InfluxDBConfig struct {
	URL                string `yaml:"url"`
	Database           string `yaml:"database"`
	Username           string `yaml:"username"`
	Password           string `yaml:"password"`
	Token              string `yaml:"token"`        // For InfluxDB 2.x
	Organization       string `yaml:"organization"` // For InfluxDB 2.x
	Bucket             string `yaml:"bucket"`       // For InfluxDB 2.x
	BatchSize          int    `yaml:"batch_size"`
	FlushInterval      string `yaml:"flush_interval"`
	QueueSize          int    `yaml:"queue_size"`
	BackpressurePolicy string `yaml:"backpressure_policy"`
}

// PrometheusConfig contains Prometheus sink configuration
type PrometheusConfig struct {
	URL                string `yaml:"url"`
	Job                string `yaml:"job"`
	Instance           string `yaml:"instance"`
	BatchSize          int    `yaml:"batch_size"`
	FlushInterval      string `yaml:"flush_interval"`
	QueueSize          int    `yaml:"queue_size"`
	BackpressurePolicy string `yaml:"backpressure_policy"`
}

// ElasticsearchConfig contains Elasticsearch sink configuration
type ElasticsearchConfig struct {
	URLs               []string `yaml:"urls"`
	Index              string   `yaml:"index"`
	Username           string   `yaml:"username"`
	Password           string   `yaml:"password"`
	APIKey             string   `yaml:"api_key"`
	BatchSize          int      `yaml:"batch_size"`
	FlushInterval      string   `yaml:"flush_interval"`
	QueueSize          int      `yaml:"queue_size"`
	BackpressurePolicy string   `yaml:"backpressure_policy"`
}

// KafkaConfig contains Kafka sink configuration
type KafkaConfig struct {
	Brokers            []string `yaml:"brokers"`
	Topic              string   `yaml:"topic"`
	QueueSize          int      `yaml:"queue_size"`
	BackpressurePolicy string   `yaml:"backpressure_policy"`
}

// FileConfig contains file-based sink configuration
type FileConfig struct {
	Path               string        `yaml:"path"`              // Path to the file, without the filename
	Prefix             string        `yaml:"prefix"`            // Prefix for the filename, will be appended to the path
	Format             string        `yaml:"format"`            // json, csv, binary
	RotationInterval   time.Duration `yaml:"rotation_interval"` // 24h, 1h, 10m, etc.
	QueueSize          int           `yaml:"queue_size"`
	BackpressurePolicy string        `yaml:"backpressure_policy"`
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

	dataStr := os.ExpandEnv(string(data))

	var config Config
	if err := yaml.Unmarshal([]byte(dataStr), &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if config.Relay.BufferSize == 0 {
		config.Relay.BufferSize = 1000
	}
	if config.MAVLink.DialectName == "" {
		config.MAVLink.DialectName = "common"
	}

	d, err := resolveDialect(config.MAVLink.DialectName)
	if err != nil {
		return nil, fmt.Errorf("invalid MAVLink dialect %q: %w", config.MAVLink.DialectName, err)
	}
	config.MAVLink.Dialect = d
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

// resolveDialect returns the gomavlib dialect for the provided name.
func resolveDialect(name string) (*dialect.Dialect, error) {
	switch strings.ToLower(name) {
	case "common":
		return common.Dialect, nil
	case "minimal":
		return minimal.Dialect, nil
	case "ardupilot", "ardupilotmega", "apm":
		return ardupilotmega.Dialect, nil
	case "paparazzi":
		return paparazzi.Dialect, nil
	case "standard":
		return standard.Dialect, nil
	case "all":
		return all.Dialect, nil
	case "development", "dev":
		return development.Dialect, nil
	default:
		return nil, fmt.Errorf("unsupported dialect")
	}
}
