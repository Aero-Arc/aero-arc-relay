package relay

import (
	"context"
	"testing"
	"time"

	"github.com/aero-arc/aero-arc-protos/commanddigest"
	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/makinje/aero-arc-relay/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDurableCommandRequiresCapabilityAndAgentEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream := &mockTelemetryStream{ctx: ctx, sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	binding := &telemetryStreamBinding{stream: stream}
	session := &DroneSession{agentID: "agent", SessionID: "session", stream: binding, operationGate: makeOperationGate()}
	r := &Relay{controlAuthorizer: func(context.Context) error { return nil }, grpcSessions: map[string]*DroneSession{"agent": session}, config: &config.Config{Telemetry: config.TelemetryConfig{AgentMappings: map[string]config.AgentMapping{"agent": {OperatorID: "operator", AircraftID: "aircraft"}}}}}
	now := time.Now()
	c := &agentv1.DurableCommand{CommandId: "command", OperatorId: "operator", AircraftId: "aircraft", AgentId: "agent", Context: &agentv1.OperationContext{AircraftId: "aircraft", FlightId: "flight", IntentId: "intent", IntentVersion: 1}, Definition: "ARM", DefinitionVersion: 1, Capability: "mavlink_command_v1", IssuedAtUnixMs: now.UnixMilli(), ExpiresAtUnixMs: now.Add(time.Second).UnixMilli(), RecoveryPolicy: "no_repeat_effect_v1", Execution: &agentv1.DurableCommand_Mavlink{Mavlink: &agentv1.MavlinkExecution{Command: 400, Parameters: []float32{1, 0, 0, 0, 0, 0, 0}, Observation: "armed"}}}
	var err error
	c.CommandDigest, err = commanddigest.Digest(c)
	if err != nil {
		t.Fatal(err)
	}
	req := &relayv1.ExchangeCommandRequest{AgentId: "agent", Command: c, AttemptId: "command/attempt-1"}
	if _, err = r.ExchangeCommand(ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unsupported capability accepted: %v", err)
	}
	session.executionCapabilities = []string{"mavlink_command_v1"}
	done := make(chan *relayv1.ExchangeCommandResponse, 1)
	fail := make(chan error, 1)
	go func() {
		response, err := r.ExchangeCommand(ctx, req)
		if err != nil {
			fail <- err
		} else {
			done <- response
		}
	}()
	select {
	case message := <-stream.sentAckChan:
		if message.GetDurableCommand().GetCommandDigest() != c.CommandDigest {
			t.Fatal("payload changed")
		}
	case <-ctx.Done():
		t.Fatal("command not dispatched")
	}
	select {
	case <-done:
		t.Fatal("Relay delivery was mistaken for Agent evidence")
	default:
	}
	session.handleC2Evidence(&telemetryStreamBinding{}, &agentv1.CommandEvidence{CommandId: c.CommandId, CommandDigest: c.CommandDigest})
	select {
	case <-done:
		t.Fatal("stale stream supplied command evidence")
	default:
	}
	session.handleC2Evidence(binding, &agentv1.CommandEvidence{CommandId: c.CommandId, CommandDigest: c.CommandDigest, Events: []*agentv1.CommandEvent{{EventId: "command/acknowledged", Stage: "acknowledged", OccurredAtUnixMs: now.UnixMilli(), EvidenceSource: "agent_journal"}}})
	select {
	case response := <-done:
		if len(response.Evidence.Events) != 3 {
			t.Fatalf("missing separate receipt evidence: %v", response)
		}
	case err := <-fail:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("correlated evidence not returned")
	}
}
