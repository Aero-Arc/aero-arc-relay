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

type RouteErrorHandler func(consumer string, err error)

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
	if !filter.hasIncludes() {
		slog.Warn("output registered without included messages; it will receive no telemetry",
			slog.String("consumer", consumer.Name()),
			slog.String("configuration_hint", "set include_messages to [\"*\"] to receive all messages"),
		)
	}
	r.routes = append(r.routes, route{consumer: consumer, filter: filter})
}

func (r *Router) HasConsumers() bool {
	return r != nil && len(r.routes) > 0
}

func (r *Router) Route(ctx context.Context, envelope telemetry.TelemetryEnvelope, onError RouteErrorHandler) {
	//TODO: Figure out what happens or how to get around blocked consumer
	var wg sync.WaitGroup

	if r == nil {
		return
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

				if onError != nil {
					onError(consumer, err)
				}
			}
		}()
	}

	wg.Wait()
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
