package sinks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/timestreamwrite"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// TimestreamSink implements Sink interface for AWS Timestream
type TimestreamSink struct {
	client        *timestreamwrite.TimestreamWrite
	database      string
	table         string
	batchSize     int
	flushInterval time.Duration
	buffer        []telemetry.TelemetryEnvelope
	mu            sync.Mutex
	lastFlush     time.Time
	ctx           context.Context
	cancel        context.CancelFunc
}

// TimestreamRecord represents a record for Timestream
type TimestreamRecord struct {
	Dimensions       []*timestreamwrite.Dimension
	MeasureName      string
	MeasureValue     string
	MeasureValueType string
	Time             string
	TimeUnit         string
}

// NewTimestreamSink creates a new Timestream sink
func NewTimestreamSink(cfg *config.TimestreamConfig) (*TimestreamSink, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create AWS session
	var sess *session.Session
	var err error

	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		// Use provided credentials
		sess, err = session.NewSession(&aws.Config{
			Region: aws.String(cfg.Region),
			Credentials: credentials.NewStaticCredentials(
				cfg.AccessKey,
				cfg.SecretKey,
				cfg.SessionToken,
			),
		})
	} else {
		// Use default credentials (IAM role, environment variables, etc.)
		sess, err = session.NewSession(&aws.Config{
			Region: aws.String(cfg.Region),
		})
	}

	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Create Timestream client
	client := timestreamwrite.New(sess)

	// Parse flush interval
	flushInterval := 30 * time.Second // Default
	if cfg.FlushInterval != "" {
		if parsed, err := time.ParseDuration(cfg.FlushInterval); err == nil {
			flushInterval = parsed
		}
	}

	// Set batch size
	batchSize := 100 // Default for Timestream
	if cfg.BatchSize > 0 {
		batchSize = cfg.BatchSize
	}

	sink := &TimestreamSink{
		client:        client,
		database:      cfg.Database,
		table:         cfg.Table,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		buffer:        make([]telemetry.TelemetryEnvelope, 0, batchSize),
		lastFlush:     time.Now(),
		ctx:           ctx,
		cancel:        cancel,
	}

	// Start background flusher
	go sink.backgroundFlusher()

	return sink, nil
}

// WriteMessage adds a telemetry message to the batch
func (t *TimestreamSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Add message to buffer
	t.buffer = append(t.buffer, msg)

	// Flush if batch size reached
	if len(t.buffer) >= t.batchSize {
		return t.flushUnsafe()
	}

	return nil
}

// flushUnsafe flushes the buffer to Timestream (must be called with lock held)
func (t *TimestreamSink) flushUnsafe() error {
	if len(t.buffer) == 0 {
		return nil
	}

	// Convert messages to Timestream records
	records := make([]*timestreamwrite.Record, 0, len(t.buffer)*3) // Multiple records per message

	for _, msg := range t.buffer {
		msgRecords := t.convertToTimestreamRecords(msg)
		records = append(records, msgRecords...)
	}

	// Write records to Timestream
	writeRecordsInput := &timestreamwrite.WriteRecordsInput{
		DatabaseName: aws.String(t.database),
		TableName:    aws.String(t.table),
		Records:      records,
	}

	_, err := t.client.WriteRecordsWithContext(t.ctx, writeRecordsInput)
	if err != nil {
		return fmt.Errorf("failed to write records to Timestream: %w", err)
	}

	// Clear buffer
	t.buffer = t.buffer[:0]
	t.lastFlush = time.Now()

	return nil
}

// convertToTimestreamRecords converts a telemetry message to Timestream records
// TODO: implement this
func (t *TimestreamSink) convertToTimestreamRecords(msg telemetry.TelemetryEnvelope) []*timestreamwrite.Record {
	_ = make([]*timestreamwrite.Record, 0) //nolint:govet
	// timestamp := msg.TimestampRelay.UnixMilli()

	// Common dimensions for all records
	_ = []*timestreamwrite.Dimension{ //nolint:govet
		{
			Name:  aws.String("source"),
			Value: aws.String(msg.Source),
		},
		{
			Name:  aws.String("message_type"),
			Value: aws.String(msg.MsgName),
		},
	}

	// Type-specific measurements
	// switch m := msg.(type) {
	// case *telemetry.PositionMessage:
	// 	// Latitude measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("latitude"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.7f", m.Latitude)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// 	// Longitude measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("longitude"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.7f", m.Longitude)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// 	// Altitude measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("altitude"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.2f", m.Altitude)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// case *telemetry.AttitudeMessage:
	// 	// Roll measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("roll"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.4f", m.Roll)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// 	// Pitch measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("pitch"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.4f", m.Pitch)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// 	// Yaw measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("yaw"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.4f", m.Yaw)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// case *telemetry.VfrHudMessage:
	// 	// Speed measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("speed"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.2f", m.Speed)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// 	// Altitude measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("altitude"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.2f", m.Altitude)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// 	// Heading measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("heading"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.2f", m.Heading)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// case *telemetry.BatteryMessage:
	// 	// Battery level measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("battery_level"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.2f", m.Battery)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// 	// Voltage measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("voltage"),
	// 		MeasureValue:     aws.String(fmt.Sprintf("%.2f", m.Voltage)),
	// 		MeasureValueType: aws.String("DOUBLE"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// case *telemetry.HeartbeatMessage:
	// 	// Heartbeat as a status measurement
	// 	records = append(records, &timestreamwrite.Record{
	// 		Dimensions:       dimensions,
	// 		MeasureName:      aws.String("heartbeat"),
	// 		MeasureValue:     aws.String("1"),
	// 		MeasureValueType: aws.String("BIGINT"),
	// 		Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 		TimeUnit:         aws.String("MILLISECONDS"),
	// 	})

	// 	// Flight mode as a dimension (if needed as measurement)
	// 	if m.Mode != "" {
	// 		records = append(records, &timestreamwrite.Record{
	// 			Dimensions: append(dimensions, &timestreamwrite.Dimension{
	// 				Name:  aws.String("flight_mode"),
	// 				Value: aws.String(m.Mode),
	// 			}),
	// 			MeasureName:      aws.String("flight_mode_status"),
	// 			MeasureValue:     aws.String("1"),
	// 			MeasureValueType: aws.String("BIGINT"),
	// 			Time:             aws.String(fmt.Sprintf("%d", timestamp)),
	// 			TimeUnit:         aws.String("MILLISECONDS"),
	// 		})
	// 	}
	// }

	return nil
}

// backgroundFlusher periodically flushes the buffer
func (t *TimestreamSink) backgroundFlusher() {
	ticker := time.NewTicker(t.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			// Final flush on shutdown
			t.mu.Lock()
			t.flushUnsafe()
			t.mu.Unlock()
			return
		case <-ticker.C:
			t.mu.Lock()
			// Flush if we have data and enough time has passed
			if len(t.buffer) > 0 && time.Since(t.lastFlush) >= t.flushInterval {
				t.flushUnsafe()
			}
			t.mu.Unlock()
		}
	}
}

// Close closes the Timestream sink
func (t *TimestreamSink) Close() error {
	// Cancel background flusher
	t.cancel()

	// Final flush
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.flushUnsafe(); err != nil {
		return fmt.Errorf("failed to flush final batch: %w", err)
	}

	// Timestream client doesn't need explicit closing
	return nil
}
