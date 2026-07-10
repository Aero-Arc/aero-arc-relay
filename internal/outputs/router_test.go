package outputs

import (
	"context"
	"testing"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type recordingConsumer struct {
	name     string
	messages []telemetry.TelemetryEnvelope
}

func (r *recordingConsumer) Name() string {
	return r.name
}

func (r *recordingConsumer) WriteEnvelope(_ context.Context, envelope telemetry.TelemetryEnvelope) error {
	r.messages = append(r.messages, envelope)
	return nil
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

	if err := router.Route(context.Background(), telemetry.TelemetryEnvelope{MsgName: "*common.MessageGlobalPositionInt"}); err != nil {
		t.Fatalf("route global position: %v", err)
	}
	if err := router.Route(context.Background(), telemetry.TelemetryEnvelope{MsgName: "Heartbeat"}); err != nil {
		t.Fatalf("route heartbeat: %v", err)
	}

	if got := len(global.messages); got != 1 {
		t.Fatalf("global consumer got %d messages, want 1", got)
	}
	if got := len(all.messages); got != 2 {
		t.Fatalf("all consumer got %d messages, want 2", got)
	}
}
