/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

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
