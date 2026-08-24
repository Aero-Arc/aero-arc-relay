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
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const operationContextControlDisabled = "operation-context control is disabled until mTLS control authentication is configured"

const (
	operationCommandRetention = 24 * time.Hour
	maxOperationCommands      = 4096
)

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

// SetOperationContext authenticates a control-plane caller, delivers one
// idempotent context mutation to the current Agent session, and waits for an
// acknowledgement whose active context matches the requested value.
//
// Parameters:
//   - ctx: bounds authorization, stream delivery, and acknowledgement waiting.
//   - req: identifies the Agent and carries a durable command ID and context.
//
// Returns:
//   - response: contains the correlated Agent acknowledgement.
//   - error: reports disabled or denied control access, invalid input, command
//     ID reuse with a different payload, missing/replaced sessions, delivery or
//     context cancellation, and malformed or mismatched acknowledgements.
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
	release, err := acquireOperationCommandSlot(ctx, session)
	if err != nil {
		return nil, err
	}
	defer release()
	session.ownershipMu.RLock()
	if !s.sessionIsCurrent(agentID, session) {
		session.ownershipMu.RUnlock()
		return nil, status.Error(codes.Aborted, "agent session changed before operation-context delivery")
	}
	state, owner, err := beginOperationCommandDelivery(ctx, session, command.GetCommandId(), &agentv1.RelayStreamMessage{
		Payload: &agentv1.RelayStreamMessage_SetOperationContext{SetOperationContext: command},
	}, operation)
	session.ownershipMu.RUnlock()
	if err != nil {
		return nil, err
	}
	ack, err := awaitOperationCommand(ctx, session, command.GetCommandId(), state, owner)
	if err != nil {
		return nil, err
	}
	if !s.sessionIsCurrent(agentID, session) {
		return nil, status.Error(codes.Aborted, "agent session changed while applying operation context")
	}
	return &pb.SetOperationContextResponse{Result: ack}, nil
}

// ClearOperationContext authenticates a control-plane caller, conditionally
// clears one flight context on the current Agent session, and waits for a
// correlated acknowledgement of the authoritative resulting context.
//
// Parameters:
//   - ctx: bounds authorization, stream delivery, and acknowledgement waiting.
//   - req: identifies the Agent and carries a durable command ID and flight ID.
//
// Returns:
//   - response: contains the correlated Agent acknowledgement.
//   - error: reports disabled or denied control access, invalid input, command
//     ID reuse with a different payload, missing/replaced sessions, delivery or
//     context cancellation, and malformed or mismatched acknowledgements.
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
	release, err := acquireOperationCommandSlot(ctx, session)
	if err != nil {
		return nil, err
	}
	defer release()
	session.ownershipMu.RLock()
	if !s.sessionIsCurrent(agentID, session) {
		session.ownershipMu.RUnlock()
		return nil, status.Error(codes.Aborted, "agent session changed before operation-context delivery")
	}
	state, owner, err := beginOperationCommandDelivery(ctx, session, command.GetCommandId(), &agentv1.RelayStreamMessage{
		Payload: &agentv1.RelayStreamMessage_ClearOperationContext{ClearOperationContext: command},
	}, expectedContextAfterClear(session, command.GetFlightId()))
	session.ownershipMu.RUnlock()
	if err != nil {
		return nil, err
	}
	ack, err := awaitOperationCommand(ctx, session, command.GetCommandId(), state, owner)
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

