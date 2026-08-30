/*
Copyright 2026 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at http://mozilla.org/MPL/2.0/.
*/

package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"strings"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/aero-arc/aero-arc-protos/missiondigest"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	missionPlanSchemaVersion = 1
	maxMissionItems          = 200
	maxMissionDeploymentWait = 2 * time.Minute
	maxMissionCommandWindow  = 5 * time.Minute
	maxMissionClockSkew      = 30 * time.Second
)

func isSupportedMissionFrame(value uint32) bool {
	return value == uint32(common.MAV_FRAME_GLOBAL)
}

func isSupportedMissionCommand(value uint32) bool {
	switch value {
	case uint32(common.MAV_CMD_NAV_WAYPOINT),
		uint32(common.MAV_CMD_NAV_LAND),
		uint32(common.MAV_CMD_NAV_TAKEOFF):
		return true
	default:
		return false
	}
}

func isPositiveZero(value float64) bool {
	return math.Float64bits(value) == 0
}

// DeployMission authenticates a control-plane caller and delivers one bounded,
// immutable mission to the exact Agent session active at admission. It refuses
// delivery unless the configured Agent mapping and reconciled operation context
// exactly match the mission binding. Concurrent exact attempts coalesce,
// terminal results replay, and retryable Agent outcomes may start one new
// delivery on the original stream binding. A command ID may never identify a
// different payload. Relay forwards an expired command because its in-memory
// retention may have been lost during restart; only an Agent with a matching
// durable uncertain record may reconcile it by readback. The Agent must reject
// a first expired effect and must not replace an expired mission after a
// mismatching recovery readback.
//
// Parameters:
//   - ctx: authenticates the control caller, bounds admission and result waiting,
//     and may detach without canceling an already admitted stream delivery.
//   - req: identifies the connected Agent and carries the immutable, bound
//     command. Relay clones the command before asynchronous delivery.
//
// Returns:
//   - *pb.DeployMissionResponse: the correlated Agent result, including terminal,
//     retryable, and outcome-unknown application statuses.
//   - error: reports authorization or canonical-plan validation failure, mapping
//     or operation-context mismatch, missing/replaced session, command-ID payload
//     conflict, retention exhaustion, stream delivery failure, malformed Agent
//     evidence, or caller/Relay wait timeout. An RPC error after admission leaves
//     the effect uncertain and requires an exact retry with the same command ID
//     and payload.
func (s *Relay) DeployMission(ctx context.Context, req *pb.DeployMissionRequest) (*pb.DeployMissionResponse, error) {
	if err := s.authorizeControlMutation(ctx); err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(req.GetAgentId())
	command := req.GetCommand()
	if agentID == "" || command == nil {
		return nil, status.Error(codes.InvalidArgument, "agent_id and command are required")
	}
	if err := validateDeployMissionCommand(command); err != nil {
		return nil, err
	}
	if s.config == nil {
		return nil, status.Error(codes.FailedPrecondition, "relay Agent mapping is not configured")
	}
	mapping, mapped := s.config.Telemetry.AgentMappings[agentID]
	if !mapped || strings.TrimSpace(mapping.OperatorID) == "" || strings.TrimSpace(mapping.AircraftID) == "" {
		return nil, status.Error(codes.FailedPrecondition, "relay Agent mapping is not configured")
	}
	binding := command.GetBinding()
	if mapping.OperatorID != binding.GetOperatorId() || mapping.AircraftID != binding.GetAircraftId() {
		return nil, status.Error(codes.FailedPrecondition, "mission binding does not match configured Agent mapping")
	}

	s.sessionsMu.RLock()
	session := s.grpcSessions[agentID]
	s.sessionsMu.RUnlock()
	if session == nil {
		return nil, status.Error(codes.NotFound, "agent is not connected")
	}
	release, err := acquireOperationCommandSlot(ctx, session)
	if err != nil {
		return nil, err
	}
	defer release()

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
	unreconciled := session.operationContextUnreconciled
	contextMatches := session.AircraftID != "" && session.AircraftID == binding.GetAircraftId() &&
		session.FlightID == binding.GetFlightId() &&
		session.IntentID == binding.GetIntentId() && session.IntentVersion == binding.GetIntentVersion()
	session.sessionMu.RUnlock()
	if !connected {
		session.ownershipMu.RUnlock()
		return nil, status.Error(codes.NotFound, "agent stream is not connected")
	}
	if unreconciled {
		session.ownershipMu.RUnlock()
		return nil, status.Error(codes.FailedPrecondition, "operation context must be reconciled before mission deployment")
	}
	if !contextMatches {
		session.ownershipMu.RUnlock()
		return nil, status.Error(codes.FailedPrecondition, "mission binding does not match the reconciled Agent operation context")
	}

	startedAt := time.Now()
	state, owner, err := beginMissionDeployment(session, command)
	if err != nil {
		session.ownershipMu.RUnlock()
		relayMissionDeploymentsTotal.WithLabelValues("delivery_failed").Inc()
		return nil, err
	}
	if !owner {
		session.ownershipMu.RUnlock()
	} else {
		slog.LogAttrs(ctx, slog.LevelInfo, "mission_deployment_started",
			slog.String("command_id", command.GetCommandId()),
			slog.String("deployment_id", binding.GetDeploymentId()),
			slog.String("mission_id", binding.GetMissionId()),
			slog.String("aircraft_id", binding.GetAircraftId()),
			slog.String("flight_id", binding.GetFlightId()),
			slog.String("intent_id", binding.GetIntentId()),
			slog.Uint64("intent_version", uint64(binding.GetIntentVersion())),
			slog.String("agent_id", agentID),
			slog.String("session_id", sessionID),
		)
	}

	waitCtx, cancel := context.WithTimeout(ctx, maxMissionDeploymentWait)
	defer cancel()
	select {
	case <-state.done:
		result, err := takeMissionDeploymentResult(session, state)
		if err != nil {
			relayMissionDeploymentsTotal.WithLabelValues("delivery_failed").Inc()
			return nil, err
		}
		if result == nil {
			relayMissionDeploymentsTotal.WithLabelValues("delivery_failed").Inc()
			return nil, status.Error(codes.Internal, "agent returned an empty mission deployment result")
		}
		duration := time.Since(startedAt)
		relayMissionDeploymentsTotal.WithLabelValues(result.GetStatus().String()).Inc()
		relayMissionDeploymentDuration.Observe(duration.Seconds())
		slog.LogAttrs(ctx, slog.LevelInfo, "mission_deployment_completed",
			slog.String("command_id", command.GetCommandId()),
			slog.String("deployment_id", binding.GetDeploymentId()),
			slog.String("agent_id", agentID),
			slog.String("session_id", sessionID),
			slog.String("result", result.GetStatus().String()),
			slog.Duration("duration", duration),
		)
		return &pb.DeployMissionResponse{Result: result}, nil
	case <-waitCtx.Done():
		requestErr := status.FromContextError(waitCtx.Err()).Err()
		cancelMissionDeploymentWaiter(session, command.GetCommandId(), state)
		relayMissionDeploymentsTotal.WithLabelValues("wait_ended").Inc()
		return nil, requestErr
	}
}

