package sinks

import (
	"errors"
	"sync"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// BaseSink implements Sink interface
type BaseAsyncSink struct {
	wg    sync.WaitGroup
	queue chan telemetry.TelemetryMessage
}

func NewBaseAsyncSink(buffer int, worker func(telemetry.TelemetryMessage) error) *BaseAsyncSink {
	b := &BaseAsyncSink{
		queue: make(chan telemetry.TelemetryMessage, buffer),
	}
	b.wg.Add(1)

	go func() {
		defer b.wg.Done()
		for msg := range b.queue {
			worker(msg)
		}
	}()

	return b
}

func (b *BaseAsyncSink) Enqueue(msg telemetry.TelemetryMessage) error {
	select {
	case b.queue <- msg:
		return nil
	default:
		return errors.New("queue is full")
	}
}

func (b *BaseAsyncSink) Close() {
	close(b.queue)
	b.wg.Wait()
}
