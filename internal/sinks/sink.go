/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

// Package sinks defines sink interfaces and implementations for telemetry
// backends such as object storage, databases, and streaming systems.
package sinks

import (
	"context"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// Sink defines the interface for data sinks
type Sink interface {
	WriteMessage(msg telemetry.TelemetryEnvelope) error
	Close(ctx context.Context) error
}

// ContextSink is implemented by sinks that can stop accepting a message when
// the caller's request is cancelled. Sink adapters prefer this method when it
// is available so backpressure cannot outlive the telemetry stream.
type ContextSink interface {
	WriteMessageContext(ctx context.Context, msg telemetry.TelemetryEnvelope) error
}

// SinkType represents the type of sink
type SinkType string

const (
	SinkTypeS3    SinkType = "s3"
	SinkTypeKafka SinkType = "kafka"
	SinkTypeFile  SinkType = "file"
)