func validateDeployMissionCommand(command *agentv1.DeployMissionCommand) error {
	if command == nil || command.GetBinding() == nil || command.GetPlan() == nil {
		return status.Error(codes.InvalidArgument, "command, binding, and plan are required")
	}
	if len(command.ProtoReflect().GetUnknown()) != 0 || len(command.GetBinding().ProtoReflect().GetUnknown()) != 0 || len(command.GetPlan().ProtoReflect().GetUnknown()) != 0 {
		return status.Error(codes.InvalidArgument, "mission command contains unknown fields")
	}
	if strings.TrimSpace(command.GetCommandId()) == "" || command.GetCommandId() != strings.TrimSpace(command.GetCommandId()) {
		return status.Error(codes.InvalidArgument, "command_id must be non-empty and canonical")
	}
	b := command.GetBinding()
	values := []string{b.GetMissionId(), b.GetMissionDigest(), b.GetDeploymentId(), b.GetOperatorId(), b.GetAircraftId(), b.GetFlightId(), b.GetIntentId()}
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) {
			return status.Error(codes.InvalidArgument, "all mission binding identifiers must be non-empty and canonical")
		}
	}
	if b.GetMissionVersion() == 0 || b.GetIntentVersion() == 0 {
		return status.Error(codes.InvalidArgument, "mission_version and intent_version must be positive")
	}
	if len(b.GetMissionDigest()) != sha256.Size*2 || strings.ToLower(b.GetMissionDigest()) != b.GetMissionDigest() {
		return status.Error(codes.InvalidArgument, "mission_digest must be lowercase hexadecimal SHA-256")
	}
	if _, err := hex.DecodeString(b.GetMissionDigest()); err != nil {
		return status.Error(codes.InvalidArgument, "mission_digest must be lowercase hexadecimal SHA-256")
	}
	if command.GetIssuedAtUnixMs() <= 0 || command.GetExpiresAtUnixMs() <= command.GetIssuedAtUnixMs() {
		return status.Error(codes.InvalidArgument, "mission command timestamps are invalid")
	}
	plan := command.GetPlan()
	if plan.GetSchemaVersion() != missionPlanSchemaVersion {
		return status.Errorf(codes.InvalidArgument, "unsupported mission schema version %d", plan.GetSchemaVersion())
	}
	if len(plan.GetItems()) == 0 || len(plan.GetItems()) > maxMissionItems {
		return status.Errorf(codes.InvalidArgument, "mission plan must contain 1 to %d items", maxMissionItems)
	}
	for i, item := range plan.GetItems() {
		if item == nil || len(item.ProtoReflect().GetUnknown()) != 0 {
			return status.Errorf(codes.InvalidArgument, "mission item %d is nil or contains unknown fields", i)
		}
		if item.GetSequence() != uint32(i) {
			return status.Errorf(codes.InvalidArgument, "mission item %d has non-contiguous sequence", i)
		}
		if item.GetCurrent() {
			return status.Errorf(codes.InvalidArgument, "mission item %d sets reserved current flag", i)
		}
		if !item.GetAutocontinue() {
			return status.Errorf(codes.InvalidArgument, "mission item %d must enable autocontinue", i)
		}
		if !isPositiveZero(item.GetParam1()) || !isPositiveZero(item.GetParam2()) || !isPositiveZero(item.GetParam3()) {
			return status.Errorf(codes.InvalidArgument, "mission item %d params 1 through 3 must be positive zero", i)
		}
		if !isSupportedMissionFrame(item.GetFrame()) {
			return status.Errorf(codes.InvalidArgument, "mission item %d uses unsupported frame %d", i, item.GetFrame())
		}
		if !isSupportedMissionCommand(item.GetCommand()) {
			return status.Errorf(codes.InvalidArgument, "mission item %d uses unsupported command %d", i, item.GetCommand())
		}
		switch item.GetCommand() {
		case uint32(common.MAV_CMD_NAV_WAYPOINT), uint32(common.MAV_CMD_NAV_TAKEOFF):
			if !isPositiveZero(item.GetParam4()) {
				return status.Errorf(codes.InvalidArgument, "mission item %d param4 must be positive zero for command %d", i, item.GetCommand())
			}
		case uint32(common.MAV_CMD_NAV_LAND):
			if item.GetParam4() != 1 {
				return status.Errorf(codes.InvalidArgument, "mission item %d param4 must be +1 for NAV_LAND", i)
			}
		}
		if item.GetLatitudeE7() < -900000000 || item.GetLatitudeE7() > 900000000 || item.GetLongitudeE7() < -1800000000 || item.GetLongitudeE7() > 1800000000 {
			return status.Errorf(codes.InvalidArgument, "mission item %d has invalid coordinates", i)
		}
		values := []float64{item.GetParam1(), item.GetParam2(), item.GetParam3(), item.GetParam4(), float64(item.GetAltitudeM())}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return status.Errorf(codes.InvalidArgument, "mission item %d contains a non-finite value", i)
			}
		}
		altitudeCM := math.Round(float64(item.GetAltitudeM()) * 100)
		if altitudeCM < -8388608 || altitudeCM > 8388607 {
			return status.Errorf(codes.InvalidArgument, "mission item %d altitude must round-trip through ArduPilot signed-centimeter storage", i)
		}
		altitudeReadback := float32(int32(altitudeCM)) / 100
		if math.Float32bits(altitudeReadback) != math.Float32bits(item.GetAltitudeM()) {
			return status.Errorf(codes.InvalidArgument, "mission item %d altitude must round-trip through ArduPilot signed-centimeter storage", i)
		}
	}
	digest, err := missionPlanDigest(plan)
	if err != nil {
		return status.Errorf(codes.Internal, "canonicalize mission plan: %v", err)
	}
	if digest != b.GetMissionDigest() {
		return status.Error(codes.InvalidArgument, "mission_digest does not match the canonical plan")
	}
	return nil
}

