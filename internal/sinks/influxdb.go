package sinks

import (
	"context"
	"fmt"
	"sync"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// InfluxDBSink implements Sink interface for InfluxDB
type InfluxDBSink struct {
	client        influxdb2.Client
	writeAPI      api.WriteAPI
	batchSize     int
	flushInterval time.Duration
	buffer        []telemetry.TelemetryMessage
	mu            sync.Mutex
	lastFlush     time.Time
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewInfluxDBSink creates a new InfluxDB sink
func NewInfluxDBSink(cfg *config.InfluxDBConfig) (*InfluxDBSink, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create InfluxDB client
	var client influxdb2.Client
	var writeAPI api.WriteAPI

	if cfg.Token != "" {
		// InfluxDB 2.x with token authentication
		client = influxdb2.NewClient(cfg.URL, cfg.Token)
		writeAPI = client.WriteAPI(cfg.Organization, cfg.Bucket)
	} else {
		// InfluxDB 1.x with username/password
		client = influxdb2.NewClient(cfg.URL, fmt.Sprintf("%s:%s", cfg.Username, cfg.Password))
		writeAPI = client.WriteAPI("", cfg.Database)
	}

	// Parse flush interval
	flushInterval := 30 * time.Second // Default
	if cfg.FlushInterval != "" {
		if parsed, err := time.ParseDuration(cfg.FlushInterval); err == nil {
			flushInterval = parsed
		}
	}

	// Set batch size
	batchSize := 1000 // Default
	if cfg.BatchSize > 0 {
		batchSize = cfg.BatchSize
	}

	sink := &InfluxDBSink{
		client:        client,
		writeAPI:      writeAPI,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		buffer:        make([]telemetry.TelemetryMessage, 0, batchSize),
		lastFlush:     time.Now(),
		ctx:           ctx,
		cancel:        cancel,
	}

	// Start background flusher
	go sink.backgroundFlusher()

	return sink, nil
}

// WriteMessage adds a telemetry message to the batch
func (i *InfluxDBSink) WriteMessage(msg telemetry.TelemetryMessage) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Add message to buffer
	i.buffer = append(i.buffer, msg)

	// Flush if batch size reached
	if len(i.buffer) >= i.batchSize {
		return i.flushUnsafe()
	}

	return nil
}

// flushUnsafe flushes the buffer to InfluxDB (must be called with lock held)
func (i *InfluxDBSink) flushUnsafe() error {
	if len(i.buffer) == 0 {
		return nil
	}

	// Convert messages to InfluxDB points
	points := make([]*write.Point, 0, len(i.buffer))

	for _, msg := range i.buffer {
		point := i.convertToInfluxPoint(msg)
		points = append(points, point)
	}

	// Write points to InfluxDB
	for _, point := range points {
		i.writeAPI.WritePoint(point)
	}

	// Clear buffer
	i.buffer = i.buffer[:0]
	i.lastFlush = time.Now()

	return nil
}

// convertToInfluxPoint converts a telemetry message to an InfluxDB point
func (i *InfluxDBSink) convertToInfluxPoint(msg telemetry.TelemetryMessage) *write.Point {
	// Create point with measurement name based on message type
	measurement := fmt.Sprintf("mavlink_%s", msg.GetMessageType())

	// Create tags (metadata)
	tags := map[string]string{
		"source": msg.GetSource(),
		"type":   msg.GetMessageType(),
	}

	// Create fields (measurements)
	fields := map[string]interface{}{}

	// Type-specific field extraction
	switch m := msg.(type) {
	case *telemetry.PositionMessage:
		fields["latitude"] = m.Latitude
		fields["longitude"] = m.Longitude
		fields["altitude"] = m.Altitude

	case *telemetry.AttitudeMessage:
		fields["roll"] = m.Roll
		fields["pitch"] = m.Pitch
		fields["yaw"] = m.Yaw

	case *telemetry.VfrHudMessage:
		fields["speed"] = m.Speed
		fields["altitude"] = m.Altitude
		fields["heading"] = m.Heading

	case *telemetry.BatteryMessage:
		fields["battery"] = m.Battery
		fields["voltage"] = m.Voltage

	case *telemetry.HeartbeatMessage:
		fields["heartbeat"] = 1
		tags["flight_mode"] = m.Mode
		tags["status"] = m.Status
	}

	// Create InfluxDB point
	point := write.NewPoint(measurement, tags, fields, msg.GetTimestamp())

	return point
}

// backgroundFlusher periodically flushes the buffer
func (i *InfluxDBSink) backgroundFlusher() {
	ticker := time.NewTicker(i.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-i.ctx.Done():
			// Final flush on shutdown
			i.mu.Lock()
			i.flushUnsafe()
			i.mu.Unlock()
			return
		case <-ticker.C:
			i.mu.Lock()
			// Flush if we have data and enough time has passed
			if len(i.buffer) > 0 && time.Since(i.lastFlush) >= i.flushInterval {
				i.flushUnsafe()
			}
			i.mu.Unlock()
		}
	}
}

// Close closes the InfluxDB sink
func (i *InfluxDBSink) Close() error {
	// Cancel background flusher
	i.cancel()

	// Final flush
	i.mu.Lock()
	defer i.mu.Unlock()

	if err := i.flushUnsafe(); err != nil {
		return fmt.Errorf("failed to flush final batch: %w", err)
	}

	// Close InfluxDB client
	i.client.Close()

	return nil
}
