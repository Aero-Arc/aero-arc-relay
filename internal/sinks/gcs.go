package sinks

import (
	"context"
	"fmt"
	"path/filepath"

	"cloud.google.com/go/storage"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/api/option"
)

// GCSSink implements Sink interface for Google Cloud Storage
type GCSSink struct {
	client *storage.Client
	bucket string
	prefix string
}

// NewGCSSink creates a new GCS sink
func NewGCSSink(cfg *config.GCSConfig) (*GCSSink, error) {
	ctx := context.Background()

	// Create client with credentials if provided
	var client *storage.Client
	var err error

	if cfg.Credentials != "" {
		// Use service account credentials file
		client, err = storage.NewClient(ctx, option.WithCredentialsFile(cfg.Credentials))
	} else {
		// Use default credentials (ADC, environment variables, etc.)
		client, err = storage.NewClient(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	return &GCSSink{
		client: client,
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

// WriteMessage uploads telemetry message to GCS
func (g *GCSSink) WriteMessage(msg telemetry.TelemetryMessage) error {
	ctx := context.Background()

	// Serialize message to JSON
	jsonData, err := msg.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Generate object name with timestamp and message type
	timestamp := msg.GetTimestamp().UTC()
	objectName := filepath.Join(g.prefix, timestamp.Format("2006/01/02"),
		fmt.Sprintf("%s_%s_%d.json", msg.GetSource(), msg.GetMessageType(), timestamp.Unix()))

	// Get bucket handle
	bucket := g.client.Bucket(g.bucket)
	obj := bucket.Object(objectName)

	// Create writer
	writer := obj.NewWriter(ctx)
	writer.ContentType = "application/json"
	writer.Metadata = map[string]string{
		"source":      msg.GetSource(),
		"messageType": msg.GetMessageType(),
		"timestamp":   timestamp.Format("2006-01-02T15:04:05Z"),
	}

	// Write data
	if _, err := writer.Write(jsonData); err != nil {
		writer.Close()
		return fmt.Errorf("failed to write to GCS: %w", err)
	}

	// Close writer to finalize upload
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close GCS writer: %w", err)
	}

	return nil
}

// Close closes the GCS sink
func (g *GCSSink) Close() error {
	if g.client != nil {
		return g.client.Close()
	}
	return nil
}