func beginMissionDeployment(session *DroneSession, command *agentv1.DeployMissionCommand) (*missionDeploymentState, bool, error) {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(command)
	if err != nil {
		return nil, false, status.Errorf(codes.Internal, "fingerprint mission command: %v", err)
	}
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])

	session.controlStreamMu.RLock()
	session.pendingMu.Lock()
	if session.missionDeployments == nil {
		session.missionDeployments = make(map[string]*missionDeploymentState)
	}
	expireMissionDeploymentsLocked(session, time.Now())
	session.sessionMu.RLock()
	currentStream := session.stream
	session.sessionMu.RUnlock()
	if existing := session.missionDeployments[command.GetCommandId()]; existing != nil {
		if existing.fingerprint != fingerprint {
			session.pendingMu.Unlock()
			session.controlStreamMu.RUnlock()
			return nil, false, status.Error(codes.AlreadyExists, "mission command ID was already used with a different payload")
		}
		if !existing.completed || !retryableMissionDeployment(existing) || existing.deliveryStream != currentStream {
			existing.waiters++
			session.pendingMu.Unlock()
			session.controlStreamMu.RUnlock()
			return existing, false, nil
		}
	}
	now := time.Now()
	if command.GetIssuedAtUnixMs() > now.Add(maxMissionClockSkew).UnixMilli() {
		session.pendingMu.Unlock()
		session.controlStreamMu.RUnlock()
		return nil, false, status.Error(codes.InvalidArgument, "mission command issue time is too far in the future")
	}
	if command.GetExpiresAtUnixMs()-command.GetIssuedAtUnixMs() > maxMissionCommandWindow.Milliseconds() {
		session.pendingMu.Unlock()
		session.controlStreamMu.RUnlock()
		return nil, false, status.Error(codes.InvalidArgument, "mission command validity window is too long")
	}
	_, replacing := session.missionDeployments[command.GetCommandId()]
	if !replacing {
		makeMissionDeploymentRoomLocked(session)
	}
	if !replacing && len(session.missionDeployments) >= maxOperationCommands {
		session.pendingMu.Unlock()
		session.controlStreamMu.RUnlock()
		return nil, false, status.Error(codes.ResourceExhausted, "mission deployment retention is full")
	}
	deliveryCtx, deliveryCancel := context.WithCancel(context.Background())
	cloned := proto.Clone(command).(*agentv1.DeployMissionCommand)
	state := &missionDeploymentState{
		fingerprint: fingerprint, command: cloned, deliveryStream: currentStream, done: make(chan struct{}),
		deliveryCancel: deliveryCancel, waiters: 1,
	}
	session.missionDeployments[command.GetCommandId()] = state
	session.pendingMu.Unlock()
	go func() {
		defer session.ownershipMu.RUnlock()
		defer session.controlStreamMu.RUnlock()
		attempted, sendErr := sendToSessionWithWritePolicy(deliveryCtx, session, &agentv1.RelayStreamMessage{
			Payload: &agentv1.RelayStreamMessage_DeployMission{DeployMission: cloned},
		}, true)
		session.pendingMu.Lock()
		if session.missionDeployments[cloned.GetCommandId()] == state && !state.completed {
			// Any invoked Send is outcome-uncertain: the peer can apply the
			// message even when Relay observes a transport error.
			state.delivered = attempted
		}
		session.pendingMu.Unlock()
		if sendErr != nil {
			if attempted {
				finishMissionDeployment(session, cloned.GetCommandId(), state, uncertainMissionDeploymentResult(cloned, time.Now(), "mission stream write failed after delivery began"), nil)
			} else {
				finishMissionDeployment(session, cloned.GetCommandId(), state, nil, sendErr)
			}
		}
	}()
	return state, true, nil
}

