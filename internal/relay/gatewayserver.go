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
	"github.com/makinje/aero-arc-relay/internal/telemetrywriter"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Register handles the initial connection handshake from an agent.
func (r *Relay) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	slog.Info(
		"Received registration request",
		"agent_id", req.AgentId,
	)

	if strings.TrimSpace(req.AgentId) == "" {
		return nil, status.Error(codes.InvalidArgument, "agent ID is required")
	}
	sessionID, err := newSessionID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate session ID: %v", err)
	}

	// Store the session in the grpcSessions map.
	r.sessionsMu.Lock()
	r.grpcSessions[req.AgentId] = &DroneSession{
		agentID:       req.AgentId,
		SessionID:     sessionID,
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		Position:      nil,
		Attitude:      nil,
		VfrHud:        nil,
		SystemStatus:  nil,
		pending:       make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	r.sessionsMu.Unlock()

	return &agentv1.RegisterResponse{
		AgentId:     req.AgentId,
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

	agentID := meta.Get("aero-arc-agent-id")
	if len(agentID) == 0 {
		return status.Errorf(codes.InvalidArgument, "missing aero-arc-agent-id")
	}

	streamSession, streamBinding, err := r.updateStream(agentID[0], stream)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to update stream: %v", err)
	}
	slog.Info("Updated stream for agent", "agent_id", agentID[0])

	defer r.deleteStream(agentID[0], streamSession, streamBinding)

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
			slog.Warn("agent stream message has no supported payload", "agent_id", agentID[0])
			continue
		}

		ack := &agentv1.TelemetryAck{
			Seq:    frame.Seq,
			Status: agentv1.TelemetryAck_STATUS_OK,
		}
		r.sessionsMu.RLock()
		session := r.grpcSessions[agentID[0]]
		r.sessionsMu.RUnlock()
		if frame.AgentId != agentID[0] {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame agent ID does not match authenticated stream"
		} else if session != streamSession {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry stream session is no longer active"
		} else if frame.SessionId != streamSession.SessionID {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame session ID does not match active session"
		} else if strings.TrimSpace(frame.MsgName) == "" {
			ack.Status = agentv1.TelemetryAck_STATUS_PERMANENT_ERROR
			ack.Error = "telemetry frame message name is required"
		} else {
			// Process the frame (e.g., forward to outputs).
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

		if err := sendOnStream(streamBinding, &agentv1.RelayStreamMessage{
			Payload: &agentv1.RelayStreamMessage_TelemetryAck{TelemetryAck: ack},
		}); err != nil {
			slog.LogAttrs(
				ctx, slog.LevelWarn, "Failed to send ACK", slog.Uint64("seq", frame.Seq),
				slog.String("agent_id", frame.AgentId), slog.String("err", err.Error()),
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

func sendToSession(session *DroneSession, message *agentv1.RelayStreamMessage) error {
	for {
		session.sessionMu.RLock()
		binding := session.stream
		session.sessionMu.RUnlock()
		if binding == nil {
			return status.Error(codes.Unavailable, "agent stream is not connected")
		}

		binding.sendMu.Lock()
		session.sessionMu.RLock()
		isCurrent := session.stream == binding
		session.sessionMu.RUnlock()
		if !isCurrent {
			binding.sendMu.Unlock()
			continue
		}
		err := binding.stream.Send(message)
		binding.sendMu.Unlock()
		return err
	}
}

func sendOnStream(binding *telemetryStreamBinding, message *agentv1.RelayStreamMessage) error {
	binding.sendMu.Lock()
	defer binding.sendMu.Unlock()
	return binding.stream.Send(message)
}

func (session *DroneSession) handleOperationContextCommandAck(ack *agentv1.OperationContextCommandAck) {
	if session == nil {
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
	session.pendingMu.Lock()
	pending := session.pending[ack.CommandId]
	if pending != nil {
		delete(session.pending, ack.CommandId)
	}
	session.pendingMu.Unlock()
	if pending != nil {
		select {
		case pending <- ack:
		default:
		}
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
