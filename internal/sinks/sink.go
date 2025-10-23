package sinks

import (
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// Sink defines the interface for data sinks
type Sink interface {
	WriteMessage(msg telemetry.TelemetryMessage) error
	Close() error
}

// SinkType represents the type of sink
type SinkType string

const (
	SinkTypeS3    SinkType = "s3"
	SinkTypeKafka SinkType = "kafka"
	SinkTypeFile  SinkType = "file"
)
