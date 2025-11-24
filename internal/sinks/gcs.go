package sinks

import (
	"context"
	"fmt"
	"io"
	"log"
	"path"
	"path/filepath"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/api/option"
)

// GCSSink implements Sink interface for Google Cloud Storage
type GCSSink struct {
	client    *storage.Client
	bucket    string
	prefix    string
	fileSink  *FileSink
	mu        sync.Mutex
	closeChan chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup
	*BaseAsyncSink
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

	flushInterval := time.Minute
	if cfg.FlushInterval > 0 {
		flushInterval = cfg.FlushInterval
	}

	fileSink, err := NewFileSink(&config.FileConfig{
		Path:               "/tmp",
		Prefix:             "gcs-sink",
		Format:             "json",
		RotationInterval:   flushInterval,
		QueueSize:          cfg.QueueSize,
		BackpressurePolicy: cfg.BackpressurePolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create file sink: %w", err)
	}

	g := &GCSSink{
		client:    client,
		bucket:    cfg.Bucket,
		prefix:    cfg.Prefix,
		fileSink:  fileSink,
		closeChan: make(chan struct{}),
	}
	g.BaseAsyncSink = NewBaseAsyncSink(cfg.QueueSize, cfg.BackpressurePolicy, "gcs", g.handleMessage)

	g.wg.Add(1)
	go func(closeCh <-chan struct{}) {
		defer g.wg.Done()

		flushTicker := time.NewTicker(flushInterval)
		defer flushTicker.Stop()

		for {
			select {
			case <-flushTicker.C:
				if err := g.RotateAndUpload(); err != nil {
					log.Printf("failed to rotate and upload file to GCS: %v", err)
				}
			case <-closeCh:
				return
			}
		}
	}(g.closeChan)

	return g, nil
}

// WriteMessage writes telemetry messages by buffering locally before upload
func (g *GCSSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	err := g.BaseAsyncSink.Enqueue(msg)
	if err != nil {
		return fmt.Errorf("failed to enqueue message: %w", err)
	}

	return nil
}

func (g *GCSSink) handleMessage(msg telemetry.TelemetryEnvelope) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.fileSink.WriteMessage(msg)
}

// RotateAndUpload forces a rotation and upload cycle
func (g *GCSSink) RotateAndUpload() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.uploadAndMaybeRotateLocked(true)
}

// Close stops background workers and flushes remaining data
func (g *GCSSink) Close(ctx context.Context) error {
	g.stopOnce.Do(func() {
		close(g.closeChan)
	})

	g.wg.Wait()

	g.BaseAsyncSink.Close()

	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.uploadAndMaybeRotateLocked(false); err != nil {
		return err
	}

	if err := g.fileSink.Close(ctx); err != nil {
		return fmt.Errorf("failed to close file sink: %w", err)
	}

	if g.client != nil {
		if err := g.client.Close(); err != nil {
			return fmt.Errorf("failed to close GCS client: %w", err)
		}
	}

	return nil
}

func (g *GCSSink) uploadAndMaybeRotateLocked(rotate bool) error {
	f := g.fileSink

	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.flushLocked(); err != nil {
		return fmt.Errorf("failed to flush file sink: %w", err)
	}

	info, err := f.file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if info.Size() == 0 {
		return nil
	}

	if _, err := f.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek file: %w", err)
	}

	key := path.Join(g.prefix, filepath.Base(f.file.Name()))

	ctx := context.Background()
	writer := g.client.Bucket(g.bucket).Object(key).NewWriter(ctx)
	writer.ContentType = "application/json"

	if _, err := io.Copy(writer, f.file); err != nil {
		writer.Close()
		return fmt.Errorf("failed to upload to GCS: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close GCS writer: %w", err)
	}

	if rotate {
		if err := f.rotateFileLocked(); err != nil {
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	return nil
}
