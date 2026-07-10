package outputs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type route struct {
	consumer EnvelopeConsumer
	filter   MessageFilter
}

// Router fans telemetry envelopes out to internal Aero Arc paths and generic
// sinks according to each consumer's message filter.
type Router struct {
	routes []route
}

func NewRouter() *Router {
	return &Router{routes: make([]route, 0)}
}

func (r *Router) AddConsumer(consumer EnvelopeConsumer, filter MessageFilter) {
	if consumer == nil {
		return
	}
	r.routes = append(r.routes, route{consumer: consumer, filter: filter})
}

func (r *Router) Route(ctx context.Context, envelope telemetry.TelemetryEnvelope) error {
	if r == nil {
		return nil
	}

	var routeErr error
	for _, route := range r.routes {
		if !route.filter.Allows(envelope.MsgName) {
			continue
		}
		if err := route.consumer.WriteEnvelope(ctx, envelope); err != nil {
			slog.Warn("telemetry consumer write failed",
				slog.String("consumer", route.consumer.Name()),
				slog.String("message_name", envelope.MsgName),
				slog.String("error", err.Error()),
			)
			routeErr = fmt.Errorf("%s: %w", route.consumer.Name(), err)
		}
	}
	return routeErr
}

func (r *Router) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}

	var closeErr error
	for _, route := range r.routes {
		if err := route.consumer.Close(ctx); err != nil {
			slog.Warn("telemetry consumer close failed",
				slog.String("consumer", route.consumer.Name()),
				slog.String("error", err.Error()),
			)
			closeErr = fmt.Errorf("%s: %w", route.consumer.Name(), err)
		}
	}
	return closeErr
}
