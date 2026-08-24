package relay

import (
	"context"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSendAircraftCommandDeliversToConnectedAgentAndCorrelatesResult(t *testing.T) {
	stream := &mockTelemetryStream{
		ctx:         context.Background(),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
	}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream:          &telemetryStreamBinding{stream: stream},
		pendingAircraft: make(map[string]chan *agentv1.AircraftCommandResult),
	}
	relay := &Relay{grpcSessions: map[string]*DroneSession{"agent-1": session}}
	command := &agentv1.AircraftCommand{
		CommandId: "command-1", AircraftId: "aircraft-1",
		Type: agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_ARM,
	}

	type outcome struct {
		response *relayv1.SendAircraftCommandResponse
		err      error
	}
	completed := make(chan outcome, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		response, err := relay.SendAircraftCommand(ctx, &relayv1.SendAircraftCommandRequest{
			AgentId: "agent-1", Command: command,
		})
		completed <- outcome{response: response, err: err}
	}()

	select {
	case message := <-stream.sentAckChan:
		if got := message.GetAircraftCommand(); got.GetCommandId() != command.GetCommandId() || got.GetAircraftId() != command.GetAircraftId() {
			t.Fatalf("delivered command = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for command delivery")
	}

	wantResult := &agentv1.AircraftCommandResult{
		CommandId: "command-1", AircraftId: "aircraft-1",
		Status: agentv1.AircraftCommandResult_STATUS_ACCEPTED,
	}
	session.handleAircraftCommandResult(wantResult)
	select {
	case got := <-completed:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.response.GetResult() != wantResult {
			t.Fatalf("result = %+v", got.response.GetResult())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for correlated command result")
	}
}

func TestSendAircraftCommandRejectsDisconnectedAgent(t *testing.T) {
	relay := &Relay{grpcSessions: make(map[string]*DroneSession)}
	_, err := relay.SendAircraftCommand(context.Background(), &relayv1.SendAircraftCommandRequest{
		AgentId: "agent-1",
		Command: &agentv1.AircraftCommand{
			CommandId: "command-1", AircraftId: "aircraft-1",
			Type: agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_ARM,
		},
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("error = %v, want NotFound", err)
	}
}

func TestSendAircraftCommandDeadlineCleansPendingCorrelation(t *testing.T) {
	stream := &mockTelemetryStream{
		ctx:         context.Background(),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
	}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream:          &telemetryStreamBinding{stream: stream},
		pendingAircraft: make(map[string]chan *agentv1.AircraftCommandResult),
	}
	relay := &Relay{grpcSessions: map[string]*DroneSession{"agent-1": session}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := relay.SendAircraftCommand(ctx, &relayv1.SendAircraftCommandRequest{
		AgentId: "agent-1",
		Command: &agentv1.AircraftCommand{
			CommandId: "command-1", AircraftId: "aircraft-1",
			Type: agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_DISARM,
		},
	})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	session.pendingMu.Lock()
	_, stillPending := session.pendingAircraft["command-1"]
	session.pendingMu.Unlock()
	if stillPending {
		t.Fatal("expired command remained pending")
	}
}
