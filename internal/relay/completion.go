package relay

import (
	"context"
	"errors"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	rpc "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/makinje/aero-arc-relay/internal/completionoutbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *Relay) admitFlightCompletion(ctx context.Context, agentID string, session *DroneSession, binding *telemetryStreamBinding, e *pb.FlightCompletionEvidence) error {
	if r.completionOutbox == nil {
		return status.Error(codes.Unavailable, "durable completion outbox is not configured")
	}
	if r.agentAuthenticator == nil || e.GetAgentId() != agentID || r.config == nil {
		return status.Error(codes.PermissionDenied, "authenticated completion producer required")
	}
	mapping, ok := r.config.Telemetry.AgentMappings[agentID]
	if !ok || mapping.AircraftID != e.GetContext().GetAircraftId() {
		return status.Error(codes.PermissionDenied, "completion aircraft mapping mismatch")
	}
	// Historical events need not match the current flight context: an offline
	// Agent can replay the old flight after a new context has been installed.
	session.ownershipMu.Lock()
	r.sessionsMu.RLock()
	current := r.grpcSessions[agentID] == session && !session.retired
	r.sessionsMu.RUnlock()
	session.sessionMu.RLock()
	current = current && session.stream == binding && !binding.closed
	session.sessionMu.RUnlock()
	if !current {
		session.ownershipMu.Unlock()
		return status.Error(codes.Aborted, "completion stream was replaced")
	}
	receipt, err := r.completionOutbox.Admit(ctx, e)
	session.ownershipMu.Unlock()
	if errors.Is(err, completionoutbox.ErrConflict) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	if errors.Is(err, completionoutbox.ErrInvalid) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	return sendOnStream(binding, &pb.RelayStreamMessage{Payload: &pb.RelayStreamMessage_FlightCompletionReceipt{FlightCompletionReceipt: receipt}})
}

// ListFlightCompletions returns durable pending events to an authenticated API.
// Events survive Agent disconnects and are retained until exact acknowledgement.
func (r *Relay) ListFlightCompletions(ctx context.Context, req *rpc.ListFlightCompletionsRequest) (*rpc.ListFlightCompletionsResponse, error) {
	if err := r.authorizeControlMutation(ctx); err != nil {
		return nil, err
	}
	if r.completionOutbox == nil {
		return nil, status.Error(codes.Unavailable, "completion outbox unavailable")
	}
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = 100
	}
	if limit > 200 {
		return nil, status.Error(codes.InvalidArgument, "limit exceeds 200")
	}
	events, err := r.completionOutbox.Pending(ctx, limit)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return &rpc.ListFlightCompletionsResponse{Events: events}, nil
}

// AckFlightCompletions records API durable admission for exact event digests.
// Repeating a receipt is idempotent and never acknowledges different content.
func (r *Relay) AckFlightCompletions(ctx context.Context, req *rpc.AckFlightCompletionsRequest) (*rpc.AckFlightCompletionsResponse, error) {
	if err := r.authorizeControlMutation(ctx); err != nil {
		return nil, err
	}
	if r.completionOutbox == nil {
		return nil, status.Error(codes.Unavailable, "completion outbox unavailable")
	}
	if len(req.GetReceipts()) > 200 {
		return nil, status.Error(codes.InvalidArgument, "too many receipts")
	}
	for _, receipt := range req.GetReceipts() {
		if err := r.completionOutbox.Acknowledge(ctx, receipt); err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
	}
	return &rpc.AckFlightCompletionsResponse{}, nil
}
