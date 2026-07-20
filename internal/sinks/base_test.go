package sinks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

func TestBaseAsyncSinkBlockPolicyObservesContext(t *testing.T) {
	workerStarted := make(chan struct{}, 1)
	releaseWorker := make(chan struct{})
	sink := NewBaseAsyncSink(1, string(BackpressurePolicyBlock), "context-test", func(telemetry.TelemetryEnvelope) error {
		workerStarted <- struct{}{}
		<-releaseWorker
		return nil
	})
	defer func() {
		close(releaseWorker)
		sink.Close()
	}()

	if err := sink.Enqueue(telemetry.TelemetryEnvelope{}); err != nil {
		t.Fatalf("enqueue worker message: %v", err)
	}
	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("async sink worker did not start")
	}
	if err := sink.Enqueue(telemetry.TelemetryEnvelope{}); err != nil {
		t.Fatalf("fill queue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := sink.EnqueueContext(ctx, telemetry.TelemetryEnvelope{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnqueueContext() error = %v, want deadline exceeded", err)
	}
}
