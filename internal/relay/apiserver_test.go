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

func TestOperationContextRPCsDisabledWithoutAuthenticatedControlPlane(t *testing.T) {
	relay := &Relay{}
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "set",
			call: func() error {
				_, err := relay.SetOperationContext(context.Background(), &relayv1.SetOperationContextRequest{})
				return err
			},
		},
		{
			name: "clear",
			call: func() error {
				_, err := relay.ClearOperationContext(context.Background(), &relayv1.ClearOperationContextRequest{})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if status.Code(err) != codes.Unimplemented {
				t.Fatalf("operation-context RPC error = %v, want Unimplemented", err)
			}
			if status.Convert(err).Message() != operationContextControlDisabled {
				t.Fatalf("operation-context RPC error message = %q", status.Convert(err).Message())
			}
		})
	}
}

func TestDeliverOperationCommandUsesCapturedSession(t *testing.T) {
	oldStream := &mockTelemetryStream{
		ctx:         context.Background(),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
	}
	replacementStream := &mockTelemetryStream{
		ctx:         context.Background(),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
	}
	oldSession := &DroneSession{
		agentID: "agent-1", SessionID: "old-session",
		stream:  &telemetryStreamBinding{stream: oldStream},
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	replacementSession := &DroneSession{
		agentID: "agent-1", SessionID: "replacement-session",
		stream:  &telemetryStreamBinding{stream: replacementStream},
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay := &Relay{grpcSessions: map[string]*DroneSession{"agent-1": replacementSession}}

	type result struct {
		ack *agentv1.OperationContextCommandAck
		err error
	}
	resultChannel := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		ack, err := deliverOperationCommandToSession(ctx, oldSession, "command-1", &agentv1.RelayStreamMessage{
			Payload: &agentv1.RelayStreamMessage_ClearOperationContext{
				ClearOperationContext: &agentv1.ClearOperationContextCommand{CommandId: "command-1", FlightId: "flight-1"},
			},
		})
		resultChannel <- result{ack: ack, err: err}
	}()

	select {
	case message := <-oldStream.sentAckChan:
		if message.GetClearOperationContext().GetCommandId() != "command-1" {
			t.Fatalf("old stream message = %#v", message)
		}
	case <-replacementStream.sentAckChan:
		t.Fatal("command for the captured session was sent to the replacement session")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for command on the captured session")
	}

	wantAck := &agentv1.OperationContextCommandAck{
		CommandId: "command-1",
		Status:    agentv1.OperationContextCommandAck_STATUS_APPLIED,
	}
	oldSession.handleOperationContextCommandAck(wantAck)
	select {
	case got := <-resultChannel:
		if got.err != nil {
			t.Fatalf("deliverOperationCommandToSession() error = %v", got.err)
		}
		if got.ack != wantAck {
			t.Fatalf("ACK = %#v, want %#v", got.ack, wantAck)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for captured session command result")
	}

	if relay.grpcSessions["agent-1"] != replacementSession {
		t.Fatal("captured-session command changed the registered replacement")
	}
}

func TestDeliverOperationCommandReturnsWhenSendOutlivesContext(t *testing.T) {
	stream := &mockTelemetryStream{
		ctx:         context.Background(),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
		sendStarted: make(chan struct{}, 1),
		sendBlock:   make(chan struct{}),
	}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream:  &telemetryStreamBinding{stream: stream},
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := deliverOperationCommandToSession(ctx, session, "command-1", &agentv1.RelayStreamMessage{
			Payload: &agentv1.RelayStreamMessage_ClearOperationContext{
				ClearOperationContext: &agentv1.ClearOperationContextCommand{CommandId: "command-1", FlightId: "flight-1"},
			},
		})
		result <- err
	}()

	select {
	case <-stream.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("operation command send did not start")
	}
	select {
	case err := <-result:
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("command error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("operation command remained blocked after its API context expired")
	}

	session.pendingMu.Lock()
	_, stillPending := session.pending["command-1"]
	session.pendingMu.Unlock()
	if stillPending {
		t.Fatal("expired command remained in the pending ACK map")
	}
	close(stream.sendBlock)
}
