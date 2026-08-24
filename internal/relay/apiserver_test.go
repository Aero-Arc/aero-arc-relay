package relay

import (
	"context"
	"errors"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestSetOperationContextRequiresAuthorizedControlCaller(t *testing.T) {
	want := status.Error(codes.PermissionDenied, "denied")
	relay := &Relay{controlAuthorizer: func(context.Context) error { return want }}
	_, err := relay.SetOperationContext(context.Background(), &relayv1.SetOperationContextRequest{})
	if !errors.Is(err, want) {
		t.Fatalf("SetOperationContext() error = %v, want %v", err, want)
	}
}

func TestSetOperationContextDeliversToCurrentAgent(t *testing.T) {
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream:  &telemetryStreamBinding{stream: stream},
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	request := &relayv1.SetOperationContextRequest{
		AgentId: "agent-1",
		Command: &agentv1.SetOperationContextCommand{
			CommandId: "command-1",
			Context: &agentv1.OperationContext{
				FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 2,
			},
		},
	}
	type result struct {
		response *relayv1.SetOperationContextResponse
		err      error
	}
	resultChannel := make(chan result, 1)
	go func() {
		response, err := relay.SetOperationContext(context.Background(), request)
		resultChannel <- result{response: response, err: err}
	}()
	message := <-stream.sentAckChan
	if message.GetSetOperationContext().GetContext().GetFlightId() != "flight-1" {
		t.Fatalf("delivered command = %#v", message.GetSetOperationContext())
	}
	session.handleOperationContextCommandAck(&agentv1.OperationContextCommandAck{
		CommandId: "command-1",
		Status:    agentv1.OperationContextCommandAck_STATUS_APPLIED,
		ActiveContext: &agentv1.OperationContext{
			FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 2,
		},
	})
	got := <-resultChannel
	if got.err != nil {
		t.Fatalf("SetOperationContext() error = %v", got.err)
	}
	if got.response.GetResult().GetStatus() != agentv1.OperationContextCommandAck_STATUS_APPLIED {
		t.Fatalf("SetOperationContext() response = %#v", got.response)
	}

	// An exact retry returns the retained result without delivering twice.
	retry, err := relay.SetOperationContext(context.Background(), request)
	if err != nil || !proto.Equal(retry.GetResult(), got.response.GetResult()) {
		t.Fatalf("exact retry = %#v, %v", retry, err)
	}
	select {
	case duplicate := <-stream.sentAckChan:
		t.Fatalf("exact retry redelivered command: %#v", duplicate)
	default:
	}

	// A reused ID with a changed payload is rejected before delivery.
	conflict := proto.Clone(request).(*relayv1.SetOperationContextRequest)
	conflict.Command.Context.FlightId = "different-flight"
	if _, err := relay.SetOperationContext(context.Background(), conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting retry error = %v, want AlreadyExists", err)
	}
}

func TestSetOperationContextRejectsMismatchedAppliedContext(t *testing.T) {
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream:  &telemetryStreamBinding{stream: stream},
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	request := &relayv1.SetOperationContextRequest{
		AgentId: "agent-1",
		Command: &agentv1.SetOperationContextCommand{
			CommandId: "command-mismatch",
			Context: &agentv1.OperationContext{
				FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 2,
			},
		},
	}
	result := make(chan error, 1)
	go func() {
		_, err := relay.SetOperationContext(context.Background(), request)
		result <- err
	}()
	<-stream.sentAckChan
	session.handleOperationContextCommandAck(&agentv1.OperationContextCommandAck{
		CommandId: "command-mismatch",
		Status:    agentv1.OperationContextCommandAck_STATUS_APPLIED,
		ActiveContext: &agentv1.OperationContext{
			FlightId: "other-flight", IntentId: "other-intent", IntentVersion: 99,
		},
	})
	if err := <-result; status.Code(err) != codes.Internal {
		t.Fatalf("mismatched ACK error = %v, want Internal", err)
	}
	session.sessionMu.RLock()
	defer session.sessionMu.RUnlock()
	if session.FlightID != "" || session.IntentID != "" || session.IntentVersion != 0 {
		t.Fatalf("mismatched ACK changed context to (%q, %q, %d)", session.FlightID, session.IntentID, session.IntentVersion)
	}
}

func TestClearOperationContextAllowsAuthoritativeEmptyReconciliation(t *testing.T) {
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream: &telemetryStreamBinding{stream: stream}, pending: make(map[string]chan *agentv1.OperationContextCommandAck),
		operationContextUnreconciled: true,
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	result := make(chan error, 1)
	go func() {
		_, err := relay.ClearOperationContext(context.Background(), &relayv1.ClearOperationContextRequest{
			AgentId: "agent-1",
			Command: &agentv1.ClearOperationContextCommand{CommandId: "reconcile-empty"},
		})
		result <- err
	}()
	message := <-stream.sentAckChan
	if command := message.GetClearOperationContext(); command == nil || command.GetFlightId() != "" {
		t.Fatalf("empty reconciliation command = %#v", command)
	}
	session.handleOperationContextCommandAck(&agentv1.OperationContextCommandAck{
		CommandId: "reconcile-empty", Status: agentv1.OperationContextCommandAck_STATUS_APPLIED,
	})
	if err := <-result; err != nil {
		t.Fatalf("ClearOperationContext() error = %v", err)
	}
	if session.requiresOperationContextReconciliation() {
		t.Fatal("acknowledged empty context did not open telemetry admission")
	}
	retry, err := relay.ClearOperationContext(context.Background(), &relayv1.ClearOperationContextRequest{
		AgentId: "agent-1",
		Command: &agentv1.ClearOperationContextCommand{CommandId: "reconcile-empty"},
	})
	if err != nil || retry.GetResult().GetStatus() != agentv1.OperationContextCommandAck_STATUS_APPLIED {
		t.Fatalf("exact empty reconciliation retry = %#v, %v", retry, err)
	}
	select {
	case message := <-stream.sentAckChan:
		t.Fatalf("exact empty reconciliation retry redelivered: %#v", message)
	default:
	}
}

func TestClearOperationContextRejectsEmptyFlightAfterReconciliation(t *testing.T) {
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream: &telemetryStreamBinding{stream: stream}, pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	_, err := relay.ClearOperationContext(context.Background(), &relayv1.ClearOperationContextRequest{
		AgentId: "agent-1", Command: &agentv1.ClearOperationContextCommand{CommandId: "empty-after-ready"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ClearOperationContext() error = %v, want InvalidArgument", err)
	}
	select {
	case message := <-stream.sentAckChan:
		t.Fatalf("invalid empty clear reached Agent: %#v", message)
	default:
	}
}

func TestSetOperationContextWaitAbortsWhenSessionRetires(t *testing.T) {
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream:  &telemetryStreamBinding{stream: stream},
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	completed := make(chan error, 1)
	go func() {
		_, err := relay.SetOperationContext(context.Background(), &relayv1.SetOperationContextRequest{
			AgentId: "agent-1",
			Command: &agentv1.SetOperationContextCommand{
				CommandId: "set-retired",
				Context: &agentv1.OperationContext{
					FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 1,
				},
			},
		})
		completed <- err
	}()
	<-stream.sentAckChan
	session.ownershipMu.Lock()
	session.retired = true
	session.abortPendingCommands()
	session.ownershipMu.Unlock()
	select {
	case err := <-completed:
		if status.Code(err) != codes.Aborted {
			t.Fatalf("SetOperationContext() error = %v, want Aborted", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending operation-context command did not wake on retirement")
	}
}

func TestSetOperationContextPinsSessionOnlyThroughDelivery(t *testing.T) {
	stream := &mockTelemetryStream{
		ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
		sendStarted: make(chan struct{}, 1), sendBlock: make(chan struct{}),
	}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream: &telemetryStreamBinding{stream: stream}, pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	completed := make(chan error, 1)
	go func() {
		_, err := relay.SetOperationContext(context.Background(), &relayv1.SetOperationContextRequest{
			AgentId: "agent-1",
			Command: &agentv1.SetOperationContextCommand{
				CommandId: "set-pinned",
				Context: &agentv1.OperationContext{
					FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 1,
				},
			},
		})
		completed <- err
	}()
	<-stream.sendStarted

	leaseAcquired := make(chan struct{})
	go func() {
		session.ownershipMu.Lock()
		close(leaseAcquired)
		session.retired = true
		session.abortPendingCommands()
		session.ownershipMu.Unlock()
	}()
	select {
	case <-leaseAcquired:
		t.Fatal("session retired before operation-context delivery completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(stream.sendBlock)
	select {
	case <-leaseAcquired:
	case <-time.After(time.Second):
		t.Fatal("session lease remained held while awaiting operation-context ACK")
	}
	if err := <-completed; status.Code(err) != codes.Aborted {
		t.Fatalf("SetOperationContext() error = %v, want Aborted", err)
	}
}

func TestConcurrentExactOperationContextRetriesShareOneDelivery(t *testing.T) {
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 2)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream:  &telemetryStreamBinding{stream: stream},
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	request := &relayv1.SetOperationContextRequest{
		AgentId: "agent-1",
		Command: &agentv1.SetOperationContextCommand{
			CommandId: "command-shared",
			Context: &agentv1.OperationContext{
				FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 2,
			},
		},
	}
	results := make(chan error, 2)
	go func() {
		_, err := relay.SetOperationContext(context.Background(), request)
		results <- err
	}()
	<-stream.sentAckChan
	go func() {
		_, err := relay.SetOperationContext(context.Background(), request)
		results <- err
	}()
	select {
	case duplicate := <-stream.sentAckChan:
		t.Fatalf("concurrent retry redelivered command: %#v", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
	session.handleOperationContextCommandAck(&agentv1.OperationContextCommandAck{
		CommandId:     "command-shared",
		Status:        agentv1.OperationContextCommandAck_STATUS_APPLIED,
		ActiveContext: proto.Clone(request.Command.Context).(*agentv1.OperationContext),
	})
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("shared retry error = %v", err)
		}
	}
}

func TestOperationContextMutationsAreSerializedPerSession(t *testing.T) {
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 2)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream:        &telemetryStreamBinding{stream: stream},
		pending:       make(map[string]chan *agentv1.OperationContextCommandAck),
		FlightID:      "flight-a",
		IntentID:      "intent-a",
		IntentVersion: 1,
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	setRequest := &relayv1.SetOperationContextRequest{
		AgentId: "agent-1",
		Command: &agentv1.SetOperationContextCommand{
			CommandId: "set-b",
			Context: &agentv1.OperationContext{
				FlightId: "flight-b", IntentId: "intent-b", IntentVersion: 2,
			},
		},
	}
	clearRequest := &relayv1.ClearOperationContextRequest{
		AgentId: "agent-1",
		Command: &agentv1.ClearOperationContextCommand{CommandId: "clear-a", FlightId: "flight-a"},
	}

	setResult := make(chan error, 1)
	go func() {
		_, err := relay.SetOperationContext(context.Background(), setRequest)
		setResult <- err
	}()
	<-stream.sentAckChan

	clearResult := make(chan error, 1)
	go func() {
		_, err := relay.ClearOperationContext(context.Background(), clearRequest)
		clearResult <- err
	}()
	select {
	case message := <-stream.sentAckChan:
		t.Fatalf("clear overtook pending set: %#v", message)
	case <-time.After(20 * time.Millisecond):
	}

	session.handleOperationContextCommandAck(&agentv1.OperationContextCommandAck{
		CommandId:     "set-b",
		Status:        agentv1.OperationContextCommandAck_STATUS_APPLIED,
		ActiveContext: proto.Clone(setRequest.Command.Context).(*agentv1.OperationContext),
	})
	if err := <-setResult; err != nil {
		t.Fatalf("set result = %v", err)
	}
	clearMessage := <-stream.sentAckChan
	if clearMessage.GetClearOperationContext().GetCommandId() != "clear-a" {
		t.Fatalf("second delivery = %#v, want clear-a", clearMessage)
	}
	session.handleOperationContextCommandAck(&agentv1.OperationContextCommandAck{
		CommandId:     "clear-a",
		Status:        agentv1.OperationContextCommandAck_STATUS_APPLIED,
		ActiveContext: proto.Clone(setRequest.Command.Context).(*agentv1.OperationContext),
	})
	if err := <-clearResult; err != nil {
		t.Fatalf("clear result = %v", err)
	}
	if got := droneStatus(session); got.GetFlightId() != "flight-b" || got.GetIntentId() != "intent-b" || got.GetIntentVersion() != 2 {
		t.Fatalf("final context = %#v, want flight-b/intent-b/2", got)
	}
}

func TestOperationContextRequestValidation(t *testing.T) {
	relay := &Relay{controlAuthorizer: func(context.Context) error { return nil }}
	for name, call := range map[string]func() error{
		"set": func() error {
			_, err := relay.SetOperationContext(context.Background(), &relayv1.SetOperationContextRequest{AgentId: "agent-1"})
			return err
		},
		"clear": func() error {
			_, err := relay.ClearOperationContext(context.Background(), &relayv1.ClearOperationContextRequest{AgentId: "agent-1"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("operation-context validation error = %v, want InvalidArgument", err)
			}
		})
	}
}

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
		}, nil)
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
		if !proto.Equal(got.ack, wantAck) {
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
		}, nil)
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

func TestSetOperationContextHoldsSessionLeaseUntilBlockedSendCompletes(t *testing.T) {
	stream := &mockTelemetryStream{
		ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
		sendStarted: make(chan struct{}, 1), sendBlock: make(chan struct{}),
	}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream: &telemetryStreamBinding{stream: stream},
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	commandCtx, cancelCommand := context.WithCancel(context.Background())
	commandResult := make(chan error, 1)
	go func() {
		_, err := relay.SetOperationContext(commandCtx, &relayv1.SetOperationContextRequest{
			AgentId: "agent-1",
			Command: &agentv1.SetOperationContextCommand{
				CommandId: "context-replace",
				Context: &agentv1.OperationContext{
					FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 1,
				},
			},
		})
		commandResult <- err
	}()
	<-stream.sendStarted

	replaced := make(chan error, 1)
	go func() {
		_, err := relay.Register(context.Background(), &agentv1.RegisterRequest{AgentId: "agent-1"})
		replaced <- err
	}()
	select {
	case err := <-replaced:
		t.Fatalf("session replaced while old context send was blocked: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancelCommand()
	select {
	case err := <-commandResult:
		t.Fatalf("context caller returned before its started write completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case err := <-replaced:
		t.Fatalf("context cancellation released lease before old send completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(stream.sendBlock)
	<-stream.sentAckChan
	if err := <-commandResult; status.Code(err) != codes.Canceled && status.Code(err) != codes.Aborted {
		t.Fatalf("context caller error = %v, want Canceled or Aborted", err)
	}
	select {
	case err := <-replaced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("session replacement did not resume after old context write completed")
	}
}
