package relay

import (
	"context"
	"io"
	"log/slog"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Register handles the initial connection handshake from an agent.
func (r *Relay) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	slog.Info("Received registration request",
		"agent_id", req.AgentId,
		"drone_id", req.DroneId,
		"hw_uid", req.HardwareUid,
	)

	// TODO: Generate a real session ID and store session state
	sessionID := "sess-" + req.AgentId // Placeholder

	return &agentv1.RegisterResponse{
		AgentId:     req.AgentId,
		DroneId:     req.DroneId,
		SessionId:   sessionID,
		MaxInflight: 100, // Example default
	}, nil
}

// TelemetryStream handles bidirectional telemetry streaming.
func (r *Relay) TelemetryStream(stream agentv1.AgentGateway_TelemetryStreamServer) error {
	ctx := stream.Context()

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
			return status.Errorf(codes.Unknown, "stream recv error: %v", err)
		}

		// Process the frame (e.g., forward to sinks)
		// r.handleAgentFrame(frame) // TODO: Implement this internal method

		// Send ACK
		ack := &agentv1.TelemetryAck{
			FrameId: frame.FrameId,
			Status:  agentv1.TelemetryAck_STATUS_OK,
		}

		if err := stream.Send(ack); err != nil {
			slog.Error("Failed to send ACK", "frame_id", frame.FrameId, "error", err)
			return status.Errorf(codes.Unknown, "failed to send ack: %v", err)
		}
	}
}
