package telemetrywriter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/makinje/aero-arc-relay/internal/outputs"
	"github.com/makinje/aero-arc-relay/internal/telemetrynormalize"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ErrQueueFull = errors.New("normalized telemetry queue is full")
	ErrClosed    = errors.New("normalized telemetry writer is closed")

	acceptedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_telemetry_writer_accepted_total",
		Help: "Envelopes accepted by the normalized telemetry writer.",
	}, []string{"message_name"})
	droppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_telemetry_writer_dropped_total",
		Help: "Envelopes dropped by the normalized telemetry writer.",
	}, []string{"reason"})
	normalizationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_telemetry_normalization_total",
		Help: "Normalized telemetry outcomes.",
	}, []string{"message_name", "outcome"})
	backendWritesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_telemetry_backend_writes_total",
		Help: "Normalized telemetry backend batch outcomes.",
	}, []string{"outcome"})
	backendBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "aero_telemetry_backend_batch_size",
		Help:    "Number of normalized records submitted in a backend batch.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 11),
	})
	backendLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "aero_telemetry_backend_write_seconds",
		Help:    "Normalized telemetry backend batch latency.",
		Buckets: prometheus.DefBuckets,
	})
	backendRetriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "aero_telemetry_backend_retries_total",
		Help: "Normalized telemetry backend retry attempts.",
	})
	queueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "aero_telemetry_writer_queue_depth",
		Help: "Current normalized telemetry queue depth.",
	})
)

type Config struct {
	QueueCapacity  int
	Workers        int
	BatchSize      int
	FlushInterval  time.Duration
	EnqueueTimeout time.Duration
	WriteTimeout   time.Duration
	MaxRetries     int
	RetryBackoff   time.Duration
}

func (c Config) withDefaults() Config {
	if c.QueueCapacity <= 0 {
		c.QueueCapacity = 10_000
	}
	if c.Workers <= 0 {
		c.Workers = 2
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 500
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = time.Second
	}
	if c.EnqueueTimeout <= 0 {
		c.EnqueueTimeout = 100 * time.Millisecond
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 5 * time.Second
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = 200 * time.Millisecond
	}
	return c
}

type Writer struct {
	config     Config
	backend    Backend
	registry   *telemetrynormalize.Registry
	queue      chan telemetry.TelemetryEnvelope
	workerCtx  context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	stateMu    sync.RWMutex
	closed     bool
	closeOnce  sync.Once
	backendErr error
	errMu      sync.Mutex
}

func NewWriter(config Config, backend Backend, registry *telemetrynormalize.Registry) (*Writer, error) {
	if backend == nil {
		return nil, errors.New("normalized telemetry backend is required")
	}
	if registry == nil {
		registry = telemetrynormalize.NewRegistry()
	}
	config = config.withDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	w := &Writer{
		config:    config,
		backend:   backend,
		registry:  registry,
		queue:     make(chan telemetry.TelemetryEnvelope, config.QueueCapacity),
		workerCtx: ctx,
		cancel:    cancel,
	}
	for worker := 0; worker < config.Workers; worker++ {
		w.wg.Add(1)
		go w.runWorker(worker)
	}
	return w, nil
}

func (w *Writer) Name() string { return "telemetry" }

func (w *Writer) WriteEnvelope(ctx context.Context, envelope telemetry.TelemetryEnvelope) error {
	if _, supported := w.registry.Lookup(envelope.MsgName); !supported {
		return nil
	}
	canonicalName := outputs.NormalizeMessageName(envelope.MsgName)
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	if w.closed {
		return ErrClosed
	}
	timer := time.NewTimer(w.config.EnqueueTimeout)
	defer timer.Stop()
	select {
	case w.queue <- envelope:
		acceptedTotal.WithLabelValues(canonicalName).Inc()
		queueDepth.Set(float64(len(w.queue)))
		return nil
	case <-ctx.Done():
		droppedTotal.WithLabelValues("context_cancelled").Inc()
		return ctx.Err()
	case <-timer.C:
		droppedTotal.WithLabelValues("queue_full").Inc()
		return ErrQueueFull
	}
}

func (w *Writer) runWorker(worker int) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.config.FlushInterval)
	defer ticker.Stop()
	batch := make([]telemetrynormalize.Record, 0, w.config.BatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.writeBatch(batch); err != nil {
			slog.Error("normalized telemetry batch write failed", "worker", worker, "records", len(batch), "error", err)
			w.errMu.Lock()
			w.backendErr = err
			w.errMu.Unlock()
		}
		batch = batch[:0]
	}
	for {
		select {
		case envelope, ok := <-w.queue:
			if !ok {
				flush()
				return
			}
			queueDepth.Set(float64(len(w.queue)))
			normalizer, supported := w.registry.Lookup(envelope.MsgName)
			if !supported {
				normalizationTotal.WithLabelValues(outputs.NormalizeMessageName(envelope.MsgName), "unsupported").Inc()
				continue
			}
			record, err := normalizer.Normalize(envelope)
			if err != nil {
				normalizationTotal.WithLabelValues(outputs.NormalizeMessageName(envelope.MsgName), "failed").Inc()
				slog.Warn("telemetry normalization failed", "message_name", envelope.MsgName, "error", err)
				continue
			}
			normalizationTotal.WithLabelValues(record.MessageName, "succeeded").Inc()
			batch = append(batch, record)
			if len(batch) >= w.config.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.workerCtx.Done():
			flush()
			return
		}
	}
}

func (w *Writer) writeBatch(batch []telemetrynormalize.Record) error {
	ctx, cancel := context.WithTimeout(w.workerCtx, w.config.WriteTimeout)
	defer cancel()
	started := time.Now()
	backendBatchSize.Observe(float64(len(batch)))
	var err error
	for attempt := 0; attempt <= w.config.MaxRetries; attempt++ {
		err = w.backend.WriteBatch(ctx, batch)
		if err == nil || attempt == w.config.MaxRetries {
			break
		}
		backendRetriesTotal.Inc()
		backoff := w.config.RetryBackoff * time.Duration(1<<attempt)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			err = errors.Join(err, ctx.Err())
			attempt = w.config.MaxRetries
		}
	}
	backendLatency.Observe(time.Since(started).Seconds())
	if err != nil {
		backendWritesTotal.WithLabelValues("failed").Inc()
		return err
	}
	backendWritesTotal.WithLabelValues("succeeded").Inc()
	return nil
}

func (w *Writer) Close(ctx context.Context) error {
	w.closeOnce.Do(func() {
		w.stateMu.Lock()
		w.closed = true
		close(w.queue)
		w.stateMu.Unlock()
	})
	drained := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-ctx.Done():
		w.cancel()
		<-drained
		return fmt.Errorf("drain normalized telemetry writer: %w", ctx.Err())
	}
	w.cancel()
	if err := w.backend.Close(ctx); err != nil {
		return fmt.Errorf("close normalized telemetry backend: %w", err)
	}
	w.errMu.Lock()
	defer w.errMu.Unlock()
	return w.backendErr
}
