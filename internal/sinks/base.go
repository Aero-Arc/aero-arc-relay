package sinks

import (
	"errors"
	"log"
	"strings"
	"sync"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// BaseSink implements Sink interface
type BaseAsyncSink struct {
	wg     sync.WaitGroup
	queue  chan telemetry.TelemetryMessage
	policy BackpressurePolicy
}

type BackpressurePolicy string

const (
	BackpressurePolicyDrop  BackpressurePolicy = "drop"
	BackpressurePolicyBlock BackpressurePolicy = "block"

	defaultQueueSize = 1000
)

var (
	ErrQueueFull = errors.New("queue is full")
)

func normalizeBackpressurePolicy(policy string) BackpressurePolicy {
	switch strings.ToLower(policy) {
	case string(BackpressurePolicyBlock):
		return BackpressurePolicyBlock
	default:
		return BackpressurePolicyDrop
	}
}

func NewBaseAsyncSink(buffer int, policy string, worker func(telemetry.TelemetryMessage) error) *BaseAsyncSink {
	if buffer <= 0 {
		buffer = defaultQueueSize
	}

	b := &BaseAsyncSink{
		queue:  make(chan telemetry.TelemetryMessage, buffer),
		policy: normalizeBackpressurePolicy(policy),
	}
	b.wg.Add(1)

	go func() {
		defer b.wg.Done()
		for msg := range b.queue {
			if err := worker(msg); err != nil {
				log.Printf("async sink worker error: %v", err)
			}
		}
	}()

	return b
}

func (b *BaseAsyncSink) Enqueue(msg telemetry.TelemetryMessage) error {
	switch b.policy {
	case BackpressurePolicyBlock:
		b.queue <- msg
		return nil
	case BackpressurePolicyDrop:
		fallthrough
	default:
		select {
		case b.queue <- msg:
			return nil
		default:
			return ErrQueueFull
		}
	}
}

func (b *BaseAsyncSink) Close() {
	close(b.queue)
	b.wg.Wait()
}
