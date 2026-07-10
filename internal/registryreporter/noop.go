package registryreporter

import (
	"context"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type NoopReporter struct{}

func NewNoopReporter() *NoopReporter {
	return &NoopReporter{}
}

func (n *NoopReporter) Name() string {
	return "registry"
}

func (n *NoopReporter) WriteEnvelope(context.Context, telemetry.TelemetryEnvelope) error {
	return nil
}

func (n *NoopReporter) Close(context.Context) error {
	return nil
}
