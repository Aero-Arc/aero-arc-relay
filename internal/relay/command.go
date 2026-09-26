package relay

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/aero-arc/aero-arc-protos/commanddigest"
	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc"
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
	var result *pb.ExchangeCommandResponse
	err := s.exchangeCommand(ctx, req, false, func(e *agentv1.CommandEvidence) error {
		result = &pb.ExchangeCommandResponse{Evidence: e}
		return nil
	})
	return result, err
}

// ExecuteCommand delivers immutable authority once and streams cumulative durable
// evidence until completion or disconnect. Recovery must reuse the same identity.
//
// Parameters: req carries command authority; stream authenticates the caller and
// bounds delivery. Returns an authorization, delivery, or stream error; loss of
// the stream never proves that the aircraft action failed.
func (s *Relay) ExecuteCommand(req *pb.ExecuteCommandRequest, stream grpc.ServerStreamingServer[pb.ExecuteCommandResponse]) error {
	return s.exchangeCommand(stream.Context(), &pb.ExchangeCommandRequest{AgentId: req.AgentId, Command: req.Command, AttemptId: req.AttemptId}, true, func(e *agentv1.CommandEvidence) error {
		return stream.Send(&pb.ExecuteCommandResponse{Evidence: e})
	})
}

func commandEvidenceComplete(e *agentv1.CommandEvidence) bool {
	if e.GetDeliveryComplete() {
		return true
	}
	applied, observed := false, false
	for _, event := range e.Events {
		switch event.Stage {
		case "rejected", "outcome_unknown":
			return true
		case "applied":
			applied = true
		case "observed", "observation_unavailable", "observation_superseded":
			observed = true
		}
	}
	return applied && observed
}

func (s *Relay) exchangeCommand(ctx context.Context, req *pb.ExchangeCommandRequest, streaming bool, emit func(*agentv1.CommandEvidence) error) error {
	if err := s.authorizeControlMutation(ctx); err != nil {
		return err
	}
	receivedAt := time.Now().UnixMilli()
	c := req.GetCommand()
	d, err := commanddigest.Digest(c)
	if err != nil || req.GetAttemptId() == "" || c.GetCommandId() == "" || d != c.GetCommandDigest() || req.GetAgentId() != c.GetAgentId() {
		return status.Error(codes.InvalidArgument, "invalid command identity")
	}
	if s.config == nil {
		return status.Error(codes.FailedPrecondition, "mapping unavailable")
	}
	mapping, ok := s.config.Telemetry.AgentMappings[req.AgentId]
	if !ok || mapping.AircraftID != c.AircraftId || mapping.OperatorID != c.OperatorId {
		return status.Error(codes.PermissionDenied, "target binding mismatch")
	}
	s.sessionsMu.RLock()
	session := s.grpcSessions[req.AgentId]
	s.sessionsMu.RUnlock()
	if session == nil {
		return status.Error(codes.Unavailable, "agent offline")
	}
	if !slices.Contains(session.executionCapabilities, c.Capability) {
		return status.Error(codes.FailedPrecondition, "Agent execution capability unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	release, err := acquireOperationCommandSlot(ctx, session)
	if err != nil {
		return err
	}
	var releaseOnce sync.Once
	defer releaseOnce.Do(release)
	if _, err = s.lockCurrentMissionSession(req.AgentId, session); err != nil {
		return err
	}
	session.controlStreamMu.RLock()
	ch := make(chan *agentv1.CommandEvidence, 1)
	session.pendingMu.Lock()
	if session.c2Pending == nil {
		session.c2Pending = map[string]chan *agentv1.CommandEvidence{}
	}
	if session.c2Pending[c.CommandId] != nil {
		session.pendingMu.Unlock()
		session.controlStreamMu.RUnlock()
		session.ownershipMu.RUnlock()
		return status.Error(codes.Aborted, "command evidence stream already active")
	}
	session.c2Pending[c.CommandId] = ch
	session.pendingMu.Unlock()
	defer func() { session.pendingMu.Lock(); delete(session.c2Pending, c.CommandId); session.pendingMu.Unlock() }()
	_, err = sendToSessionWithWritePolicy(ctx, session, &agentv1.RelayStreamMessage{Payload: &agentv1.RelayStreamMessage_DurableCommand{DurableCommand: proto.Clone(c).(*agentv1.DurableCommand)}}, true)
	session.controlStreamMu.RUnlock()
	session.ownershipMu.RUnlock()
	if err != nil {
		return err
	}
	dispatchedAt := time.Now().UnixMilli()
	for {
		select {
		case e := <-ch:
			releaseOnce.Do(release)
			if e.CommandDigest != d {
				return status.Error(codes.DataLoss, "command digest mismatch")
			}
			e.Events = append(e.Events,
				&agentv1.CommandEvent{EventId: req.AttemptId + "/relay_received", Stage: "relay_received", OccurredAtUnixMs: receivedAt, EvidenceSource: "relay:" + s.config.Registry.RelayID, Message: "Relay admitted delivery attempt"},
				&agentv1.CommandEvent{EventId: req.AttemptId + "/dispatched", Stage: "dispatched", OccurredAtUnixMs: dispatchedAt, EvidenceSource: "relay:" + s.config.Registry.RelayID, Message: "command handed to bound Agent stream"})
			if err := emit(e); err != nil {
				return err
			}
			if !streaming || commandEvidenceComplete(e) {
				return nil
			}
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		}
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
			// Evidence is cumulative. Keep the newest snapshot when the consumer
			// is slower than Agent, including completion emitted immediately after ACK.
			select {
			case <-ch:
			default:
			}
			ch <- proto.Clone(e).(*agentv1.CommandEvidence)
		}
	}
}
