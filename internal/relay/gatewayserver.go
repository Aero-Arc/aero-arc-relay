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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/google/uuid"
	"github.com/makinje/aero-arc-relay/internal/telemetrywriter"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Register handles the initial connection handshake from an agent.
func (r *Relay) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	agentID := strings.TrimSpace(req.AgentId)
	slog.Info(
		"Received registration request",
		"agent_id", agentID,
	)

	if agentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agent ID is required")
	}
	if err := r.authenticateAgent(ctx, agentID); err != nil {
		return nil, err
	}
	sessionID, err := newSessionID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate session ID: %v", err)
	}
	newSession := &DroneSession{
		agentID:       agentID,
		SessionID:     sessionID,
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		Position:      nil,
		Attitude:      nil,
		VfrHud:        nil,
		SystemStatus:  nil,
		pending:       make(map[string]chan *agentv1.OperationContextCommandAck),
	}

	// Retire the previous session before publishing its replacement. Do not hold
	// the global map lock while waiting for one agent's admission lease.
	for {
		r.sessionsMu.RLock()
		previous := r.grpcSessions[agentID]
		r.sessionsMu.RUnlock()
		if previous != nil {
			previous.ownershipMu.Lock()
		}

		r.sessionsMu.Lock()
		if r.grpcSessions[agentID] != previous {
			r.sessionsMu.Unlock()
			if previous != nil {
				previous.ownershipMu.Unlock()
			}
			continue
		}
		if previous != nil {
			previous.retired = true
			if r.registryReporter != nil {
				r.registryReporter.StopAgent(agentID)
			}
		}
		r.grpcSessions[agentID] = newSession
		r.sessionsMu.Unlock()
		if previous != nil {
			previous.ownershipMu.Unlock()
		}
		break
	}

	return &agentv1.RegisterResponse{
		AgentId:     agentID,
		SessionId:   sessionID,
		MaxInflight: 100, // Example default
	}, nil
}

