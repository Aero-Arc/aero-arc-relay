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
	"log/slog"
	"strings"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const operationContextControlDisabled = "operation-context control is disabled until mTLS control authentication is configured"

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

// GetDroneStatus returns Relay-local state for one registered Agent. The
// snapshot may represent a pending registration before telemetry-stream admission.
//
// Parameters:
//   - ctx: is accepted for RPC lifecycle compatibility; this in-memory read
//     completes synchronously.
//   - req: identifies the Agent through its drone_id field.
//
// Returns:
//   - response: contains the matching session snapshot.
//   - error: reports an empty ID or an Agent without a registered session.
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
func (s *Relay) SetOperationContext(ctx context.Context, req *pb.SetOperationContextRequest) (*pb.SetOperationContextResponse, error) {
	if err := s.authorizeControlMutation(ctx); err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(req.GetAgentId())
	command := req.GetCommand()
	if agentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agent ID is required")
	}
	if command == nil || strings.TrimSpace(command.GetCommandId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "operation-context command ID is required")
	}
	operation := command.GetContext()
	if operation == nil || strings.TrimSpace(operation.GetFlightId()) == "" || strings.TrimSpace(operation.GetIntentId()) == "" || operation.GetIntentVersion() == 0 {
		return nil, status.Error(codes.InvalidArgument, "flight ID, intent ID, and positive intent version are required")
	}
	session, err := s.currentControlSession(agentID)
	if err != nil {
		return nil, err
	}
	ack, err := deliverOperationCommandToSession(ctx, session, command.GetCommandId(), &agentv1.RelayStreamMessage{
		Payload: &agentv1.RelayStreamMessage_SetOperationContext{SetOperationContext: command},
	})
	if err != nil {
		return nil, err
	}
	if !s.sessionIsCurrent(agentID, session) {
		return nil, status.Error(codes.Aborted, "agent session changed while applying operation context")
	}
	return &pb.SetOperationContextResponse{Result: ack}, nil
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
func (s *Relay) ClearOperationContext(ctx context.Context, req *pb.ClearOperationContextRequest) (*pb.ClearOperationContextResponse, error) {
	if err := s.authorizeControlMutation(ctx); err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(req.GetAgentId())
	command := req.GetCommand()
	if agentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agent ID is required")
	}
	if command == nil || strings.TrimSpace(command.GetCommandId()) == "" || strings.TrimSpace(command.GetFlightId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "operation-context command ID and flight ID are required")
	}
	session, err := s.currentControlSession(agentID)
	if err != nil {
		return nil, err
	}
	ack, err := deliverOperationCommandToSession(ctx, session, command.GetCommandId(), &agentv1.RelayStreamMessage{
		Payload: &agentv1.RelayStreamMessage_ClearOperationContext{ClearOperationContext: command},
	})
	if err != nil {
		return nil, err
	}
	if !s.sessionIsCurrent(agentID, session) {
		return nil, status.Error(codes.Aborted, "agent session changed while clearing operation context")
	}
	return &pb.ClearOperationContextResponse{Result: ack}, nil
}

func (s *Relay) authorizeControlMutation(ctx context.Context) error {
	if s.controlAuthorizer == nil {
		return status.Error(codes.Unimplemented, operationContextControlDisabled)
	}
	return s.controlAuthorizer(ctx)
}

func (s *Relay) currentControlSession(agentID string) (*DroneSession, error) {
	s.sessionsMu.RLock()
	session := s.grpcSessions[agentID]
	s.sessionsMu.RUnlock()
	if session == nil {
		return nil, status.Error(codes.NotFound, "agent is not registered")
	}
	session.ownershipMu.RLock()
	defer session.ownershipMu.RUnlock()
	if !s.sessionIsCurrent(agentID, session) || session.retired {
		return nil, status.Error(codes.Aborted, "agent session changed before operation-context delivery")
	}
	return session, nil
}

func (s *Relay) sessionIsCurrent(agentID string, expected *DroneSession) bool {
	s.sessionsMu.RLock()
	current := s.grpcSessions[agentID]
	s.sessionsMu.RUnlock()
	return current == expected
}

