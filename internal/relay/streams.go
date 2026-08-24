/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

package relay

import (
	"context"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
)

func (r *Relay) updateStream(agentID, sessionID string, stream agentv1.AgentGateway_TelemetryStreamServer) (*DroneSession, *telemetryStreamBinding, *telemetryStreamBinding, error) {
	r.sessionsMu.RLock()
	defer r.sessionsMu.RUnlock()

	session, ok := r.grpcSessions[agentID]
	if !ok || session.SessionID != sessionID || session.retired {
		return nil, nil, nil, ErrSessionNotFound
	}

	if r.registryReporter == nil {
		session.controlStreamMu.Lock()
		defer session.controlStreamMu.Unlock()
	}
	session.sessionMu.Lock()
	defer session.sessionMu.Unlock()

	previous := session.stream
	session.streamGeneration++
	binding := &telemetryStreamBinding{
		stream:     stream,
		generation: session.streamGeneration,
	}
	if r.registryReporter == nil {
		session.stream = binding
	} else {
		session.pendingStream = binding
	}

	return session, binding, previous, nil
}

func (r *Relay) registerActiveAgent(
	ctx context.Context,
	agentID string,
	expectedSession *DroneSession,
	expectedStream *telemetryStreamBinding,
	_ *telemetryStreamBinding,
) error {
	if r.registryReporter == nil {
		return nil
	}
	expectedSession.publicationMu.Lock()
	defer expectedSession.publicationMu.Unlock()

	// Registration replacement and active-stream cleanup take the matching
	// ownership write lease before changing the session map. The read lease does
	// not block admissions on the previously accepted stream, unlike sessionMu.
	// Keep it through Registry publication so the liveness record linearizes
	// while this exact session still owns the Agent.
	expectedSession.ownershipMu.RLock()
	defer expectedSession.ownershipMu.RUnlock()

	r.sessionsMu.RLock()
	currentSession := r.grpcSessions[agentID]
	r.sessionsMu.RUnlock()
	if currentSession != expectedSession || expectedSession.retired {
		return ErrSessionNotFound
	}

	expectedSession.sessionMu.Lock()
	isCandidate := expectedSession.pendingStream == expectedStream
	// Direct tests and embedders may pass the already-active binding. Production
	// replacement candidates remain pending until publication succeeds.
	isActive := expectedSession.stream == expectedStream
	closed := expectedStream.closed
	expectedSession.sessionMu.Unlock()
	if (!isCandidate && !isActive) || closed {
		return ErrSessionNotFound
	}
	if err := r.registryReporter.RegisterAgent(ctx, agentID); err != nil {
		expectedSession.sessionMu.Lock()
		if expectedSession.pendingStream == expectedStream {
			expectedSession.pendingStream = nil
		}
		expectedSession.sessionMu.Unlock()
		return err
	}

	// Publication may remain slow without blocking accepted telemetry. Only the
	// final active-binding swap excludes command writes and command evidence.
	expectedSession.controlStreamMu.Lock()
	defer expectedSession.controlStreamMu.Unlock()
	expectedSession.sessionMu.Lock()
	defer expectedSession.sessionMu.Unlock()
	if isActive {
		return nil
	}
	if expectedSession.pendingStream != expectedStream || expectedStream.closed {
		return ErrSessionNotFound
	}
	// The prior accepted binding stays current and can route telemetry throughout
	// the Registry RPC. Commit the replacement only after publication succeeds.
	expectedSession.stream = expectedStream
	expectedSession.pendingStream = nil
	return nil
}

func (r *Relay) deleteStream(agentID string, expectedSession *DroneSession, expectedStream *telemetryStreamBinding) {
	expectedSession.ownershipMu.Lock()
	r.sessionsMu.Lock()

	session, ok := r.grpcSessions[agentID]
	if !ok || session != expectedSession {
		r.sessionsMu.Unlock()
		expectedSession.ownershipMu.Unlock()
		return
	}

	session.sessionMu.Lock()
	expectedStream.closed = true
	if session.pendingStream == expectedStream {
		session.pendingStream = nil
	}
	isCurrentStream := session.stream == expectedStream || (session.stream == nil && session.pendingStream == nil)
	session.sessionMu.Unlock()
	if isCurrentStream {
		session.retired = true
		session.abortPendingCommands()
		delete(r.grpcSessions, agentID)
		// Stop liveness before releasing the session-map lock. Otherwise a new
		// registration could publish and activate a replacement stream between
		// this deletion and StopAgent, and the old cleanup would stop the new
		// generation's heartbeats.
		if r.registryReporter != nil {
			r.registryReporter.StopAgent(agentID)
		}
	}
	r.sessionsMu.Unlock()
	expectedSession.ownershipMu.Unlock()
}
