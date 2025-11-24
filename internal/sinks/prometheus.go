package sinks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// PrometheusSink implements Sink interface for Prometheus
type PrometheusSink struct {
	client        api.Client
	writeAPI      v1.API
	job           string
	instance      string
	batchSize     int
	flushInterval time.Duration
	buffer        []telemetry.TelemetryEnvelope
	mu            sync.Mutex
	lastFlush     time.Time
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewPrometheusSink creates a new Prometheus sink
func NewPrometheusSink(cfg *config.PrometheusConfig) (*PrometheusSink, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create Prometheus client
	client, err := api.NewClient(api.Config{
		Address: cfg.URL,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	writeAPI := v1.NewAPI(client)

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

	// Set job and instance
	job := "aero-arc-relay"
	if cfg.Job != "" {
		job = cfg.Job
	}

	instance := "default"
	if cfg.Instance != "" {
		instance = cfg.Instance
	}

	sink := &PrometheusSink{
		client:        client,
		writeAPI:      writeAPI,
		job:           job,
		instance:      instance,
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
func (p *PrometheusSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Add message to buffer
	p.buffer = append(p.buffer, msg)

	// Flush if batch size reached
	if len(p.buffer) >= p.batchSize {
		return p.flushUnsafe()
	}

	return nil
}

// flushUnsafe flushes the buffer to Prometheus (must be called with lock held)
func (p *PrometheusSink) flushUnsafe() error {
	if len(p.buffer) == 0 {
		return nil
	}

	// Convert messages to Prometheus samples
	samples := make([]model.Sample, 0, len(p.buffer))

	for _, msg := range p.buffer {
		sample := p.convertToPrometheusSample(msg)
		samples = append(samples, sample)
	}

	// Write samples to Prometheus
	// Note: In a real implementation, you would use a Prometheus pushgateway
	// or remote write endpoint. This is a simplified example.

	// Clear buffer
	p.buffer = p.buffer[:0]
	p.lastFlush = time.Now()

	return nil
}

// convertToPrometheusSample converts a telemetry message to a Prometheus sample
// TODO: implement this
func (p *PrometheusSink) convertToPrometheusSample(msg telemetry.TelemetryEnvelope) model.Sample {
	// Create metric name based on message type
	metricName := fmt.Sprintf("mavlink_%s", msg.MsgName)

	// Create labels
	labels := model.LabelSet{
		"job":      model.LabelValue(p.job),
		"instance": model.LabelValue(p.instance),
		"source":   model.LabelValue(msg.Source),
		"type":     model.LabelValue(msg.MsgName),
	}

	// Create metric value
	var value model.SampleValue = 1 // Default value

	// Create Prometheus sample
	sample := model.Sample{
		Metric: model.Metric{
			model.MetricNameLabel: model.LabelValue(metricName),
		},
		Value:     value,
		Timestamp: model.Time(msg.TimestampRelay.Unix() * 1000), // Convert to milliseconds
	}

	// Add labels to metric
	for k, v := range labels {
		sample.Metric[k] = v
	}

	return sample
}

// backgroundFlusher periodically flushes the buffer
func (p *PrometheusSink) backgroundFlusher() {
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			// Final flush on shutdown
			p.mu.Lock()
			p.flushUnsafe()
			p.mu.Unlock()
			return
		case <-ticker.C:
			p.mu.Lock()
			// Flush if we have data and enough time has passed
			if len(p.buffer) > 0 && time.Since(p.lastFlush) >= p.flushInterval {
				p.flushUnsafe()
			}
			p.mu.Unlock()
		}
	}
}

// Close closes the Prometheus sink
func (p *PrometheusSink) Close() error {
	// Cancel background flusher
	p.cancel()

	// Final flush
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.flushUnsafe(); err != nil {
		return fmt.Errorf("failed to flush final batch: %w", err)
	}

	return nil
}
