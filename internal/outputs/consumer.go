package outputs

import (
	"context"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// EnvelopeConsumer receives telemetry envelopes from the relay router.
//
// Internal Aero Arc paths such as registry reporting and telemetry writing can
// implement this interface alongside generic export sinks.
type EnvelopeConsumer interface {
	Name() string
	WriteEnvelope(ctx context.Context, envelope telemetry.TelemetryEnvelope) error
	Close(ctx context.Context) error
}
