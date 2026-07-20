package outputs

import (
	"context"

	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type SinkConsumer struct {
	name string
	sink sinks.Sink
}

func NewSinkConsumer(name string, sink sinks.Sink) *SinkConsumer {
	return &SinkConsumer{name: name, sink: sink}
}

func (s *SinkConsumer) Name() string {
	return s.name
}

func (s *SinkConsumer) WriteEnvelope(ctx context.Context, envelope telemetry.TelemetryEnvelope) error {
	if contextSink, ok := s.sink.(sinks.ContextSink); ok {
		return contextSink.WriteMessageContext(ctx, envelope)
	}
	return s.sink.WriteMessage(envelope)
}

func (s *SinkConsumer) Close(ctx context.Context) error {
	return s.sink.Close(ctx)
}
