package sinks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// NATSSink implements the Sink interface for NATS JetStream
type NATSSink struct {
	nc             *nats.Conn
	js             nats.JetStreamContext
	subjectPattern string
	streamName     string
	base           *BaseAsyncSink
}

// NewNATSSink creates a new NATS JetStream sink
func NewNATSSink(cfg config.NATSConfig) (*NATSSink, error) {
	// Build connection options
	opts := []nats.Option{
		nats.Timeout(10 * time.Second),
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1), // Unlimited reconnects
	}

	// Add authentication if provided
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}
	if cfg.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	}

	// Connect to NATS
	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to nats: %w", err)
	}

	// Create JetStream Context
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create jetstream context: %w", err)
	}

	sink := &NATSSink{
		nc:             nc,
		js:             js,
		subjectPattern: cfg.Subject,
	}

	// Create or update JetStream if configured
	if cfg.Stream != nil {
		if err := sink.ensureStream(cfg.Stream); err != nil {
			nc.Close()
			return nil, fmt.Errorf("failed to create jetstream: %w", err)
		}
		sink.streamName = cfg.Stream.Name
	}

	// Create worker function for async processing
	worker := func(msg telemetry.TelemetryEnvelope) error {
		return sink.publishMessage(msg)
	}

	// Initialize base async sink
	sink.base = NewBaseAsyncSink(cfg.QueueSize, cfg.BackpressurePolicy, "nats", worker)

	logger.Info("NATS JetStream sink initialized successfully",
		zap.String("url", cfg.URL),
		zap.String("subject_pattern", cfg.Subject),
		zap.String("stream", cfg.Stream.Name))
	return sink, nil
}

// WriteMessage implements the Sink interface
func (s *NATSSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	return s.base.Enqueue(msg)
}

// Close implements the Sink interface
func (s *NATSSink) Close(ctx context.Context) error {
	s.base.Close()
	s.nc.Drain()
	return nil
}

// ensureStream creates or updates a JetStream stream
func (s *NATSSink) ensureStream(cfg *config.StreamConfig) error {
	// Set defaults
	storage := nats.FileStorage
	if strings.ToLower(cfg.Storage) == "memory" {
		storage = nats.MemoryStorage
	}

	replicas := cfg.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	// Parse max age
	var maxAge time.Duration
	if cfg.MaxAge != "" {
		var err error
		maxAge, err = time.ParseDuration(cfg.MaxAge)
		if err != nil {
			return fmt.Errorf("invalid max_age format: %w", err)
		}
	}

	streamConfig := &nats.StreamConfig{
		Name:        cfg.Name,
		Subjects:    cfg.Subjects,
		Storage:     storage,
		Replicas:    replicas,
		MaxAge:      maxAge,
		MaxBytes:    cfg.MaxBytes,
		MaxMsgs:     cfg.MaxMsgs,
		Compression: nats.NoCompression,
	}

	if cfg.Compression {
		streamConfig.Compression = nats.S2Compression
	}

	// Try to get existing stream info
	_, err := s.js.StreamInfo(cfg.Name)
	if err != nil {
		// Stream doesn't exist, create it
		_, err = s.js.AddStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
		logger.Info("Created NATS stream",
			zap.String("name", cfg.Name),
			zap.Strings("subjects", cfg.Subjects),
			zap.String("storage", cfg.Storage))
	} else {
		// Stream exists, update it
		_, err = s.js.UpdateStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to update stream: %w", err)
		}
		logger.Info("Updated NATS stream",
			zap.String("name", cfg.Name),
			zap.Strings("subjects", cfg.Subjects))
	}

	return nil
}

// publishMessage publishes a telemetry message to NATS JetStream with entity-specific subjects
func (s *NATSSink) publishMessage(msg telemetry.TelemetryEnvelope) error {
	// Serialize message to JSON
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Resolve subject pattern with entity/drone ID
	subject := s.resolveSubject(msg)

	// Create NATS message with headers
	natsMsg := &nats.Msg{
		Subject: subject,
		Data:    jsonData,
		Header: nats.Header{
			"entity_id":    []string{msg.DroneID},
			"source":       []string{msg.Source},
			"message_type": []string{msg.MsgName},
			"timestamp":    []string{msg.TimestampRelay.Format(time.RFC3339Nano)},
		},
	}

	// Publish message to JetStream with ack
	ack, err := s.js.PublishMsg(natsMsg)
	if err != nil {
		return fmt.Errorf("failed to publish to jetstream: %w", err)
	}

	// Log successful publish with debug level
	logger.Debug("Published message to NATS",
		zap.String("subject", subject),
		zap.String("stream", ack.Stream),
		zap.Uint64("sequence", ack.Sequence),
		zap.String("entity_id", msg.DroneID),
		zap.String("message_type", msg.MsgName))

	return nil
}

// resolveSubject resolves subject pattern with entity information
func (s *NATSSink) resolveSubject(msg telemetry.TelemetryEnvelope) string {
	subject := s.subjectPattern

	// Replace placeholders with actual values
	// For 1:1 mode: constellation.telemetry.{entity_id}
	// For multi mode: constellation.telemetry.{org_id} (would need org_id from config)
	subject = strings.ReplaceAll(subject, "{entity_id}", msg.DroneID)
	subject = strings.ReplaceAll(subject, "{drone_id}", msg.DroneID)
	subject = strings.ReplaceAll(subject, "{source}", msg.Source)
	subject = strings.ReplaceAll(subject, "{message_type}", strings.ToLower(msg.MsgName))

	// For multi mode, you could replace {org_id} with organizational identifier
	// This would need to be passed in via configuration or derived from source
	subject = strings.ReplaceAll(subject, "{org_id}", "default_org")

	return subject
}
