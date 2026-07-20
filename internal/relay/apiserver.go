package relay

import (
	"context"
	"strings"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const operationContextControlDisabled = "operation-context control is disabled until the relay control plane is authenticated and authorized"

func (s *Relay) ListActiveDrones(context.Context, *pb.ListActiveDronesRequest) (*pb.ListActiveDronesResponse, error) {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	response := &pb.ListActiveDronesResponse{Drones: make([]*pb.DroneStatus, 0, len(s.grpcSessions))}
	for _, session := range s.grpcSessions {
		response.Drones = append(response.Drones, droneStatus(session))
	}
	return response, nil
}

func (s *Relay) GetDroneStatus(_ context.Context, req *pb.GetDroneStatusRequest) (*pb.GetDroneStatusResponse, error) {
	agentID := strings.TrimSpace(req.GetDroneId())
	if agentID == "" {
		return nil, status.Error(codes.InvalidArgument, "drone ID is required")
	}
	s.sessionsMu.RLock()
	session, ok := s.grpcSessions[agentID]
	s.sessionsMu.RUnlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "drone is not connected")
	}
	return &pb.GetDroneStatusResponse{Drone: droneStatus(session)}, nil
}

func (*Relay) SetOperationContext(context.Context, *pb.SetOperationContextRequest) (*pb.SetOperationContextResponse, error) {
	return nil, status.Error(codes.Unimplemented, operationContextControlDisabled)
}

func (*Relay) ClearOperationContext(context.Context, *pb.ClearOperationContextRequest) (*pb.ClearOperationContextResponse, error) {
	return nil, status.Error(codes.Unimplemented, operationContextControlDisabled)
}

func deliverOperationCommandToSession(ctx context.Context, session *DroneSession, commandID string, message *agentv1.RelayStreamMessage) (*agentv1.OperationContextCommandAck, error) {
	pending := make(chan *agentv1.OperationContextCommandAck, 1)
	session.pendingMu.Lock()
	if session.pending == nil {
		session.pending = make(map[string]chan *agentv1.OperationContextCommandAck)
	}
	if _, exists := session.pending[commandID]; exists {
		session.pendingMu.Unlock()
		return nil, status.Error(codes.AlreadyExists, "operation-context command is already pending")
	}
	session.pending[commandID] = pending
	session.pendingMu.Unlock()
	cleanup := func() {
		session.pendingMu.Lock()
		delete(session.pending, commandID)
		session.pendingMu.Unlock()
	}
	if err := sendToSession(ctx, session, message); err != nil {
		cleanup()
		return nil, err
	}
	select {
	case ack := <-pending:
		return ack, nil
	case <-ctx.Done():
		cleanup()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func droneStatus(session *DroneSession) *pb.DroneStatus {
	session.sessionMu.RLock()
	defer session.sessionMu.RUnlock()
	return &pb.DroneStatus{
		DroneId:             session.agentID,
		SessionId:           session.SessionID,
		AgentId:             session.agentID,
		ConnectedAtUnixNs:   session.ConnectedAt.UnixNano(),
		LastHeartbeatUnixNs: session.LastHeartbeat.UnixNano(),
		FlightId:            session.FlightID,
		IntentId:            session.IntentID,
		IntentVersion:       session.IntentVersion,
	}
}
