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
	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
)

func (r *Relay) updateStream(agentID string, stream agentv1.AgentGateway_TelemetryStreamServer) (*DroneSession, *telemetryStreamBinding, error) {
	r.sessionsMu.RLock()
	defer r.sessionsMu.RUnlock()

	session, ok := r.grpcSessions[agentID]
	if !ok {
		return nil, nil, ErrSessionNotFound
	}

	session.sessionMu.Lock()
	defer session.sessionMu.Unlock()

	session.streamGeneration++
	binding := &telemetryStreamBinding{
		stream:     stream,
		generation: session.streamGeneration,
	}
	session.stream = binding

	return session, binding, nil
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

	session.sessionMu.RLock()
	isCurrentStream := session.stream == expectedStream && session.streamGeneration == expectedStream.generation
	session.sessionMu.RUnlock()
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
