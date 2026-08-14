/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

package telemetrywriter

import (
	"context"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type NoopWriter struct{}

// NewNoopWriter constructs telemetrywriter from the supplied configuration and dependencies.
//
// Returns:
//   - result: is the *NoopWriter value produced by NewNoopWriter.
func NewNoopWriter() *NoopWriter { return &NoopWriter{} }

// Name returns the no-op writer's stable output name.
//
// Returns:
//   - result: is the string value produced by Name.
func (n *NoopWriter) Name() string { return ConsumerName }

// WriteEnvelope writes the supplied data through NoopWriter.
//
// Parameters:
//   - ctx: is accepted for interface compatibility; the in-memory operation completes synchronously.
//   - envelope: contains the telemetry value accepted by the writer.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (n *NoopWriter) WriteEnvelope(context.Context, telemetry.TelemetryEnvelope) error { return nil }

// Close releases resources owned by NoopWriter and completes any required shutdown work.
//
// Parameters:
//   - ctx: is accepted for interface compatibility; the in-memory operation completes synchronously.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (n *NoopWriter) Close(context.Context) error { return nil }
