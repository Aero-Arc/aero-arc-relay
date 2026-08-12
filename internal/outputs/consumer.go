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

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// EnvelopeConsumer receives telemetry envelopes from the relay router.
//
// Normalized telemetry writers and generic export sinks implement this
// interface. Control-plane lifecycle services do not pass through the router.
type EnvelopeConsumer interface {
	Name() string
	WriteEnvelope(ctx context.Context, envelope telemetry.TelemetryEnvelope) error
	Close(ctx context.Context) error
}
