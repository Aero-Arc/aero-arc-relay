package relay

import (
	"context"
	"slices"
	"time"

	"github.com/aero-arc/aero-arc-protos/commanddigest"
	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// ExchangeCommand forwards immutable authority to an authenticated, bound Agent
// and returns its replayable evidence. A transport error proves no execution
// outcome; callers must reconcile the same identity. Relay retains no authority.
//
// Parameters:
//   - ctx: authenticates the API and bounds this delivery attempt only.
//   - req: contains the authoritative Agent destination and command.
//
// Returns:
//   - response: contains Agent evidence, never a Relay-generated application ACK.
//   - error: indicates validation, session, delivery, or timeout failure.
func (s *Relay) ExchangeCommand(ctx context.Context, req *pb.ExchangeCommandRequest) (*pb.ExchangeCommandResponse, error) {
	if err := s.authorizeControlMutation(ctx); err != nil {
		return nil, err
	}
	receivedAt := time.Now().UnixMilli()
	c := req.GetCommand()
	d, err := commanddigest.Digest(c)
	if err != nil || req.GetAttemptId() == "" || c.GetCommandId() == "" || d != c.GetCommandDigest() || req.GetAgentId() != c.GetAgentId() {
		return nil, status.Error(codes.InvalidArgument, "invalid command identity")
	}
	if s.config == nil {
		return nil, status.Error(codes.FailedPrecondition, "mapping unavailable")
	}
	mapping, ok := s.config.Telemetry.AgentMappings[req.AgentId]
	if !ok || mapping.AircraftID != c.AircraftId || mapping.OperatorID != c.OperatorId {
		return nil, status.Error(codes.PermissionDenied, "target binding mismatch")
	}
	s.sessionsMu.RLock()
	session := s.grpcSessions[req.AgentId]
	s.sessionsMu.RUnlock()
	if session == nil {
		return nil, status.Error(codes.Unavailable, "agent offline")
	}
	if !slices.Contains(session.executionCapabilities, c.Capability) {
		return nil, status.Error(codes.FailedPrecondition, "Agent execution capability unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	release, err := acquireOperationCommandSlot(ctx, session)
	if err != nil {
		return nil, err
	}
	defer release()
	if _, err = s.lockCurrentMissionSession(req.AgentId, session); err != nil {
		return nil, err
	}
	session.controlStreamMu.RLock()
	ch := make(chan *agentv1.CommandEvidence, 1)
	session.pendingMu.Lock()
	if session.c2Pending == nil {
		session.c2Pending = map[string]chan *agentv1.CommandEvidence{}
	}
	session.c2Pending[c.CommandId] = ch
	session.pendingMu.Unlock()
	defer func() { session.pendingMu.Lock(); delete(session.c2Pending, c.CommandId); session.pendingMu.Unlock() }()
	_, err = sendToSessionWithWritePolicy(ctx, session, &agentv1.RelayStreamMessage{Payload: &agentv1.RelayStreamMessage_DurableCommand{DurableCommand: proto.Clone(c).(*agentv1.DurableCommand)}}, true)
	session.controlStreamMu.RUnlock()
	session.ownershipMu.RUnlock()
	if err != nil {
		return nil, err
	}
	dispatchedAt := time.Now().UnixMilli()
	select {
	case e := <-ch:
		if e.CommandDigest != d {
			return nil, status.Error(codes.DataLoss, "command digest mismatch")
		}
		e.Events = append(e.Events,
			&agentv1.CommandEvent{EventId: req.AttemptId + "/relay_received", Stage: "relay_received", OccurredAtUnixMs: receivedAt, EvidenceSource: "relay:" + s.config.Registry.RelayID, Message: "Relay admitted delivery attempt"},
			&agentv1.CommandEvent{EventId: req.AttemptId + "/dispatched", Stage: "dispatched", OccurredAtUnixMs: dispatchedAt, EvidenceSource: "relay:" + s.config.Registry.RelayID, Message: "command handed to bound Agent stream"})
		return &pb.ExchangeCommandResponse{Evidence: e}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func (s *DroneSession) handleC2Evidence(binding *telemetryStreamBinding, e *agentv1.CommandEvidence) {
	s.controlStreamMu.RLock()
	defer s.controlStreamMu.RUnlock()
	s.sessionMu.RLock()
	current := s.stream == binding
	s.sessionMu.RUnlock()
	if !current {
		return
	}
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if ch := s.c2Pending[e.CommandId]; ch != nil {
		select {
		case ch <- proto.Clone(e).(*agentv1.CommandEvidence):
		default:
		}
	}
}
