package relay

import (
	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
)

func (r *Relay) updateStream(sessionID string, stream agentv1.AgentGateway_TelemetryStreamServer) error {
	// 1. Lock map to find session
	r.sessionsMu.RLock()
	session, ok := r.grpcSessions[sessionID]
	r.sessionsMu.RUnlock()

	// 2. Handle missing session
	if !ok {
		return ErrSessionNotFound
	}

	// 3. Update stream safely
	session.sessionMu.Lock()
	session.stream = stream
	session.sessionMu.Unlock()

	return nil
}
