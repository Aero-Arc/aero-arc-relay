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

func (r *Relay) updateStream(agentID string, stream agentv1.AgentGateway_TelemetryStreamServer) (string, error) {
	// 1. Lock map to find session
	r.sessionsMu.RLock()
	session, ok := r.grpcSessions[agentID]

	// 2. Handle missing session
	if !ok {
		r.sessionsMu.RUnlock()
		return "", ErrSessionNotFound
	}

	storedID := session.SessionID
	r.sessionsMu.RUnlock()

	// 3. Update stream safely
	session.sessionMu.Lock()
	session.stream = stream
	session.sessionMu.Unlock()

	return storedID, nil
}

func (r *Relay) deleteStream(agentID, sessionID string) {
	r.sessionsMu.Lock()
	defer r.sessionsMu.Unlock()

	session, ok := r.grpcSessions[agentID]
	if ok && session.SessionID == sessionID {
		delete(r.grpcSessions, agentID)
	}
}
