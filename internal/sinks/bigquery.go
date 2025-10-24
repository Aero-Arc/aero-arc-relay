package sinks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/api/option"
)

// BigQuerySink implements Sink interface for Google BigQuery
type BigQuerySink struct {
	client        *bigquery.Client
	inserter      *bigquery.Inserter
	batchSize     int
	flushInterval time.Duration
	buffer        []telemetry.TelemetryMessage
	mu            sync.Mutex
	lastFlush     time.Time
	ctx           context.Context
	cancel        context.CancelFunc
}

// BigQueryRow represents a row in BigQuery for telemetry data
type BigQueryRow struct {
	Source      string    `bigquery:"source"`
	Timestamp   time.Time `bigquery:"timestamp"`
	MessageType string    `bigquery:"message_type"`

	// Position data
	Latitude  *float64 `bigquery:"latitude"`
	Longitude *float64 `bigquery:"longitude"`
	Altitude  *float64 `bigquery:"altitude"`

	// Attitude data
	Roll  *float64 `bigquery:"roll"`
	Pitch *float64 `bigquery:"pitch"`
	Yaw   *float64 `bigquery:"yaw"`

	// VFR HUD data
	GroundSpeed *float64 `bigquery:"ground_speed"`
	Heading     *float64 `bigquery:"heading"`
	Throttle    *float64 `bigquery:"throttle"`

	// Battery data
	BatteryLevel   *float64 `bigquery:"battery_level"`
	BatteryVoltage *float64 `bigquery:"battery_voltage"`
	BatteryCurrent *float64 `bigquery:"battery_current"`

	// Status data
	FlightMode string `bigquery:"flight_mode"`
	Status     string `bigquery:"status"`
	CustomMode *int32 `bigquery:"custom_mode"`

	// Raw JSON for flexibility
	RawData string `bigquery:"raw_data"`
}

// NewBigQuerySink creates a new BigQuery sink
func NewBigQuerySink(cfg *config.BigQueryConfig) (*BigQuerySink, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create client with credentials if provided
	var client *bigquery.Client
	var err error

	if cfg.Credentials != "" {
		// Use service account credentials file
		client, err = bigquery.NewClient(ctx, cfg.ProjectID, option.WithCredentialsFile(cfg.Credentials))
	} else {
		// Use default credentials (ADC, environment variables, etc.)
		client, err = bigquery.NewClient(ctx, cfg.ProjectID)
	}

	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create BigQuery client: %w", err)
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

	// Create table reference
	table := client.Dataset(cfg.Dataset).Table(cfg.Table)
	inserter := table.Inserter()

	sink := &BigQuerySink{
		client:        client,
		inserter:      inserter,
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
func (b *BigQuerySink) WriteMessage(msg telemetry.TelemetryMessage) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Add message to buffer
	b.buffer = append(b.buffer, msg)

	// Flush if batch size reached
	if len(b.buffer) >= b.batchSize {
		return b.flushUnsafe()
	}

	return nil
}

// flushUnsafe flushes the buffer to BigQuery (must be called with lock held)
func (b *BigQuerySink) flushUnsafe() error {
	if len(b.buffer) == 0 {
		return nil
	}

	// Convert messages to BigQuery rows
	rows := make([]*BigQueryRow, 0, len(b.buffer))
	for _, msg := range b.buffer {
		row := b.convertToBigQueryRow(msg)
		rows = append(rows, row)
	}

	// Insert rows
	if err := b.inserter.Put(b.ctx, rows); err != nil {
		return fmt.Errorf("failed to insert rows to BigQuery: %w", err)
	}

	// Clear buffer
	b.buffer = b.buffer[:0]
	b.lastFlush = time.Now()

	return nil
}

// convertToBigQueryRow converts a telemetry message to a BigQuery row
func (b *BigQuerySink) convertToBigQueryRow(msg telemetry.TelemetryMessage) *BigQueryRow {
	// Get raw JSON data
	jsonData, _ := msg.ToJSON()

	row := &BigQueryRow{
		Source:      msg.GetSource(),
		Timestamp:   msg.GetTimestamp(),
		MessageType: msg.GetMessageType(),
		RawData:     string(jsonData),
	}

	// Type-specific data extraction
	switch m := msg.(type) {
	case *telemetry.PositionMessage:
		row.Latitude = &m.Latitude
		row.Longitude = &m.Longitude
		row.Altitude = &m.Altitude

	case *telemetry.AttitudeMessage:
		row.Roll = &m.Roll
		row.Pitch = &m.Pitch
		row.Yaw = &m.Yaw

	case *telemetry.VfrHudMessage:
		row.GroundSpeed = &m.Speed
		row.Heading = &m.Heading
		// Throttle not available in VfrHudMessage

	case *telemetry.BatteryMessage:
		row.BatteryLevel = &m.Battery
		row.BatteryVoltage = &m.Voltage
		// BatteryCurrent not available in BatteryMessage

	case *telemetry.HeartbeatMessage:
		row.FlightMode = m.Mode
		row.Status = m.Status
	}

	return row
}

// backgroundFlusher periodically flushes the buffer
func (b *BigQuerySink) backgroundFlusher() {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			// Final flush on shutdown
			b.mu.Lock()
			b.flushUnsafe()
			b.mu.Unlock()
			return
		case <-ticker.C:
			b.mu.Lock()
			// Flush if we have data and enough time has passed
			if len(b.buffer) > 0 && time.Since(b.lastFlush) >= b.flushInterval {
				b.flushUnsafe()
			}
			b.mu.Unlock()
		}
	}
}

// Close closes the BigQuery sink
func (b *BigQuerySink) Close() error {
	// Cancel background flusher
	b.cancel()

	// Final flush
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.flushUnsafe(); err != nil {
		return fmt.Errorf("failed to flush final batch: %w", err)
	}

	// Close client
	return b.client.Close()
}
