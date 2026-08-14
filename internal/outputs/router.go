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
	"fmt"
	"log/slog"
	"sync"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type RouteError struct {
	Consumer string
	Err      error
}

type route struct {
	consumer EnvelopeConsumer
	filter   MessageFilter
}

// Router fans telemetry envelopes out to internal Aero Arc paths and generic
// sinks according to each consumer's message filter.
type Router struct {
	routes []route
}

// NewRouter constructs outputs from the supplied configuration and dependencies.
//
// Returns:
//   - result: is the *Router value produced by NewRouter.
func NewRouter() *Router {
	return &Router{routes: make([]route, 0)}
}

// AddConsumer adds the supplied value to Router.
//
// Parameters:
//   - consumer: is the EnvelopeConsumer value supplied to AddConsumer.
//   - filter: is the MessageFilter value supplied to AddConsumer.
func (r *Router) AddConsumer(consumer EnvelopeConsumer, filter MessageFilter) {
	if consumer == nil {
		return
	}
	if !filter.hasIncludes() {
		slog.Warn("output registered without included messages; it will receive no telemetry",
			slog.String("consumer", consumer.Name()),
			slog.String("configuration_hint", "set include_messages to [\"*\"] to receive all messages"),
		)
	}
	r.routes = append(r.routes, route{consumer: consumer, filter: filter})
}

// HasConsumers reports whether Router has the requested state or capability.
//
// Returns:
//   - bool: reports whether the requested condition was satisfied.
func (r *Router) HasConsumers() bool {
	return r != nil && len(r.routes) > 0
}

// Route invokes every matching consumer concurrently and waits for all of them
// to return. A consumer that ignores cancellation can therefore block Route;
// failures are collected in completion order rather than route order.
//
// Parameters:
//   - ctx: controls cancellation and deadlines for the operation.
//   - envelope: is the telemetry.TelemetryEnvelope value supplied to Route.
//
// Returns:
//   - errors: contains consumer failures in nondeterministic completion order;
//     nil means every matching consumer accepted the envelope.
func (r *Router) Route(ctx context.Context, envelope telemetry.TelemetryEnvelope) []RouteError {
	//TODO: Figure out what happens or how to get around blocked consumer
	var wg sync.WaitGroup
	var errorsMu sync.Mutex
	var routeErrors []RouteError

	if r == nil {
		return nil
	}

	for _, route := range r.routes {
		if !route.filter.Allows(envelope.MsgName) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := route.consumer.WriteEnvelope(ctx, envelope); err != nil {
				consumer := route.consumer.Name()
				slog.Warn("telemetry consumer write failed",
					slog.String("consumer", route.consumer.Name()),
					slog.String("message_name", envelope.MsgName),
					slog.Any("error", err),
				)

				errorsMu.Lock()
				routeErrors = append(routeErrors, RouteError{Consumer: consumer, Err: err})
				errorsMu.Unlock()
			}
		}()
	}

	wg.Wait()
	return routeErrors
}

// Close releases resources owned by Router and completes any required shutdown work.
//
// Parameters:
//   - ctx: controls cancellation and deadlines for the operation.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
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