func retryableMissionDeployment(state *missionDeploymentState) bool {
	if state == nil || !state.completed || state.err != nil || state.result == nil {
		return false
	}
	switch state.result.GetStatus() {
	case agentv1.MissionDeploymentResult_STATUS_TEMPORARY_ERROR,
		agentv1.MissionDeploymentResult_STATUS_OUTCOME_UNKNOWN:
		return true
	default:
		return false
	}
}

func validateMissionDeploymentResult(command *agentv1.DeployMissionCommand, result *agentv1.MissionDeploymentResult) error {
	if command == nil || result == nil || result.GetCommandId() != command.GetCommandId() || !proto.Equal(result.GetBinding(), command.GetBinding()) {
		return status.Error(codes.Internal, "agent returned a mismatched mission deployment result")
	}
	if len(result.ProtoReflect().GetUnknown()) != 0 || result.GetBinding() == nil || len(result.GetBinding().ProtoReflect().GetUnknown()) != 0 {
		return status.Error(codes.Internal, "agent returned an invalid mission deployment result")
	}
	switch result.GetStatus() {
	case agentv1.MissionDeploymentResult_STATUS_APPLIED,
		agentv1.MissionDeploymentResult_STATUS_ALREADY_APPLIED,
		agentv1.MissionDeploymentResult_STATUS_REJECTED,
		agentv1.MissionDeploymentResult_STATUS_TEMPORARY_ERROR,
		agentv1.MissionDeploymentResult_STATUS_OUTCOME_UNKNOWN,
		agentv1.MissionDeploymentResult_STATUS_BINDING_MISMATCH,
		agentv1.MissionDeploymentResult_STATUS_ONBOARD_MISSION_MISMATCH:
	default:
		return status.Error(codes.Internal, "agent returned an invalid mission deployment status")
	}
	if result.GetCompletedAtUnixMs() <= 0 {
		return status.Error(codes.Internal, "agent returned a mission result without completion time")
	}
	if result.GetStatus() == agentv1.MissionDeploymentResult_STATUS_APPLIED || result.GetStatus() == agentv1.MissionDeploymentResult_STATUS_ALREADY_APPLIED {
		if result.GetOnboardMissionDigest() != command.GetBinding().GetMissionDigest() || int(result.GetUploadedItemCount()) != len(command.GetPlan().GetItems()) {
			return status.Error(codes.Internal, "agent success result does not prove the expected onboard mission")
		}
	}
	return nil
}

