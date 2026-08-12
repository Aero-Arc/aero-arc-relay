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

	session.sessionMu.Lock()
	defer session.sessionMu.Unlock()

	previous := session.stream
	session.streamGeneration++
	binding := &telemetryStreamBinding{
		stream:     stream,
		generation: session.streamGeneration,
	}
	session.stream = binding

	return session, binding, previous, nil
}

func (r *Relay) registerActiveAgent(
	ctx context.Context,
	agentID string,
	expectedSession *DroneSession,
	expectedStream *telemetryStreamBinding,
	previousStream *telemetryStreamBinding,
) error {
	if r.registryReporter == nil {
		return nil
	}

	// Registration replacement and active-stream cleanup take the matching
	// ownership write lease before changing the session map. Keep the read lease
	// through Registry publication so the liveness record linearizes while this
	// exact session and stream binding still own the Agent.
	expectedSession.ownershipMu.RLock()
	defer expectedSession.ownershipMu.RUnlock()

	r.sessionsMu.RLock()
	currentSession := r.grpcSessions[agentID]
	r.sessionsMu.RUnlock()
	if currentSession != expectedSession || expectedSession.retired {
		return ErrSessionNotFound
	}

	expectedSession.sessionMu.Lock()
	defer expectedSession.sessionMu.Unlock()
	if expectedSession.stream != expectedStream || expectedStream.closed {
		return ErrSessionNotFound
	}
	if err := r.registryReporter.RegisterAgent(ctx, agentID); err != nil {
		// Restore only a binding whose cleanup has not already run. A closed
		// previous stream must never be resurrected after a failed replacement.
		if previousStream != nil && !previousStream.closed {
			expectedSession.stream = previousStream
		}
		return err
	}
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
	isCurrentStream := session.stream == expectedStream
	session.sessionMu.Unlock()
	if isCurrentStream {
		session.retired = true
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
