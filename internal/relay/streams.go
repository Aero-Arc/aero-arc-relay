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
