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

func NewNoopWriter() *NoopWriter { return &NoopWriter{} }

func (n *NoopWriter) Name() string { return ConsumerName }

func (n *NoopWriter) WriteEnvelope(context.Context, telemetry.TelemetryEnvelope) error { return nil }

func (n *NoopWriter) Close(context.Context) error { return nil }
