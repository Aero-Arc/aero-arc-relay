package sinks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// ElasticsearchSink implements Sink interface for Elasticsearch
type ElasticsearchSink struct {
	client        *elasticsearch.Client
	index         string
	batchSize     int
	flushInterval time.Duration
	buffer        []telemetry.TelemetryEnvelope
	mu            sync.Mutex
	lastFlush     time.Time
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewElasticsearchSink creates a new Elasticsearch sink
func NewElasticsearchSink(cfg *config.ElasticsearchConfig) (*ElasticsearchSink, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create Elasticsearch client
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: cfg.URLs,
		Username:  cfg.Username,
		Password:  cfg.Password,
		APIKey:    cfg.APIKey,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
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

	// Set index name
	index := "mavlink-telemetry"
	if cfg.Index != "" {
		index = cfg.Index
	}

	sink := &ElasticsearchSink{
		client:        client,
		index:         index,
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
func (e *ElasticsearchSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Add message to buffer
	e.buffer = append(e.buffer, msg)

	// Flush if batch size reached
	if len(e.buffer) >= e.batchSize {
		return e.flushUnsafe()
	}

	return nil
}

// flushUnsafe flushes the buffer to Elasticsearch (must be called with lock held)
func (e *ElasticsearchSink) flushUnsafe() error {
	if len(e.buffer) == 0 {
		return nil
	}

	// Convert messages to Elasticsearch documents
	documents := make([]map[string]interface{}, 0, len(e.buffer))

	for _, msg := range e.buffer {
		doc := e.convertToElasticsearchDocument(msg)
		documents = append(documents, doc)
	}

	// Bulk index documents
	if err := e.bulkIndex(documents); err != nil {
		return fmt.Errorf("failed to bulk index documents: %w", err)
	}

	// Clear buffer
	e.buffer = e.buffer[:0]
	e.lastFlush = time.Now()

	return nil
}

// convertToElasticsearchDocument converts a telemetry message to an Elasticsearch document
// TODO implement this
func (e *ElasticsearchSink) convertToElasticsearchDocument(msg telemetry.TelemetryEnvelope) map[string]interface{} {
	// Base document structure
	_ = map[string]interface{}{ //nolint:govet
		"@timestamp": msg.TimestampRelay,
		"source":     msg.Source,
		"type":       msg.MsgName,
		"message":    msg.MsgName,
	}

	return nil
}

// bulkIndex performs bulk indexing of documents
func (e *ElasticsearchSink) bulkIndex(documents []map[string]interface{}) error {
	// Create bulk request body
	var body string
	for _, doc := range documents {
		// Index action
		indexAction := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": e.index,
			},
		}

		// Serialize action
		actionBytes, err := json.Marshal(indexAction)
		if err != nil {
			return fmt.Errorf("failed to marshal index action: %w", err)
		}
		body += string(actionBytes) + "\n"

		// Document
		docBytes, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("failed to marshal document: %w", err)
		}
		body += string(docBytes) + "\n"
	}

	// Perform bulk request
	req := esapi.BulkRequest{
		Body:    strings.NewReader(body),
		Refresh: "true",
	}

	res, err := req.Do(e.ctx, e.client)
	if err != nil {
		return fmt.Errorf("failed to perform bulk request: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("bulk request failed: %s", res.String())
	}

	return nil
}

// backgroundFlusher periodically flushes the buffer
func (e *ElasticsearchSink) backgroundFlusher() {
	ticker := time.NewTicker(e.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			// Final flush on shutdown
			e.mu.Lock()
			e.flushUnsafe()
			e.mu.Unlock()
			return
		case <-ticker.C:
			e.mu.Lock()
			// Flush if we have data and enough time has passed
			if len(e.buffer) > 0 && time.Since(e.lastFlush) >= e.flushInterval {
				e.flushUnsafe()
			}
			e.mu.Unlock()
		}
	}
}

// Close closes the Elasticsearch sink
func (e *ElasticsearchSink) Close(ctx context.Context) error {
	// Cancel background flusher
	e.cancel()

	// Final flush
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.flushUnsafe(); err != nil {
		return fmt.Errorf("failed to flush final batch: %w", err)
	}

	return nil
}