// TelemetryStream handles bidirectional telemetry streaming.
func (r *Relay) TelemetryStream(stream agentv1.AgentGateway_TelemetryStreamServer) error {
	ctx := stream.Context()

	meta, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.InvalidArgument, "missing metadata")
	}

	agentIDs := meta.Get("aero-arc-agent-id")
	if len(agentIDs) == 0 {
		return status.Errorf(codes.InvalidArgument, "missing aero-arc-agent-id")
	}
	agentID := strings.TrimSpace(agentIDs[0])
	if agentID == "" {
		return status.Errorf(codes.InvalidArgument, "empty aero-arc-agent-id")
	}
	if err := r.authenticateAgent(ctx, agentID); err != nil {
		return err
	}
	sessionIDs := meta.Get("aero-arc-session-id")
	if len(sessionIDs) != 1 || strings.TrimSpace(sessionIDs[0]) == "" {
		return status.Error(codes.Unauthenticated, "exactly one aero-arc-session-id is required")
	}
	sessionID := strings.TrimSpace(sessionIDs[0])

	streamSession, streamBinding, previousStream, err := r.updateStream(agentID, sessionID, stream)
	if err != nil {
		return status.Error(codes.Unauthenticated, "telemetry stream is not bound to an active session")
	}
	slog.Info("Updated stream for agent", "agent_id", agentID)
	if r.registryReporter != nil {
		if err := r.registerActiveAgent(ctx, agentID, streamSession, streamBinding, previousStream); err != nil {
			r.deleteStream(agentID, streamSession, streamBinding)
			if errors.Is(err, ErrSessionNotFound) {
				return status.Error(codes.Aborted, "telemetry session was replaced before publication")
			}
			return status.Errorf(codes.Unavailable, "register active agent with control plane: %v", err)
		}
	}

	defer r.deleteStream(agentID, streamSession, streamBinding)

	// TODO: In a real implementation, you might want to start a goroutine to send ACKs back
	// independently of receiving frames, but for strict request-response style streaming
	// (or simple acking), a simple loop works.

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		message, err := stream.Recv()
		if err == io.EOF {
			slog.Info("client closed stream")
			return nil
		}
		if err != nil {
			slog.Error("Error receiving telemetry frame. Cancelling stream.", "error", err)

			return err
		}

		if commandAck := message.GetOperationContextCommandAck(); commandAck != nil {
			streamSession.handleOperationContextCommandAck(commandAck)
			continue
		}
		frame := message.GetTelemetryFrame()
		if frame == nil {
			slog.Warn("agent stream message has no supported payload", "agent_id", agentID)
			continue
		}
		frameAgentID := strings.TrimSpace(frame.AgentId)
		frameWALID := strings.TrimSpace(frame.WalId)
		frameWALUUID, frameWALIDErr := uuid.Parse(frameWALID)

		ack := &agentv1.TelemetryAck{
			Seq:    frame.Seq,
			Status: agentv1.TelemetryAck_STATUS_OK,
		}
		// Acquire the per-session ownership lease before checking the map. Session
		// replacement and cleanup use the same ownership-to-map lock order.
		streamSession.ownershipMu.RLock()
		r.sessionsMu.RLock()
		session := r.grpcSessions[agentID]
		ownsSession := session == streamSession && !streamSession.retired
		r.sessionsMu.RUnlock()
		if frameAgentID != agentID {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame agent ID does not match authenticated stream"
		} else if !ownsSession {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry stream session is no longer active"
		} else if frame.SessionId != streamSession.SessionID {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame session ID does not match active session"
		} else if strings.TrimSpace(frame.MsgName) == "" {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame message name is required"
		} else if frame.SentAtUnixNs <= 0 {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame capture timestamp is required"
		} else if frameWALID == "" {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame WAL generation ID is required"
		} else if frameWALIDErr != nil || frameWALUUID == uuid.Nil {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame WAL generation ID is invalid"
		} else {
			// Process the frame (e.g., forward to outputs).
			frame.WalId = frameWALUUID.String()
			streamSession.sessionMu.Lock()
			streamSession.LastHeartbeat = time.Now().UTC()
			streamSession.sessionMu.Unlock()
			if err := r.handleTelemetryFrame(ctx, streamSession, frame); err != nil {
				if errors.Is(err, telemetrywriter.ErrNormalize) {
					ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
					ack.Error = "telemetry normalization failed: " + err.Error()
				} else {
					ack.Status = agentv1.TelemetryAck_STATUS_RETRY_WITH_BACKOFF
					ack.Error = "telemetry admission failed: " + err.Error()
				}
			}
		}
		streamSession.ownershipMu.RUnlock()

		if err := sendOnStream(streamBinding, &agentv1.RelayStreamMessage{
			Payload: &agentv1.RelayStreamMessage_TelemetryAck{TelemetryAck: ack},
		}); err != nil {
			slog.LogAttrs(
				ctx, slog.LevelWarn, "Failed to send ACK", slog.Uint64("seq", frame.Seq),
				slog.String("agent_id", agentID), slog.String("err", err.Error()),
			)

			return status.Errorf(codes.Unknown, "failed to send ack: %v", err)
		}
	}
}

func newSessionID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "sess-" + hex.EncodeToString(bytes[:]), nil
}

func sendToSession(ctx context.Context, session *DroneSession, message *agentv1.RelayStreamMessage) error {
	for {
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		session.sessionMu.RLock()
		binding := session.stream
		session.sessionMu.RUnlock()
		if binding == nil {
			return status.Error(codes.Unavailable, "agent stream is not connected")
		}

		if err := binding.sendMu.Lock(ctx); err != nil {
			return status.FromContextError(err).Err()
		}
		session.sessionMu.RLock()
		isCurrent := session.stream == binding
		session.sessionMu.RUnlock()
		if !isCurrent {
			binding.sendMu.Unlock()
			continue
		}
		if err := ctx.Err(); err != nil {
			binding.sendMu.Unlock()
			return status.FromContextError(err).Err()
		}
		sent := make(chan error, 1)
		go func() {
			err := binding.stream.Send(message)
			binding.sendMu.Unlock()
			sent <- err
		}()
		select {
		case err := <-sent:
			return err
		case <-ctx.Done():
			select {
			case err := <-sent:
				return err
			default:
				return status.FromContextError(ctx.Err()).Err()
			}
		}
	}
}

