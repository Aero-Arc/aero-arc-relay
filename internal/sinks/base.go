/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

package sinks

import (
	"context"
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
	queue   chan telemetry.TelemetryEnvelope
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

// NewBaseAsyncSink constructs sinks from the supplied configuration and dependencies.
//
// Parameters:
//   - buffer: is the int value supplied to NewBaseAsyncSink.
//   - policy: is the string value supplied to NewBaseAsyncSink.
//   - sinkName: is the string value supplied to NewBaseAsyncSink.
//   - worker: provides the error value handled by the operation.
//
// Returns:
//   - result: is the *BaseAsyncSink value produced by NewBaseAsyncSink.
func NewBaseAsyncSink(buffer int, policy string, sinkName string, worker func(telemetry.TelemetryEnvelope) error) *BaseAsyncSink {
	if buffer <= 0 {
		buffer = defaultQueueSize
	}

	labels := prometheus.Labels{"sink": sinkName}

	b := &BaseAsyncSink{
		queue:  make(chan telemetry.TelemetryEnvelope, buffer),
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

// Enqueue queues the supplied item for asynchronous processing by BaseAsyncSink.
//
// Parameters:
//   - msg: is the telemetry.TelemetryEnvelope value supplied to Enqueue.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (b *BaseAsyncSink) Enqueue(msg telemetry.TelemetryEnvelope) error {
	return b.EnqueueContext(context.Background(), msg)
}

// WriteMessageContext allows the sink adapter to propagate stream
// cancellation through an async sink's backpressure wait. Concrete sinks that
// embed BaseAsyncSink inherit this implementation.
func (b *BaseAsyncSink) WriteMessageContext(ctx context.Context, msg telemetry.TelemetryEnvelope) error {
	return b.EnqueueContext(ctx, msg)
}

// EnqueueContext queues the supplied item for asynchronous processing by BaseAsyncSink.
//
// Parameters:
//   - ctx: controls cancellation and deadlines for the operation.
//   - msg: is the telemetry.TelemetryEnvelope value supplied to EnqueueContext.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (b *BaseAsyncSink) EnqueueContext(ctx context.Context, msg telemetry.TelemetryEnvelope) error {
	switch b.policy {
	case BackpressurePolicyBlock:
		select {
		case b.queue <- msg:
			b.metrics.enqueued.Inc()
			b.metrics.queueLen.Set(float64(len(b.queue)))
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
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

// Close releases resources owned by BaseAsyncSink and completes any required shutdown work.
func (b *BaseAsyncSink) Close() {
	close(b.queue)
	b.wg.Wait()

	b.metrics.queueLen.Set(0)
}