func finishMissionDeployment(session *DroneSession, commandID string, state *missionDeploymentState, result *agentv1.MissionDeploymentResult, err error) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	if session.missionDeployments[commandID] != state || state.completed {
		return
	}
	if result != nil {
		state.result = proto.Clone(result).(*agentv1.MissionDeploymentResult)
	}
	state.err = err
	state.completed = true
	state.completedAt = time.Now()
	state.deliveryCancel()
	close(state.done)
}

func completeMissionDeploymentForLostSessionLocked(state *missionDeploymentState, now time.Time, reason string) {
	if state == nil || state.completed {
		return
	}
	if state.delivered {
		state.result = uncertainMissionDeploymentResult(state.command, now, reason)
	} else {
		state.err = status.Error(codes.Aborted, reason+" before confirmed delivery")
	}
	state.completed = true
	state.completedAt = now
	if state.deliveryCancel != nil {
		state.deliveryCancel()
	}
	close(state.done)
}

func uncertainMissionDeploymentResult(command *agentv1.DeployMissionCommand, now time.Time, reason string) *agentv1.MissionDeploymentResult {
	return &agentv1.MissionDeploymentResult{
		CommandId: command.GetCommandId(), Binding: proto.Clone(command.GetBinding()).(*agentv1.MissionBinding),
		Status: agentv1.MissionDeploymentResult_STATUS_OUTCOME_UNKNOWN, Message: reason, CompletedAtUnixMs: now.UnixMilli(),
	}
}

func takeMissionDeploymentResult(session *DroneSession, state *missionDeploymentState) (*agentv1.MissionDeploymentResult, error) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	if state.waiters > 0 {
		state.waiters--
	}
	if state.result == nil {
		return nil, state.err
	}
	return proto.Clone(state.result).(*agentv1.MissionDeploymentResult), state.err
}

func cancelMissionDeploymentWaiter(session *DroneSession, commandID string, state *missionDeploymentState) {
	session.pendingMu.Lock()
	defer session.pendingMu.Unlock()
	if session.missionDeployments[commandID] != state {
		return
	}
	if state.waiters > 0 {
		state.waiters--
	}
	if state.waiters != 0 || state.completed {
		return
	}
	// Admission and stream delivery run independently of an individual waiter.
	// Conservatively retain uncertainty even if cancellation wins just before
	// the delivery task records Send as started; this prevents a retry from
	// duplicating a command that may already be crossing the stream.
	state.result = uncertainMissionDeploymentResult(state.command, time.Now(), "mission result wait ended after command admission")
	state.completed = true
	state.completedAt = time.Now()
	state.deliveryCancel()
	close(state.done)
}

func expireMissionDeploymentsLocked(session *DroneSession, now time.Time) {
	for commandID, state := range session.missionDeployments {
		if state.completed && now.Sub(state.completedAt) >= operationCommandRetention {
			delete(session.missionDeployments, commandID)
		}
	}
}

func makeMissionDeploymentRoomLocked(session *DroneSession) {
	for len(session.missionDeployments) >= maxOperationCommands {
		var oldestID string
		var oldest time.Time
		for commandID, state := range session.missionDeployments {
			if !state.completed {
				continue
			}
			if oldestID == "" || state.completedAt.Before(oldest) {
				oldestID, oldest = commandID, state.completedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(session.missionDeployments, oldestID)
	}
}

func missionPlanDigest(plan *agentv1.MissionPlan) (string, error) {
	return missiondigest.Digest(plan)
}
