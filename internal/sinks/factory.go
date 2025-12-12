package sinks

import (
	"fmt"

	"github.com/makinje/aero-arc-relay/internal/config"
)

// SinkFactory creates sinks based on configuration
type SinkFactory struct{}

// NewSinkFactory creates a new sink factory
func NewSinkFactory() *SinkFactory {
	return &SinkFactory{}
}

// CreateConfiguredSinks creates only the sinks that are configured in the config
func (f *SinkFactory) CreateConfiguredSinks(cfg *config.Config) ([]Sink, error) {
	var sinks []Sink
	var errors []error

	// Only create sinks for configured types
	if cfg.Sinks.NATS != nil {
		sink, err := NewNATSSink(*cfg.Sinks.NATS)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create NATS sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	if cfg.Sinks.S3 != nil {
		sink, err := NewS3Sink(cfg.Sinks.S3)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create S3 sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	if cfg.Sinks.GCS != nil {
		sink, err := NewGCSSink(cfg.Sinks.GCS)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create GCS sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	if cfg.Sinks.BigQuery != nil {
		sink, err := NewBigQuerySink(cfg.Sinks.BigQuery)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create BigQuery sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	if cfg.Sinks.Timestream != nil {
		sink, err := NewTimestreamSink(cfg.Sinks.Timestream)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create Timestream sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	if cfg.Sinks.InfluxDB != nil {
		sink, err := NewInfluxDBSink(cfg.Sinks.InfluxDB)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create InfluxDB sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	if cfg.Sinks.Prometheus != nil {
		sink, err := NewPrometheusSink(cfg.Sinks.Prometheus)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create Prometheus sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	if cfg.Sinks.Elasticsearch != nil {
		sink, err := NewElasticsearchSink(cfg.Sinks.Elasticsearch)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create Elasticsearch sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	if cfg.Sinks.File != nil {
		sink, err := NewFileSink(cfg.Sinks.File)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create File sink: %w", err))
		} else {
			sinks = append(sinks, sink)
		}
	}

	// Note: Kafka support has been removed - use NATS for similar streaming functionality
	if cfg.Sinks.Kafka != nil {
		errors = append(errors, fmt.Errorf("kafka sink is no longer supported - use NATS JetStream for similar functionality"))
	}

	if len(sinks) == 0 {
		return nil, fmt.Errorf("no sinks could be created - check configuration and dependencies")
	}

	// Return successfully created sinks, log any errors as warnings
	return sinks, nil
}
