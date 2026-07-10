package outputs

import (
	"context"
	"errors"
	"testing"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type recordingConsumer struct {
	name     string
	messages []telemetry.TelemetryEnvelope
	err      error
}

func (r *recordingConsumer) Name() string {
	return r.name
}

func (r *recordingConsumer) WriteEnvelope(_ context.Context, envelope telemetry.TelemetryEnvelope) error {
	r.messages = append(r.messages, envelope)
	return r.err
}

func (r *recordingConsumer) Close(context.Context) error {
	return nil
}

func TestRouterRoutesByMessageFilter(t *testing.T) {
	router := NewRouter()
	global := &recordingConsumer{name: "global"}
	all := &recordingConsumer{name: "all"}

	router.AddConsumer(global, MessageFilter{Include: []string{"GlobalPositionInt"}})
	router.AddConsumer(all, MessageFilter{Include: []string{"*"}})

	router.Route(context.Background(), telemetry.TelemetryEnvelope{MsgName: "*common.MessageGlobalPositionInt"}, nil)
	router.Route(context.Background(), telemetry.TelemetryEnvelope{MsgName: "Heartbeat"}, nil)

	if got := len(global.messages); got != 1 {
		t.Fatalf("global consumer got %d messages, want 1", got)
	}
	if got := len(all.messages); got != 2 {
		t.Fatalf("all consumer got %d messages, want 2", got)
	}
}

func TestRouterReportsConsumerWriteError(t *testing.T) {
	router := NewRouter()
	wantErr := errors.New("write failed")
	router.AddConsumer(&recordingConsumer{name: "failing", err: wantErr}, MessageFilter{Include: []string{"*"}})

	var gotConsumer string
	var gotErr error
	router.Route(context.Background(), telemetry.TelemetryEnvelope{MsgName: "Heartbeat"}, func(consumer string, err error) {
		gotConsumer = consumer
		gotErr = err
	})

	if gotConsumer != "failing" {
		t.Fatalf("error consumer = %q, want %q", gotConsumer, "failing")
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("error = %v, want %v", gotErr, wantErr)
	}
}
