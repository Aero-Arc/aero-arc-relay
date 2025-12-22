package relay

import (
	"log/slog"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
)

func (r *Relay) updateStream(sessionID string, stream agentv1.AgentGateway_TelemetryStreamServer) {
	// 1. Lock map to find session
	r.sessionsMu.RLock()
	session, ok := r.grpcSessions[sessionID]
	r.sessionsMu.RUnlock()

	// 2. Handle missing session
	if !ok {
		slog.Warn("Attempted to update stream for unknown session", "session_id", sessionID)
		return
	}

	// 3. Update stream safely
	session.sessionMu.Lock()
	session.stream = stream
	session.sessionMu.Unlock()
}