// SendAircraftCommand authenticates a control-plane caller, delivers an
// immediate ARM or DISARM command to the Agent session active at RPC admission,
// and waits for its correlated result. The command is never queued for a later
// or replacement session.
//
// Parameters:
//   - ctx: bounds delivery and the wait for the Agent/autopilot result.
//   - req: identifies the Agent route and carries the aircraft command envelope.
//
// Returns:
//   - response: contains the Agent's correlated autopilot-level result.
//   - error: reports disabled or denied control access, invalid input, an
//     offline/replaced Agent session, stream delivery failure, deadline expiry,
//     or a malformed Agent result.
func (s *Relay) SendAircraftCommand(ctx context.Context, req *pb.SendAircraftCommandRequest) (*pb.SendAircraftCommandResponse, error) {
	if err := s.authorizeControlMutation(ctx); err != nil {
		return nil, err
	}
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

	// Pin this exact session generation only through admission and stream send.
	// Waiting while holding the lease would prevent a disconnected Agent from
	// being retired or replaced. Retirement explicitly aborts the pending wait.
	session.ownershipMu.RLock()
	s.sessionsMu.RLock()
	current := s.grpcSessions[agentID] == session && !session.retired
	s.sessionsMu.RUnlock()
	if !current {
		session.ownershipMu.RUnlock()
		return nil, status.Error(codes.NotFound, "agent session is no longer active")
	}
	session.sessionMu.RLock()
	connected := session.stream != nil
	sessionID := session.SessionID
	session.sessionMu.RUnlock()
	if !connected {
		session.ownershipMu.RUnlock()
		return nil, status.Error(codes.NotFound, "agent stream is not connected")
	}

	startedAt := time.Now()
	state, owner, err := beginAircraftCommandDelivery(session, command)
	session.ownershipMu.RUnlock()
	if err != nil {
		relayAircraftCommandsTotal.WithLabelValues(command.GetType().String(), "delivery_failed").Inc()
		return nil, err
	}
	if owner {
		slog.LogAttrs(ctx, slog.LevelInfo, "command_delivery_started",
			slog.String("command_id", command.GetCommandId()),
			slog.String("aircraft_id", command.GetAircraftId()),
			slog.String("agent_id", agentID),
			slog.String("session_id", sessionID),
			slog.String("command_type", command.GetType().String()),
		)
	}
	var result *agentv1.AircraftCommandResult
	select {
	case <-state.done:
		result, err = takeAircraftCommandResult(session, state)
		if err != nil {
			relayAircraftCommandsTotal.WithLabelValues(command.GetType().String(), "delivery_failed").Inc()
			return nil, err
		}
	case <-ctx.Done():
		requestErr := status.FromContextError(ctx.Err()).Err()
		cancelAircraftCommandWaiter(session, command.GetCommandId(), state, requestErr)
		relayAircraftCommandsTotal.WithLabelValues(command.GetType().String(), "delivery_failed").Inc()
		return nil, requestErr
	}
	if result == nil {
		return nil, status.Error(codes.Internal, "agent returned an empty aircraft command result")
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

func beginAircraftCommandDelivery(session *DroneSession, command *agentv1.AircraftCommand) (*aircraftCommandState, bool, error) {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(command)
	if err != nil {
		return nil, false, status.Errorf(codes.Internal, "fingerprint aircraft command: %v", err)
	}
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	session.pendingMu.Lock()
	if session.aircraftCommands == nil {
		session.aircraftCommands = make(map[string]*aircraftCommandState)
	}
	pruneAircraftCommandsLocked(session, time.Now())
	if existing := session.aircraftCommands[command.GetCommandId()]; existing != nil {
		if existing.fingerprint != fingerprint {
			session.pendingMu.Unlock()
			return nil, false, status.Error(codes.AlreadyExists, "aircraft command ID was already used with a different payload")
		}
		existing.waiters++
		session.pendingMu.Unlock()
		return existing, false, nil
	}
	if len(session.aircraftCommands) >= maxOperationCommands {
		session.pendingMu.Unlock()
		return nil, false, status.Error(codes.ResourceExhausted, "aircraft command retention is full")
	}
	deliveryCtx, deliveryCancel := context.WithCancel(context.Background())
	state := &aircraftCommandState{
		fingerprint: fingerprint, done: make(chan struct{}),
		deliveryCancel: deliveryCancel, waiters: 1,
	}
	session.aircraftCommands[command.GetCommandId()] = state
	session.pendingMu.Unlock()
	command = proto.Clone(command).(*agentv1.AircraftCommand)
	go func() {
		if err := sendToSession(deliveryCtx, session, &agentv1.RelayStreamMessage{
			Payload: &agentv1.RelayStreamMessage_AircraftCommand{AircraftCommand: command},
		}); err != nil {
			finishAircraftCommand(session, command.GetCommandId(), state, nil, err)
		}
	}()
	return state, true, nil
}

func pruneAircraftCommandsLocked(session *DroneSession, now time.Time) {
	for commandID, state := range session.aircraftCommands {
		if state.completed && now.Sub(state.completedAt) >= operationCommandRetention {
			delete(session.aircraftCommands, commandID)
		}
	}
	for len(session.aircraftCommands) >= maxOperationCommands {
		var oldestID string
		var oldest time.Time
		for commandID, state := range session.aircraftCommands {
			if !state.completed {
				continue
			}
			if oldestID == "" || state.completedAt.Before(oldest) {
				oldestID = commandID
				oldest = state.completedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(session.aircraftCommands, oldestID)
	}
}

func finishAircraftCommand(session *DroneSession, commandID string, state *aircraftCommandState, result *agentv1.AircraftCommandResult, err error) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	if session.aircraftCommands[commandID] != state || state.completed {
		return
	}
	if result != nil {
		state.result = proto.Clone(result).(*agentv1.AircraftCommandResult)
	}
	state.err = err
	state.completed = true
	state.completedAt = time.Now()
	state.deliveryCancel()
	close(state.done)
}

func takeAircraftCommandResult(session *DroneSession, state *aircraftCommandState) (*agentv1.AircraftCommandResult, error) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	if state.waiters > 0 {
		state.waiters--
	}
	if state.result == nil {
		return nil, state.err
	}
	return proto.Clone(state.result).(*agentv1.AircraftCommandResult), state.err
}

func cancelAircraftCommandWaiter(session *DroneSession, commandID string, state *aircraftCommandState, err error) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	if session.aircraftCommands[commandID] != state {
		return
	}
	if state.waiters > 0 {
		state.waiters--
	}
	if state.waiters != 0 || state.completed {
		return
	}
	state.err = err
	state.completed = true
	state.completedAt = time.Now()
	state.deliveryCancel()
	close(state.done)
}

// deliverOperationCommandToSession retains command fingerprints and terminal
// outcomes for the session, coalesces exact concurrent retries, and rejects a
// command ID reused with a different payload. The Agent WAL provides the
// durable cross-session and cross-process idempotency backstop.
func beginOperationCommandDelivery(
	ctx context.Context,
	session *DroneSession,
	commandID string,
	message *agentv1.RelayStreamMessage,
	expected *agentv1.OperationContext,
) (*operationCommandState, bool, error) {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		return nil, false, status.Errorf(codes.Internal, "fingerprint operation-context command: %v", err)
	}
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	state, owner, err := beginOperationCommand(session, commandID, fingerprint, expected)
	if err != nil {
		return nil, false, err
	}
	if owner {
		if err := sendToSession(ctx, session, message); err != nil {
			finishOperationCommand(session, commandID, state, nil, err)
		}
	}
	return state, owner, nil
}

