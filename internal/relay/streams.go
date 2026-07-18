package relay

import (
	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
)

func (r *Relay) updateStream(agentID string, stream agentv1.AgentGateway_TelemetryStreamServer) (*DroneSession, uint64, error) {
	r.sessionsMu.RLock()
	defer r.sessionsMu.RUnlock()

	session, ok := r.grpcSessions[agentID]
	if !ok {
		return nil, 0, ErrSessionNotFound
	}

	// Serialize replacement with sends that target the active stream. This keeps
	// a command from selecting one stream while a replacement installs another.
	session.sendMu.Lock()
	defer session.sendMu.Unlock()
	session.sessionMu.Lock()
	defer session.sessionMu.Unlock()

	session.streamGeneration++
	session.stream = stream

	return session, session.streamGeneration, nil
}

func (r *Relay) deleteStream(agentID string, expectedSession *DroneSession, streamGeneration uint64) {
	r.sessionsMu.Lock()
	defer r.sessionsMu.Unlock()

	session, ok := r.grpcSessions[agentID]
	if !ok || session != expectedSession {
		return
	}

	session.sessionMu.RLock()
	isCurrentStream := session.streamGeneration == streamGeneration
	session.sessionMu.RUnlock()
	if isCurrentStream {
		delete(r.grpcSessions, agentID)
	}
}
