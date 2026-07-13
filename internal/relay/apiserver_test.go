package relay

import (
	"context"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
)

func TestSetOperationContextDeliversAndWaitsForAgentAck(t *testing.T) {
	stream := &mockTelemetryStream{
		ctx:         context.Background(),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
	}
	relay := &Relay{grpcSessions: map[string]*DroneSession{
		"agent-1": {
			agentID: "agent-1", SessionID: "session-1", stream: stream,
			pending: make(map[string]chan *agentv1.OperationContextCommandAck),
		},
	}}
	request := &relayv1.SetOperationContextRequest{
		AgentId: "agent-1",
		Command: &agentv1.SetOperationContextCommand{
			CommandId: "command-1",
			Context:   &agentv1.OperationContext{FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 3},
		},
	}
	type result struct {
		response *relayv1.SetOperationContextResponse
		err      error
	}
	resultChannel := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		response, err := relay.SetOperationContext(ctx, request)
		resultChannel <- result{response: response, err: err}
	}()

	message := <-stream.sentAckChan
	if message.GetSetOperationContext() == nil || message.GetSetOperationContext().CommandId != "command-1" {
		t.Fatalf("unexpected relay stream message: %#v", message)
	}
	relay.handleOperationContextCommandAck("agent-1", &agentv1.OperationContextCommandAck{
		CommandId: "command-1",
		Status:    agentv1.OperationContextCommandAck_STATUS_APPLIED,
		ActiveContext: &agentv1.OperationContext{
			FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 3,
		},
	})
	got := <-resultChannel
	if got.err != nil {
		t.Fatalf("SetOperationContext() error = %v", got.err)
	}
	if got.response.GetResult().GetStatus() != agentv1.OperationContextCommandAck_STATUS_APPLIED {
		t.Fatalf("result = %#v", got.response.GetResult())
	}
	statusResponse, err := relay.GetDroneStatus(context.Background(), &relayv1.GetDroneStatusRequest{DroneId: "agent-1"})
	if err != nil {
		t.Fatalf("GetDroneStatus() error = %v", err)
	}
	if statusResponse.Drone.FlightId != "flight-1" || statusResponse.Drone.IntentVersion != 3 {
		t.Fatalf("drone context = %#v", statusResponse.Drone)
	}
}
