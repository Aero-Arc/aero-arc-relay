package telemetrywriter

import (
	"context"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type NoopWriter struct{}

func NewNoopWriter() *NoopWriter { return &NoopWriter{} }

func (n *NoopWriter) Name() string { return ConsumerName }

func (n *NoopWriter) WriteEnvelope(context.Context, telemetry.TelemetryEnvelope) error { return nil }

func (n *NoopWriter) Close(context.Context) error { return nil }
