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

// NewSinkConsumer constructs outputs from the supplied configuration and dependencies.
//
// Parameters:
//   - name: is the string value supplied to NewSinkConsumer.
//   - sink: is the sinks.Sink value supplied to NewSinkConsumer.
//
// Returns:
//   - result: is the *SinkConsumer value produced by NewSinkConsumer.
func NewSinkConsumer(name string, sink sinks.Sink) *SinkConsumer {
	return &SinkConsumer{name: name, sink: sink}
}

// Name returns the configured router-consumer name for this sink adapter.
//
// Returns:
//   - result: is the string value produced by Name.
func (s *SinkConsumer) Name() string {
	return s.name
}

// WriteEnvelope writes the supplied data through SinkConsumer.
//
// Parameters:
//   - ctx: controls cancellation and deadlines for the operation.
//   - envelope: is the telemetry.TelemetryEnvelope value supplied to WriteEnvelope.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (s *SinkConsumer) WriteEnvelope(ctx context.Context, envelope telemetry.TelemetryEnvelope) error {
	if contextSink, ok := s.sink.(sinks.ContextSink); ok {
		return contextSink.WriteMessageContext(ctx, envelope)
	}
	return s.sink.WriteMessage(envelope)
}

// Close releases resources owned by SinkConsumer and completes any required shutdown work.
//
// Parameters:
//   - ctx: controls cancellation and deadlines for the operation.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (s *SinkConsumer) Close(ctx context.Context) error {
	return s.sink.Close(ctx)
}
