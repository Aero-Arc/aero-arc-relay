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
	defer expectedSession.ownershipMu.Unlock()

	r.sessionsMu.Lock()
	defer r.sessionsMu.Unlock()

	session, ok := r.grpcSessions[agentID]
	if !ok || session != expectedSession {
		return
	}

	session.sessionMu.RLock()
	isCurrentStream := session.stream == expectedStream && session.streamGeneration == expectedStream.generation
	session.sessionMu.RUnlock()
	if isCurrentStream {
		session.retired = true
		delete(r.grpcSessions, agentID)
	}
}
