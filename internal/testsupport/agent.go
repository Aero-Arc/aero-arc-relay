//go:build integration

package testsupport

import (
	"context"
	"fmt"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type FakeAgent struct {
	ID        string
	SessionID string
	stream    agentv1.AgentGateway_TelemetryStreamClient
}

// RegisterFakeAgent registers the supplied testsupport identity or handler.
//
// Parameters:
//   - ctx: controls cancellation and deadlines for the operation.
//   - conn: is the grpc.ClientConnInterface value supplied to RegisterFakeAgent.
//   - agentID: identifies the target agent.
//
// Returns:
//   - result: is the *FakeAgent value produced by RegisterFakeAgent.
//   - error: reports validation, dependency, cancellation, or persistence failures.
func RegisterFakeAgent(ctx context.Context, conn grpc.ClientConnInterface, agentID string) (*FakeAgent, error) {
	client := agentv1.NewAgentGatewayClient(conn)
	registration, err := client.Register(ctx, &agentv1.RegisterRequest{AgentId: agentID})
	if err != nil {
		return nil, fmt.Errorf("register fake agent %q: %w", agentID, err)
	}
	streamCtx := metadata.AppendToOutgoingContext(
		ctx,
		"aero-arc-agent-id", agentID,
		"aero-arc-session-id", registration.GetSessionId(),
	)
	stream, err := client.TelemetryStream(streamCtx)
	if err != nil {
		return nil, fmt.Errorf("open telemetry stream for fake agent %q: %w", agentID, err)
	}
	return &FakeAgent{
		ID:        registration.GetAgentId(),
		SessionID: registration.GetSessionId(),
		stream:    stream,
	}, nil
}

// Send sends the supplied data through FakeAgent.
//
// Parameters:
//   - frame: is the *agentv1.TelemetryFrame value supplied to Send.
//
// Returns:
//   - result: is the *agentv1.TelemetryAck value produced by Send.
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (a *FakeAgent) Send(frame *agentv1.TelemetryFrame) (*agentv1.TelemetryAck, error) {
	frame.AgentId = a.ID
	frame.SessionId = a.SessionID
	if err := a.stream.Send(&agentv1.AgentStreamMessage{
		Payload: &agentv1.AgentStreamMessage_TelemetryFrame{TelemetryFrame: frame},
	}); err != nil {
		return nil, fmt.Errorf("send telemetry sequence %d: %w", frame.Seq, err)
	}
	response, err := a.stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receive telemetry sequence %d ACK: %w", frame.Seq, err)
	}
	ack := response.GetTelemetryAck()
	if ack == nil {
		return nil, fmt.Errorf("telemetry sequence %d response has no ACK", frame.Seq)
	}
	return ack, nil
}

// Close releases resources owned by FakeAgent and completes any required shutdown work.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (a *FakeAgent) Close() error {
	if err := a.stream.CloseSend(); err != nil {
		return fmt.Errorf("close fake agent telemetry stream: %w", err)
	}
	return nil
}