// SendAircraftCommand delivers an immediate ARM or DISARM command to the
// Agent session active at RPC admission and waits for its correlated result.
// The command is never queued for a later or replacement session.
//
// Parameters:
//   - ctx: bounds delivery and the wait for the Agent/autopilot result.
//   - req: identifies the Agent route and carries the aircraft command envelope.
//
// Returns:
//   - response: contains the Agent's correlated autopilot-level result.
//   - error: reports invalid input, an offline/replaced Agent session, stream
//     delivery failure, deadline expiry, or a malformed Agent result.
func (s *Relay) SendAircraftCommand(ctx context.Context, req *pb.SendAircraftCommandRequest) (*pb.SendAircraftCommandResponse, error) {
	agentID := strings.TrimSpace(req.GetAgentId())
	command := req.GetCommand()
	if agentID == "" || command == nil || strings.TrimSpace(command.GetCommandId()) == "" || strings.TrimSpace(command.GetAircraftId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id, command_id, and aircraft_id are required")
	}
	if command.GetType() != agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_ARM &&
		command.GetType() != agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_DISARM {
		return nil, status.Error(codes.InvalidArgument, "aircraft command type must be ARM or DISARM")
	}

	s.sessionsMu.RLock()
	session := s.grpcSessions[agentID]
	s.sessionsMu.RUnlock()
	if session == nil {
		return nil, status.Error(codes.NotFound, "agent is not connected")
	}

	// Pin this exact session generation through result correlation. Registration
	// replacement and stream cleanup require the write side of this lease, so an
	// admitted command cannot migrate to a reconnecting Agent.
	session.ownershipMu.RLock()
	defer session.ownershipMu.RUnlock()
	s.sessionsMu.RLock()
	current := s.grpcSessions[agentID] == session && !session.retired
	s.sessionsMu.RUnlock()
	if !current {
		return nil, status.Error(codes.NotFound, "agent session is no longer active")
	}
	session.sessionMu.RLock()
	connected := session.stream != nil
	sessionID := session.SessionID
	session.sessionMu.RUnlock()
	if !connected {
		return nil, status.Error(codes.NotFound, "agent stream is not connected")
	}

	startedAt := time.Now()
	slog.LogAttrs(ctx, slog.LevelInfo, "command_delivered_to_agent",
		slog.String("command_id", command.GetCommandId()),
		slog.String("aircraft_id", command.GetAircraftId()),
		slog.String("agent_id", agentID),
		slog.String("session_id", sessionID),
		slog.String("command_type", command.GetType().String()),
	)
	result, err := deliverAircraftCommandToSession(ctx, session, command)
	if err != nil {
		relayAircraftCommandsTotal.WithLabelValues(command.GetType().String(), "delivery_failed").Inc()
		return nil, err
	}
	if result.GetCommandId() != command.GetCommandId() || result.GetAircraftId() != command.GetAircraftId() {
		return nil, status.Error(codes.Internal, "agent returned a mismatched aircraft command result")
	}
	duration := time.Since(startedAt)
	relayAircraftCommandsTotal.WithLabelValues(command.GetType().String(), result.GetStatus().String()).Inc()
	relayAircraftCommandDuration.WithLabelValues(command.GetType().String()).Observe(duration.Seconds())
	slog.LogAttrs(ctx, slog.LevelInfo, "command_completed",
		slog.String("command_id", command.GetCommandId()),
		slog.String("aircraft_id", command.GetAircraftId()),
		slog.String("agent_id", agentID),
		slog.String("session_id", sessionID),
		slog.String("command_type", command.GetType().String()),
		slog.String("result", result.GetStatus().String()),
		slog.Duration("duration", duration),
	)
	return &pb.SendAircraftCommandResponse{Result: result}, nil
}

func deliverAircraftCommandToSession(ctx context.Context, session *DroneSession, command *agentv1.AircraftCommand) (*agentv1.AircraftCommandResult, error) {
	pending := make(chan *agentv1.AircraftCommandResult, 1)
	session.pendingMu.Lock()
	if session.pendingAircraft == nil {
		session.pendingAircraft = make(map[string]chan *agentv1.AircraftCommandResult)
	}
	if _, exists := session.pendingAircraft[command.GetCommandId()]; exists {
		session.pendingMu.Unlock()
		return nil, status.Error(codes.AlreadyExists, "aircraft command is already pending")
	}
	session.pendingAircraft[command.GetCommandId()] = pending
	session.pendingMu.Unlock()
	cleanup := func() {
		session.pendingMu.Lock()
		delete(session.pendingAircraft, command.GetCommandId())
		session.pendingMu.Unlock()
	}
	if err := sendToSession(ctx, session, &agentv1.RelayStreamMessage{
		Payload: &agentv1.RelayStreamMessage_AircraftCommand{AircraftCommand: command},
	}); err != nil {
		cleanup()
		return nil, err
	}
	select {
	case result := <-pending:
		return result, nil
	case <-ctx.Done():
		cleanup()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
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
