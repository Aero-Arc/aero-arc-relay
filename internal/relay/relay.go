package relay

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/mavlink"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// Relay manages MAVLink connections and data forwarding to sinks
type Relay struct {
	config *config.Config
	sinks  []sinks.Sink
	conns  []*mavlink.Connection
	mu     sync.RWMutex
}

// New creates a new relay instance
func New(cfg *config.Config) (*Relay, error) {
	relay := &Relay{
		config: cfg,
		sinks:  make([]sinks.Sink, 0),
		conns:  make([]*mavlink.Connection, 0),
	}

	// Initialize sinks
	if err := relay.initializeSinks(); err != nil {
		return nil, fmt.Errorf("failed to initialize sinks: %w", err)
	}

	// Initialize MAVLink connections
	if err := relay.initializeConnections(); err != nil {
		return nil, fmt.Errorf("failed to initialize connections: %w", err)
	}

	return relay, nil
}

// Start begins the relay operation
func (r *Relay) Start(ctx context.Context) error {
	log.Println("Starting aero-arc-relay...")

	// Start all MAVLink connections
	for _, conn := range r.conns {
		if err := conn.Start(ctx); err != nil {
			return fmt.Errorf("failed to start connection %s: %w", conn.Name(), err)
		}
	}

	// Wait for context cancellation
	<-ctx.Done()
	log.Println("Shutting down relay...")

	// Stop all connections
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, conn := range r.conns {
		conn.Stop()
	}

	// Close all sinks
	for _, sink := range r.sinks {
		sink.Close()
	}

	return nil
}

// initializeSinks sets up all configured data sinks
func (r *Relay) initializeSinks() error {
	// Initialize S3 sink if configured
	if r.config.Sinks.S3 != nil {
		s3Sink, err := sinks.NewS3Sink(r.config.Sinks.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 sink: %w", err)
		}
		r.sinks = append(r.sinks, s3Sink)
	}

	// Initialize Kafka sink if configured
	if r.config.Sinks.Kafka != nil {
		kafkaSink, err := sinks.NewKafkaSink(r.config.Sinks.Kafka)
		if err != nil {
			return fmt.Errorf("failed to create Kafka sink: %w", err)
		}
		r.sinks = append(r.sinks, kafkaSink)
	}

	// Initialize file sink if configured
	if r.config.Sinks.File != nil {
		fileSink, err := sinks.NewFileSink(r.config.Sinks.File)
		if err != nil {
			return fmt.Errorf("failed to create file sink: %w", err)
		}
		r.sinks = append(r.sinks, fileSink)
	}

	if len(r.sinks) == 0 {
		return fmt.Errorf("no sinks configured")
	}

	return nil
}

// initializeConnections sets up MAVLink connections
func (r *Relay) initializeConnections() error {
	for _, endpoint := range r.config.MAVLink.Endpoints {
		conn, err := mavlink.NewConnection(endpoint, r.handleTelemetry)
		if err != nil {
			return fmt.Errorf("failed to create connection for %s: %w", endpoint.Name, err)
		}
		r.conns = append(r.conns, conn)
	}

	if len(r.conns) == 0 {
		return fmt.Errorf("no MAVLink endpoints configured")
	}

	return nil
}

// handleTelemetry processes incoming telemetry data
func (r *Relay) handleTelemetry(data *telemetry.Data) {
	// Forward to all sinks
	for _, sink := range r.sinks {
		if err := sink.Write(data); err != nil {
			log.Printf("Failed to write to sink: %v", err)
		}
	}
}
