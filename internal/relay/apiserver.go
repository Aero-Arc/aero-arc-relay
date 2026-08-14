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
	"context"
	"strings"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const operationContextControlDisabled = "operation-context control is experimental and disabled until the relay control plane is authenticated and authorized"

// ListActiveDrones snapshots the Relay's currently admitted Agent sessions as
// drone-status records.
//
// Parameters:
//   - ctx: is accepted for RPC lifecycle compatibility; this in-memory read
//     completes synchronously.
//   - request: carries no filters in the current Relay API.
//
// Returns:
//   - response: contains one status record per currently admitted session.
//   - error: is currently always nil.
func (s *Relay) ListActiveDrones(context.Context, *pb.ListActiveDronesRequest) (*pb.ListActiveDronesResponse, error) {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	response := &pb.ListActiveDronesResponse{Drones: make([]*pb.DroneStatus, 0, len(s.grpcSessions))}
	for _, session := range s.grpcSessions {
		response.Drones = append(response.Drones, droneStatus(session))
	}
	return response, nil
}

// GetDroneStatus returns the current Relay-local session state for one Agent.
//
// Parameters:
//   - ctx: is accepted for RPC lifecycle compatibility; this in-memory read
//     completes synchronously.
//   - req: identifies the Agent through its drone_id field.
//
// Returns:
//   - response: contains the matching session snapshot.
//   - error: reports an empty ID or an Agent without an active session.
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

// SetOperationContext rejects operation-context mutation while the Relay
// control plane lacks its final authentication and authorization policy.
//
// Parameters:
//   - ctx: is reserved for the future authenticated control-plane lifecycle.
//   - request: is reserved for the future operation-context command.
//
// Returns:
//   - response: is always nil while the RPC is disabled.
//   - error: is a gRPC Unimplemented status explaining the security gate.
func (*Relay) SetOperationContext(context.Context, *pb.SetOperationContextRequest) (*pb.SetOperationContextResponse, error) {
	return nil, status.Error(codes.Unimplemented, operationContextControlDisabled)
}

// ClearOperationContext rejects operation-context mutation while the Relay
// control plane lacks its final authentication and authorization policy.
//
// Parameters:
//   - ctx: is reserved for the future authenticated control-plane lifecycle.
//   - request: is reserved for the future operation-context command.
//
// Returns:
//   - response: is always nil while the RPC is disabled.
//   - error: is a gRPC Unimplemented status explaining the security gate.
func (*Relay) ClearOperationContext(context.Context, *pb.ClearOperationContextRequest) (*pb.ClearOperationContextResponse, error) {
	return nil, status.Error(codes.Unimplemented, operationContextControlDisabled)
}

// deliverOperationCommandToSession is experimental command-delivery machinery.
// The public mutation RPCs remain disabled until the control plane is
// authenticated, authorized, and its command lifecycle is finalized.
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
