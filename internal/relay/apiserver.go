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

func (s *Relay) SetOperationContext(ctx context.Context, req *pb.SetOperationContextRequest) (*pb.SetOperationContextResponse, error) {
	if req.GetCommand() == nil || req.GetCommand().GetContext() == nil {
		return nil, status.Error(codes.InvalidArgument, "set operation context command is required")
	}
	if strings.TrimSpace(req.GetCommand().GetContext().GetFlightId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "flight ID is required")
	}
	ack, err := s.deliverOperationCommand(ctx, req.GetAgentId(), req.GetCommand().GetCommandId(), &agentv1.RelayStreamMessage{
		Payload: &agentv1.RelayStreamMessage_SetOperationContext{SetOperationContext: req.GetCommand()},
	})
	if err != nil {
		return nil, err
	}
	return &pb.SetOperationContextResponse{Result: ack}, nil
}

func (s *Relay) ClearOperationContext(ctx context.Context, req *pb.ClearOperationContextRequest) (*pb.ClearOperationContextResponse, error) {
	if req.GetCommand() == nil || strings.TrimSpace(req.GetCommand().GetFlightId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "clear operation context command and flight ID are required")
	}
	ack, err := s.deliverOperationCommand(ctx, req.GetAgentId(), req.GetCommand().GetCommandId(), &agentv1.RelayStreamMessage{
		Payload: &agentv1.RelayStreamMessage_ClearOperationContext{ClearOperationContext: req.GetCommand()},
	})
	if err != nil {
		return nil, err
	}
	return &pb.ClearOperationContextResponse{Result: ack}, nil
}

func (s *Relay) deliverOperationCommand(ctx context.Context, agentID, commandID string, message *agentv1.RelayStreamMessage) (*agentv1.OperationContextCommandAck, error) {
	agentID = strings.TrimSpace(agentID)
	commandID = strings.TrimSpace(commandID)
	if agentID == "" || commandID == "" {
		return nil, status.Error(codes.InvalidArgument, "agent ID and command ID are required")
	}
	s.sessionsMu.RLock()
	session, ok := s.grpcSessions[agentID]
	s.sessionsMu.RUnlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "agent is not registered on this relay")
	}
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
	if err := s.sendToAgent(agentID, message); err != nil {
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
