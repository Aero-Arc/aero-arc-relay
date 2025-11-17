package sinks

import (
	"errors"
	"log"
	"strings"
	"sync"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// BaseSink implements Sink interface
type BaseAsyncSink struct {
	wg      sync.WaitGroup
	queue   chan telemetry.TelemetryMessage
	policy  BackpressurePolicy
	metrics *asyncSinkMetrics
}

type BackpressurePolicy string

const (
	BackpressurePolicyDrop  BackpressurePolicy = "drop"
	BackpressurePolicyBlock BackpressurePolicy = "block"

	defaultQueueSize = 1000
)

var (
	ErrQueueFull = errors.New("queue is full")

	sinkEnqueuedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_sink_enqueued_total",
		Help: "Number of telemetry messages enqueued for sink delivery.",
	}, []string{"sink"})

	sinkDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_sink_dropped_total",
		Help: "Number of telemetry messages dropped due to full sink queue.",
	}, []string{"sink"})

	sinkWorkerErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_sink_worker_errors_total",
		Help: "Number of sink worker errors encountered while handling telemetry.",
	}, []string{"sink"})

	sinkQueueLengthGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aero_sink_queue_length",
		Help: "Current number of telemetry messages buffered in the sink queue.",
	}, []string{"sink"})
)

type asyncSinkMetrics struct {
	enqueued prometheus.Counter
	dropped  prometheus.Counter
	errors   prometheus.Counter
	queueLen prometheus.Gauge
}

func normalizeBackpressurePolicy(policy string) BackpressurePolicy {
	switch strings.ToLower(policy) {
	case string(BackpressurePolicyBlock):
		return BackpressurePolicyBlock
	default:
		return BackpressurePolicyDrop
	}
}

func NewBaseAsyncSink(buffer int, policy string, sinkName string, worker func(telemetry.TelemetryMessage) error) *BaseAsyncSink {
	if buffer <= 0 {
		buffer = defaultQueueSize
	}

	labels := prometheus.Labels{"sink": sinkName}

	b := &BaseAsyncSink{
		queue:  make(chan telemetry.TelemetryMessage, buffer),
		policy: normalizeBackpressurePolicy(policy),
		metrics: &asyncSinkMetrics{
			enqueued: sinkEnqueuedTotal.With(labels),
			dropped:  sinkDroppedTotal.With(labels),
			errors:   sinkWorkerErrorsTotal.With(labels),
			queueLen: sinkQueueLengthGauge.With(labels),
		},
	}
	b.wg.Add(1)

	go func() {
		defer b.wg.Done()
		for msg := range b.queue {
			if err := worker(msg); err != nil {
				log.Printf("async sink worker error: %v", err)
				b.metrics.errors.Inc()
			}
			b.metrics.queueLen.Set(float64(len(b.queue)))
		}

		b.metrics.queueLen.Set(0)
	}()

	return b
}

func (b *BaseAsyncSink) Enqueue(msg telemetry.TelemetryMessage) error {
	switch b.policy {
	case BackpressurePolicyBlock:
		b.queue <- msg
		b.metrics.enqueued.Inc()
		b.metrics.queueLen.Set(float64(len(b.queue)))
		return nil
	case BackpressurePolicyDrop:
		fallthrough
	default:
		select {
		case b.queue <- msg:
			b.metrics.enqueued.Inc()
			b.metrics.queueLen.Set(float64(len(b.queue)))
			return nil
		default:
			b.metrics.dropped.Inc()
			return ErrQueueFull
		}
	}
}

func (b *BaseAsyncSink) Close() {
	close(b.queue)
	b.wg.Wait()

	b.metrics.queueLen.Set(0)
}
