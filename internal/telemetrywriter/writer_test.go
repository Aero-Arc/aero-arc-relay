package telemetrywriter

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/makinje/aero-arc-relay/internal/telemetrynormalize"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type fakeBackend struct {
	mu      sync.Mutex
	records []telemetrynormalize.Record
	writes  chan struct{}
	err     error
	calls   int
	started chan struct{}
	block   chan struct{}
	closed  chan struct{}
}

func (f *fakeBackend) WriteBatch(ctx context.Context, records []telemetrynormalize.Record) error {
	f.mu.Lock()
	f.calls++
	f.records = append(f.records, records...)
	f.mu.Unlock()
	if f.writes != nil {
		select {
		case f.writes <- struct{}{}:
		default:
		}
	}
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func TestWriterRetriesBackendBatch(t *testing.T) {
	backend := &fakeBackend{err: context.DeadlineExceeded}
	writer, err := NewWriter(Config{Workers: 1, MaxRetries: 2, RetryBackoff: time.Millisecond, WriteTimeout: time.Second}, backend, nil)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	err = writer.writeBatch([]telemetrynormalize.Record{{MessageName: "heartbeat"}})
	if err == nil {
		t.Fatal("expected backend error")
	}
	backend.mu.Lock()
	calls := backend.calls
	backend.mu.Unlock()
	if calls != 3 {
		t.Fatalf("backend calls = %d, want 3", calls)
	}
	backend.err = nil
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func (f *fakeBackend) Close(context.Context) error {
	if f.closed != nil {
		select {
		case f.closed <- struct{}{}:
		default:
		}
	}
	return nil
}

func TestWriterNormalizesAndWritesBatch(t *testing.T) {
	backend := &fakeBackend{writes: make(chan struct{}, 1)}
	writer, err := NewWriter(Config{Workers: 1, BatchSize: 1}, backend, nil)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.WriteEnvelope(context.Background(), validPositionEnvelope()); err != nil {
		t.Fatalf("WriteEnvelope() error = %v", err)
	}
	select {
	case <-backend.writes:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for backend write")
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.records) != 1 || backend.records[0].MessageName != "global_position_int" {
		t.Fatalf("records = %#v", backend.records)
	}
}

func TestWriterIgnoresUnsupportedMessage(t *testing.T) {
	backend := &fakeBackend{}
	writer, err := NewWriter(Config{Workers: 1, BatchSize: 1}, backend, nil)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	envelope := validPositionEnvelope()
	envelope.MsgName = "Attitude"
	if err := writer.WriteEnvelope(context.Background(), envelope); err != nil {
		t.Fatalf("WriteEnvelope() error = %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if len(backend.records) != 0 {
		t.Fatalf("unexpected records = %#v", backend.records)
	}
}

func TestWriterReturnsNormalizationFailureBeforeAdmission(t *testing.T) {
	backend := &fakeBackend{}
	writer, err := NewWriter(Config{Workers: 1, BatchSize: 1}, backend, nil)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	envelope := validPositionEnvelope()
	envelope.Fields = map[string]any{}
	if err := writer.WriteEnvelope(context.Background(), envelope); !errors.Is(err, ErrNormalize) {
		t.Fatalf("WriteEnvelope() error = %v, want ErrNormalize", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.records) != 0 {
		t.Fatalf("invalid envelope reached backend: %#v", backend.records)
	}
}

func TestWriterReturnsQueueFullWhenAdmissionTimesOut(t *testing.T) {
	backend := &fakeBackend{started: make(chan struct{}, 1), block: make(chan struct{})}
	writer, err := NewWriter(Config{
		QueueCapacity:  1,
		Workers:        1,
		BatchSize:      1,
		EnqueueTimeout: 5 * time.Millisecond,
		WriteTimeout:   time.Second,
	}, backend, nil)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}

	if err := writer.WriteEnvelope(context.Background(), validPositionEnvelope()); err != nil {
		t.Fatalf("first WriteEnvelope() error = %v", err)
	}
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("backend write did not start")
	}
	if err := writer.WriteEnvelope(context.Background(), validPositionEnvelope()); err != nil {
		t.Fatalf("second WriteEnvelope() error = %v", err)
	}
	if err := writer.WriteEnvelope(context.Background(), validPositionEnvelope()); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("third WriteEnvelope() error = %v, want ErrQueueFull", err)
	}

	close(backend.block)
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestWriterClosesBackendAfterDrainTimeout(t *testing.T) {
	backend := &fakeBackend{
		started: make(chan struct{}, 1),
		block:   make(chan struct{}),
		closed:  make(chan struct{}, 1),
	}
	writer, err := NewWriter(Config{
		Workers: 1, BatchSize: 1, WriteTimeout: time.Minute,
	}, backend, nil)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.WriteEnvelope(context.Background(), validPositionEnvelope()); err != nil {
		t.Fatalf("WriteEnvelope() error = %v", err)
	}
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("backend write did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := writer.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want deadline exceeded", err)
	}
	select {
	case <-backend.closed:
	case <-time.After(time.Second):
		t.Fatal("backend was not closed after writer drain timed out")
	}
}

func validPositionEnvelope() telemetry.TelemetryEnvelope {
	return telemetry.TelemetryEnvelope{
		AgentID:        "agent-1",
		RelayID:        "relay-1",
		SessionID:      "session-1",
		AircraftID:     "aircraft-1",
		OperatorID:     "operator-1",
		TimestampRelay: time.Date(2026, 7, 12, 12, 0, 1, 0, time.UTC),
		TimestampAgent: time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC),
		Dialect:        "common",
		MsgID:          33,
		MsgName:        "GlobalPositionInt",
		WALSequence:    42,
		Fields: map[string]any{
			"Lat": "418781000", "Lon": "-876291000", "Alt": "123450",
		},
	}
}