func awaitOperationCommand(ctx context.Context, session *DroneSession, commandID string, state *operationCommandState, owner bool) (*agentv1.OperationContextCommandAck, error) {
	select {
	case <-state.done:
		return operationCommandResult(session, state)
	case <-ctx.Done():
		requestErr := status.FromContextError(ctx.Err()).Err()
		if owner {
			finishOperationCommand(session, commandID, state, nil, requestErr)
		}
		return nil, requestErr
	}
}

func deliverOperationCommandToSession(ctx context.Context, session *DroneSession, commandID string, message *agentv1.RelayStreamMessage, expected *agentv1.OperationContext) (*agentv1.OperationContextCommandAck, error) {
	state, owner, err := beginOperationCommandDelivery(ctx, session, commandID, message, expected)
	if err != nil {
		return nil, err
	}
	return awaitOperationCommand(ctx, session, commandID, state, owner)
}

func makeOperationGate() chan struct{} {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return gate
}

func acquireOperationCommandSlot(ctx context.Context, session *DroneSession) (func(), error) {
	session.pendingMu.Lock()
	if session.operationGate == nil {
		session.operationGate = makeOperationGate()
	}
	gate := session.operationGate
	session.pendingMu.Unlock()

	select {
	case <-gate:
		return func() { gate <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func beginOperationCommand(
	session *DroneSession,
	commandID string,
	fingerprint string,
	expected *agentv1.OperationContext,
) (*operationCommandState, bool, error) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	if session.pending == nil {
		session.pending = make(map[string]chan *agentv1.OperationContextCommandAck)
	}
	if session.operationCommands == nil {
		session.operationCommands = make(map[string]*operationCommandState)
	}
	pruneOperationCommandsLocked(session, time.Now())
	if existing := session.operationCommands[commandID]; existing != nil {
		if existing.fingerprint != fingerprint {
			return nil, false, status.Error(codes.AlreadyExists, "operation-context command ID was already used with a different payload")
		}
		if !existing.completed || !operationCommandRetryable(existing) {
			return existing, false, nil
		}
	}
	if len(session.operationCommands) >= maxOperationCommands {
		return nil, false, status.Error(codes.ResourceExhausted, "operation-context command retention is full")
	}

	state := &operationCommandState{
		fingerprint: fingerprint,
		expected:    cloneOperationContext(expected),
		done:        make(chan struct{}),
	}
	session.operationCommands[commandID] = state
	session.pending[commandID] = make(chan *agentv1.OperationContextCommandAck, 1)
	return state, true, nil
}

func pruneOperationCommandsLocked(session *DroneSession, now time.Time) {
	for commandID, state := range session.operationCommands {
		if state.completed && now.Sub(state.completedAt) >= operationCommandRetention {
			delete(session.operationCommands, commandID)
		}
	}
	for len(session.operationCommands) >= maxOperationCommands {
		var oldestID string
		var oldest time.Time
		for commandID, state := range session.operationCommands {
			if !state.completed {
				continue
			}
			if oldestID == "" || state.completedAt.Before(oldest) {
				oldestID = commandID
				oldest = state.completedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(session.operationCommands, oldestID)
	}
}

func operationCommandRetryable(state *operationCommandState) bool {
	return state.err != nil || state.ack.GetStatus() == agentv1.OperationContextCommandAck_STATUS_TEMPORARY_ERROR
}

func finishOperationCommand(
	session *DroneSession,
	commandID string,
	state *operationCommandState,
	ack *agentv1.OperationContextCommandAck,
	err error,
) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	if session.operationCommands[commandID] != state || state.completed {
		return
	}
	delete(session.pending, commandID)
	state.ack = cloneOperationAck(ack)
	state.err = err
	state.completed = true
	state.completedAt = time.Now()
	close(state.done)
}

func operationCommandResult(session *DroneSession, state *operationCommandState) (*agentv1.OperationContextCommandAck, error) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	return cloneOperationAck(state.ack), state.err
}

func cloneOperationContext(value *agentv1.OperationContext) *agentv1.OperationContext {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*agentv1.OperationContext)
}

func cloneOperationAck(value *agentv1.OperationContextCommandAck) *agentv1.OperationContextCommandAck {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*agentv1.OperationContextCommandAck)
}

func expectedContextAfterClear(session *DroneSession, flightID string) *agentv1.OperationContext {
	session.sessionMu.RLock()
	defer session.sessionMu.RUnlock()
	if session.FlightID == "" || session.FlightID == flightID {
		return nil
	}
	return &agentv1.OperationContext{
		FlightId:      session.FlightID,
		IntentId:      session.IntentID,
		IntentVersion: session.IntentVersion,
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
