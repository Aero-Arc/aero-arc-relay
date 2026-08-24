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

func TestSendAircraftCommandDeliversToConnectedAgentAndCorrelatesResult(t *testing.T) {
	stream := &mockTelemetryStream{
		ctx:         context.Background(),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
	}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream: &telemetryStreamBinding{stream: stream},
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
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
		if !proto.Equal(got.response.GetResult(), wantResult) {
			t.Fatalf("result = %+v", got.response.GetResult())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for correlated command result")
	}

	retry, err := relay.SendAircraftCommand(context.Background(), &relayv1.SendAircraftCommandRequest{AgentId: "agent-1", Command: command})
	if err != nil || !proto.Equal(retry.GetResult(), wantResult) {
		t.Fatalf("retained exact retry = %#v, %v", retry, err)
	}
	select {
	case duplicate := <-stream.sentAckChan:
		t.Fatalf("exact retry redelivered aircraft command: %#v", duplicate)
	default:
	}
	conflict := proto.Clone(command).(*agentv1.AircraftCommand)
	conflict.Type = agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_DISARM
	if _, err := relay.SendAircraftCommand(context.Background(), &relayv1.SendAircraftCommandRequest{AgentId: "agent-1", Command: conflict}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting command-ID reuse error = %v, want AlreadyExists", err)
	}
}

func TestSendAircraftCommandRejectsDisconnectedAgent(t *testing.T) {
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      make(map[string]*DroneSession),
	}
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
		stream: &telemetryStreamBinding{stream: stream},
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
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
	state := session.aircraftCommands["command-1"]
	session.pendingMu.Unlock()
	if state == nil || !state.completed || status.Code(state.err) != codes.DeadlineExceeded {
		t.Fatalf("expired command state = %#v", state)
	}
}

func TestAircraftCommandCallerCancellationDoesNotFinalizeSharedCommand(t *testing.T) {
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 2)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream: &telemetryStreamBinding{stream: stream},
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	request := &relayv1.SendAircraftCommandRequest{
		AgentId: "agent-1",
		Command: &agentv1.AircraftCommand{
			CommandId: "command-shared", AircraftId: "aircraft-1",
			Type: agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_ARM,
		},
	}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := relay.SendAircraftCommand(firstCtx, request)
		first <- err
	}()
	<-stream.sentAckChan
	second := make(chan error, 1)
	go func() {
		_, err := relay.SendAircraftCommand(context.Background(), request)
		second <- err
	}()
	select {
	case duplicate := <-stream.sentAckChan:
		t.Fatalf("shared caller redelivered command: %#v", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
	cancelFirst()
	if err := <-first; status.Code(err) != codes.Canceled {
		t.Fatalf("first caller error = %v, want Canceled", err)
	}
	select {
	case err := <-second:
		t.Fatalf("first cancellation finalized shared command: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	session.handleAircraftCommandResult(&agentv1.AircraftCommandResult{
		CommandId: "command-shared", AircraftId: "aircraft-1",
		Status: agentv1.AircraftCommandResult_STATUS_ACCEPTED,
	})
	if err := <-second; err != nil {
		t.Fatalf("second caller error = %v", err)
	}
}

func TestAircraftCommandCallerCancellationDuringSendDoesNotFinalizeSharedCommand(t *testing.T) {
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
	request := &relayv1.SendAircraftCommandRequest{
		AgentId: "agent-1",
		Command: &agentv1.AircraftCommand{
			CommandId: "command-shared-send", AircraftId: "aircraft-1",
			Type: agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_ARM,
		},
	}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := relay.SendAircraftCommand(firstCtx, request)
		first <- err
	}()
	<-stream.sendStarted
	second := make(chan error, 1)
	go func() {
		_, err := relay.SendAircraftCommand(context.Background(), request)
		second <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		session.pendingMu.Lock()
		state := session.aircraftCommands["command-shared-send"]
		attached := state != nil && state.waiters == 2
		session.pendingMu.Unlock()
		if attached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second caller did not attach to blocked delivery")
		}
		time.Sleep(time.Millisecond)
	}

	cancelFirst()
	if err := <-first; status.Code(err) != codes.Canceled {
		t.Fatalf("first caller error = %v, want Canceled", err)
	}
	select {
	case err := <-second:
		t.Fatalf("first cancellation finalized blocked shared delivery: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(stream.sendBlock)
	<-stream.sentAckChan
	session.handleAircraftCommandResult(&agentv1.AircraftCommandResult{
		CommandId: "command-shared-send", AircraftId: "aircraft-1",
		Status: agentv1.AircraftCommandResult_STATUS_ACCEPTED,
	})
	if err := <-second; err != nil {
		t.Fatalf("second caller error = %v", err)
	}
}

func TestAircraftCommandDeliveryHoldsSessionLeaseUntilBlockedSendCompletes(t *testing.T) {
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
		_, err := relay.SendAircraftCommand(commandCtx, &relayv1.SendAircraftCommandRequest{
			AgentId: "agent-1",
			Command: &agentv1.AircraftCommand{
				CommandId: "command-replace", AircraftId: "aircraft-1",
				Type: agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_ARM,
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
		t.Fatalf("session replaced while old command send was blocked: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancelCommand()
	if err := <-commandResult; status.Code(err) != codes.Canceled {
		t.Fatalf("command caller error = %v, want Canceled", err)
	}
	select {
	case err := <-replaced:
		t.Fatalf("caller cancellation released lease before old send completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(stream.sendBlock)
	<-stream.sentAckChan
	select {
	case err := <-replaced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("session replacement did not resume after old command send completed")
	}
}

func TestSendAircraftCommandDoesNotBlockSessionRetirementWhileAwaitingResult(t *testing.T) {
	stream := &mockTelemetryStream{
		ctx:         context.Background(),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
	}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1",
		stream: &telemetryStreamBinding{stream: stream},
	}
	relay := &Relay{
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	completed := make(chan error, 1)
	go func() {
		_, err := relay.SendAircraftCommand(context.Background(), &relayv1.SendAircraftCommandRequest{
			AgentId: "agent-1",
			Command: &agentv1.AircraftCommand{
				CommandId: "command-retire", AircraftId: "aircraft-1",
				Type: agentv1.AircraftCommandType_AIRCRAFT_COMMAND_TYPE_ARM,
			},
		})
		completed <- err
	}()
	<-stream.sentAckChan

	retired := make(chan struct{})
	go func() {
		session.ownershipMu.Lock()
		session.retired = true
		session.abortPendingCommands()
		session.ownershipMu.Unlock()
		close(retired)
	}()
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("session retirement blocked behind aircraft command result wait")
	}
	select {
	case err := <-completed:
		if status.Code(err) != codes.Aborted {
			t.Fatalf("command error = %v, want Aborted", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending command did not wake when session retired")
	}
}

func TestSendAircraftCommandRequiresAuthorizedControlCaller(t *testing.T) {
	want := status.Error(codes.PermissionDenied, "denied")
	relay := &Relay{controlAuthorizer: func(context.Context) error { return want }}
	_, err := relay.SendAircraftCommand(context.Background(), &relayv1.SendAircraftCommandRequest{})
	if !errors.Is(err, want) {
		t.Fatalf("SendAircraftCommand() error = %v, want %v", err, want)
	}
}
