package relay

import (
	"context"
	"io"
	"log/slog"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Register handles the initial connection handshake from an agent.
func (r *Relay) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	slog.Info("Received registration request",
		"agent_id", req.AgentId,
	)

	// Store the session in the grpcSessions map
	r.sessionsMu.Lock()
	r.grpcSessions[req.AgentId] = &DroneSession{
		agentID:       req.AgentId,
		SessionID:     req.AgentId,
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		Position:      nil,
		Attitude:      nil,
		VfrHud:        nil,
		SystemStatus:  nil,
	}
	r.sessionsMu.Unlock()

	// TODO: Generate a real session ID and store session state
	sessionID := "sess-" + req.AgentId // Placeholder

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

	if err := r.updateStream(agentID[0], stream); err != nil {
		return status.Errorf(codes.Internal, "failed to update stream: %v", err)
	}

	slog.Info("Updated stream for agent", "agent_id", agentID[0])

	// TODO: In a real implementation, you might want to start a goroutine to send ACKs back
	// independently of receiving frames, but for strict request-response style streaming
	// (or simple acking), a simple loop works.

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Receive a frame
		frame, err := stream.Recv()
		if err == io.EOF {
			return nil // Stream closed by client
		}
		if err != nil {
			slog.Error("Error receiving telemetry frame", "error", err)
			// TODO: prolly shouldn't close stream. Really should only close stream if agent is disconnected.
			return status.Errorf(codes.Unknown, "stream recv error: %v", err)
		}

		// Process the frame (e.g., forward to sinks)
		r.handleTelemetryFrame(frame)

		// Send ACK
		ack := &agentv1.TelemetryAck{
			Seq:    frame.Seq,
			Status: agentv1.TelemetryAck_STATUS_OK,
		}

		if err := stream.Send(ack); err != nil {
			slog.LogAttrs(ctx, slog.LevelWarn, "Failed to send ACK", slog.Uint64("seq", frame.Seq),
				slog.String("agent_id", frame.AgentId), slog.String("err", err.Error()),
			)

			return status.Errorf(codes.Unknown, "failed to send ack: %v", err)
		}
	}
}

func (r *Relay) handleTelemetryFrame(frame *agentv1.TelemetryFrame) {
	envelope := r.buildTelemetryFrameEnvelope(frame)
	r.handleTelemetryMessage(envelope)
}

func (r *Relay) buildTelemetryFrameEnvelope(frame *agentv1.TelemetryFrame) telemetry.TelemetryEnvelope {
	// TODO: This is going to have stringified values, so we need to handle that. Possibly
	fields := make(map[string]any, len(frame.Fields))
	for k, v := range frame.Fields {
		fields[k] = v
	}

	envelope := telemetry.TelemetryEnvelope{
		AgentID:         frame.AgentId,
		TimestampRelay:  time.Now().UTC(),
		TimestampDevice: 0,
		MsgID:           frame.MsgId,
		MsgName:         frame.MsgName,
		Fields:          fields,
	}

	raw, err := proto.Marshal(frame)
	if err != nil {
		slog.Error("Failed to marshal telemetry frame", "error", err)
		return envelope
	}

	envelope.Raw = raw

	return envelope
}