func sendOnStream(binding *telemetryStreamBinding, message *agentv1.RelayStreamMessage) error {
	if err := binding.sendMu.Lock(context.Background()); err != nil {
		return err
	}
	defer binding.sendMu.Unlock()
	return binding.stream.Send(message)
}

func (session *DroneSession) handleOperationContextCommandAck(ack *agentv1.OperationContextCommandAck) {
	if session == nil || ack == nil {
		return
	}
	// A command ACK is authoritative only while the relay has a matching
	// request pending on this exact session. Unsolicited and late ACKs must not
	// be allowed to change telemetry attribution.
	session.pendingMu.Lock()
	pending := session.pending[ack.CommandId]
	if pending != nil {
		delete(session.pending, ack.CommandId)
	}
	session.pendingMu.Unlock()
	if pending == nil {
		return
	}
	if ack.Status == agentv1.OperationContextCommandAck_STATUS_APPLIED ||
		ack.Status == agentv1.OperationContextCommandAck_STATUS_ALREADY_APPLIED {
		session.sessionMu.Lock()
		if active := ack.ActiveContext; active != nil {
			session.FlightID = active.FlightId
			session.IntentID = active.IntentId
			session.IntentVersion = active.IntentVersion
		} else {
			session.FlightID = ""
			session.IntentID = ""
			session.IntentVersion = 0
		}
		session.sessionMu.Unlock()
	}
	select {
	case pending <- ack:
	default:
	}
}

func (r *Relay) handleTelemetryFrame(ctx context.Context, session *DroneSession, frame *agentv1.TelemetryFrame) error {
	envelope := r.buildTelemetryFrameEnvelope(session, frame)
	return r.handleTelemetryMessage(ctx, envelope)
}

func (r *Relay) buildTelemetryFrameEnvelope(session *DroneSession, frame *agentv1.TelemetryFrame) telemetry.TelemetryEnvelope {
	// TODO: This is going to have stringified values, so we need to handle that. Possibly
	fields := make(map[string]any, len(frame.Fields))
	for k, v := range frame.Fields {
		fields[k] = v
	}

	var agentTime time.Time
	if frame.SentAtUnixNs > 0 {
		agentTime = time.Unix(0, frame.SentAtUnixNs).UTC()
	}
	session.sessionMu.RLock()
	agentID := session.agentID
	sessionID := session.SessionID
	flightID := session.FlightID
	intentID := session.IntentID
	intentVersion := session.IntentVersion
	session.sessionMu.RUnlock()

	envelope := telemetry.TelemetryEnvelope{
		AgentID:         agentID,
		Source:          agentID,
		SessionID:       sessionID,
		FlightID:        flightID,
		IntentID:        intentID,
		IntentVersion:   intentVersion,
		TimestampRelay:  time.Now().UTC(),
		TimestampAgent:  agentTime,
		TimestampDevice: frame.DeviceTimestampSec,
		Dialect:         frame.Dialect,
		MsgID:           frame.MsgId,
		MsgName:         frame.MsgName,
		WALID:           frame.WalId,
		WALSequence:     frame.Seq,
		Fields:          fields,
	}
	if r.config != nil {
		envelope.RelayID = r.config.Telemetry.RelayID
		if mapping, ok := r.config.Telemetry.AgentMappings[agentID]; ok {
			envelope.OperatorID = mapping.OperatorID
			envelope.AircraftID = mapping.AircraftID
		}
	}

	raw, err := proto.Marshal(frame)
	if err != nil {
		slog.Error("Failed to marshal telemetry frame", "error", err)
		return envelope
	}

	envelope.Raw = raw

	return envelope
}
