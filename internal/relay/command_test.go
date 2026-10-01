package relay

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aero-arc/aero-arc-protos/commanddigest"
	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/makinje/aero-arc-relay/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDurableCommandRequiresCapabilityAndAgentEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream := &mockTelemetryStream{ctx: ctx, sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	binding := &telemetryStreamBinding{stream: stream}
	session := &DroneSession{agentID: "agent", SessionID: "session", stream: binding, operationGate: makeOperationGate()}
	authenticator, err := newAgentTokenAuthenticator(map[string]string{"agent": testAgentToken})
	if err != nil {
		t.Fatal(err)
	}
	r := &Relay{agentAuthenticator: authenticator, controlAuthorizer: func(context.Context) error { return nil }, grpcSessions: map[string]*DroneSession{"agent": session}, config: &config.Config{Telemetry: config.TelemetryConfig{RelayID: "relay-test", AgentMappings: map[string]config.AgentMapping{"agent": {OperatorID: "operator", AircraftID: "aircraft"}}}}}
	now := time.Now()
	c := &agentv1.DurableCommand{CommandId: "command", OperatorId: "operator", AircraftId: "aircraft", AgentId: "agent", Context: &agentv1.OperationContext{AircraftId: "aircraft", FlightId: "flight", IntentId: "intent", IntentVersion: 1}, Definition: "ARM", DefinitionVersion: 1, Capability: "mavlink_command_v1", IssuedAtUnixMs: now.UnixMilli(), ExpiresAtUnixMs: now.Add(time.Second).UnixMilli(), RecoveryPolicy: "no_repeat_effect_v1", Execution: &agentv1.DurableCommand_Mavlink{Mavlink: &agentv1.MavlinkExecution{Command: 400, Parameters: []float32{1, 0, 0, 0, 0, 0, 0}, Observation: "armed"}}}
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

func TestStreamingCommandKeepsOneDeliveryThroughProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream := &mockTelemetryStream{ctx: ctx, sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	binding := &telemetryStreamBinding{stream: stream}
	session := &DroneSession{agentID: "agent", SessionID: "session", stream: binding, operationGate: makeOperationGate()}
	authenticator, err := newAgentTokenAuthenticator(map[string]string{"agent": testAgentToken})
	if err != nil {
		t.Fatal(err)
	}
	r := &Relay{agentAuthenticator: authenticator, controlAuthorizer: func(context.Context) error { return nil }, grpcSessions: map[string]*DroneSession{"agent": session}, config: &config.Config{Telemetry: config.TelemetryConfig{RelayID: "relay-test", AgentMappings: map[string]config.AgentMapping{"agent": {OperatorID: "operator", AircraftID: "aircraft"}}}}}
	now := time.Now()
	c := &agentv1.DurableCommand{CommandId: "command", OperatorId: "operator", AircraftId: "aircraft", AgentId: "agent", Context: &agentv1.OperationContext{AircraftId: "aircraft", FlightId: "flight", IntentId: "intent", IntentVersion: 1}, Definition: "ARM", DefinitionVersion: 1, Capability: "mavlink_command_v1", IssuedAtUnixMs: now.UnixMilli(), ExpiresAtUnixMs: now.Add(time.Second).UnixMilli(), RecoveryPolicy: "no_repeat_effect_v1", Execution: &agentv1.DurableCommand_Mavlink{Mavlink: &agentv1.MavlinkExecution{Command: 400, Parameters: []float32{1, 0, 0, 0, 0, 0, 0}, Observation: "armed"}}}
	c.CommandDigest, err = commanddigest.Digest(c)
	if err != nil {
		t.Fatal(err)
	}
	req := &relayv1.ExchangeCommandRequest{AgentId: "agent", Command: c, AttemptId: "command/attempt-1"}
	session.executionCapabilities = []string{"mavlink_command_v1"}

	snapshots := make(chan *agentv1.CommandEvidence, 8)
	done := make(chan error, 1)
	go func() {
		done <- r.exchangeCommand(ctx, req, true, func(e *agentv1.CommandEvidence) error { snapshots <- e; return nil })
	}()
	select {
	case <-stream.sentAckChan:
	case <-ctx.Done():
		t.Fatal("no delivery")
	}
	events := []*agentv1.CommandEvent{}
	for _, stage := range []string{"acknowledged", "verifying_mission", "awaiting_ack", "applied", "observed"} {
		events = append(events, &agentv1.CommandEvent{EventId: "command/" + stage, Stage: stage, OccurredAtUnixMs: now.UnixMilli()})
		session.handleC2Evidence(binding, &agentv1.CommandEvidence{CommandId: c.CommandId, CommandDigest: c.CommandDigest, Events: events})
		select {
		case got := <-snapshots:
			if len(got.Events) != len(events)+2 {
				t.Fatalf("lost cumulative progress: %v", got)
			}
		case <-ctx.Done():
			t.Fatal("progress not streamed")
		}
		if stage != "observed" {
			select {
			case err := <-done:
				t.Fatalf("ended before observation: %v", err)
			default:
			}
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-stream.sentAckChan:
		t.Fatal("progress redelivered command")
	default:
	}
}

func TestCumulativeEvidenceRetainsCompletionAndRejectsOldStream(t *testing.T) {
	binding := &telemetryStreamBinding{}
	ch := make(chan *agentv1.CommandEvidence, 1)
	session := &DroneSession{stream: binding, c2Pending: map[string]chan *agentv1.CommandEvidence{"c": ch}}
	session.handleC2Evidence(binding, &agentv1.CommandEvidence{CommandId: "c", Events: []*agentv1.CommandEvent{{Stage: "acknowledged"}}})
	session.handleC2Evidence(binding, &agentv1.CommandEvidence{CommandId: "c", Events: []*agentv1.CommandEvent{{Stage: "acknowledged"}, {Stage: "applied"}, {Stage: "observed"}}})
	session.handleC2Evidence(&telemetryStreamBinding{}, &agentv1.CommandEvidence{CommandId: "c"})
	if !commandEvidenceComplete(<-ch) {
		t.Fatal("completion lost behind initial acknowledgment")
	}
}

func TestRetiredSessionRejectsC2Evidence(t *testing.T) {
	binding := &telemetryStreamBinding{}
	ch := make(chan *agentv1.CommandEvidence, 1)
	session := &DroneSession{retired: true, stream: binding, c2Pending: map[string]chan *agentv1.CommandEvidence{"c": ch}}
	session.handleC2Evidence(binding, &agentv1.CommandEvidence{CommandId: "c"})
	select {
	case <-ch:
		t.Fatal("retired session accepted evidence")
	default:
	}
}

func TestLostSessionWakesC2WaitersAndDropsBufferedEvidence(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			ch := make(chan *agentv1.CommandEvidence, 1)
			ch <- &agentv1.CommandEvidence{CommandId: "c"}
			session := &DroneSession{c2Pending: map[string]chan *agentv1.CommandEvidence{"c": ch}}
			if replacement {
				session.abortPendingCommandsForStreamReplacement()
			} else {
				session.abortPendingCommands()
			}
			select {
			case e, ok := <-ch:
				if ok {
					t.Fatalf("stale evidence survived: %v", e)
				}
			default:
				t.Fatal("waiter was not woken")
			}
			if len(session.c2Pending) != 0 {
				t.Fatal("pending correlation retained")
			}
		})
	}
}

func TestDurableCommandRejectsUnauthenticatedAgentConfiguration(t *testing.T) {
	r := &Relay{controlAuthorizer: func(context.Context) error { return nil }}
	if _, err := r.ExchangeCommand(context.Background(), &relayv1.ExchangeCommandRequest{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unauthenticated command path enabled: %v", err)
	}
}

func TestOldExchangeCleanupPreservesReplacementWaiter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream := &mockTelemetryStream{ctx: ctx, sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	binding := &telemetryStreamBinding{stream: stream}
	session := &DroneSession{agentID: "agent", SessionID: "session", stream: binding, operationGate: makeOperationGate()}
	authenticator, err := newAgentTokenAuthenticator(map[string]string{"agent": testAgentToken})
	if err != nil {
		t.Fatal(err)
	}
	r := &Relay{agentAuthenticator: authenticator, controlAuthorizer: func(context.Context) error { return nil }, grpcSessions: map[string]*DroneSession{"agent": session}, config: &config.Config{Telemetry: config.TelemetryConfig{RelayID: "relay-test", AgentMappings: map[string]config.AgentMapping{"agent": {OperatorID: "operator", AircraftID: "aircraft"}}}}}
	now := time.Now()
	c := &agentv1.DurableCommand{CommandId: "command", OperatorId: "operator", AircraftId: "aircraft", AgentId: "agent", Context: &agentv1.OperationContext{AircraftId: "aircraft", FlightId: "flight", IntentId: "intent", IntentVersion: 1}, Definition: "ARM", DefinitionVersion: 1, Capability: "mavlink_command_v1", IssuedAtUnixMs: now.UnixMilli(), ExpiresAtUnixMs: now.Add(time.Second).UnixMilli(), RecoveryPolicy: "no_repeat_effect_v1", Execution: &agentv1.DurableCommand_Mavlink{Mavlink: &agentv1.MavlinkExecution{Command: 400, Parameters: []float32{1, 0, 0, 0, 0, 0, 0}, Observation: "armed"}}}
	c.CommandDigest, err = commanddigest.Digest(c)
	if err != nil {
		t.Fatal(err)
	}
	req := &relayv1.ExchangeCommandRequest{AgentId: "agent", Command: c, AttemptId: "command/attempt-1"}
	session.executionCapabilities = []string{"mavlink_command_v1"}

	entered := make(chan struct{})
	unblock := make(chan struct{})
	oldDone := make(chan error, 1)
	go func() {
		oldDone <- r.exchangeCommand(ctx, req, true, func(*agentv1.CommandEvidence) error {
			close(entered)
			<-unblock
			return status.Error(codes.Canceled, "old caller disconnected")
		})
	}()
	select {
	case <-stream.sentAckChan:
	case <-ctx.Done():
		t.Fatal("old delivery missing")
	}
	session.handleC2Evidence(binding, &agentv1.CommandEvidence{CommandId: c.CommandId, CommandDigest: c.CommandDigest})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("old emission did not block")
	}
	session.abortPendingCommandsForStreamReplacement()
	replacement := &telemetryStreamBinding{stream: stream}
	session.sessionMu.Lock()
	session.stream = replacement
	session.sessionMu.Unlock()
	newDone := make(chan error, 1)
	go func() { _, err := r.ExchangeCommand(ctx, req); newDone <- err }()
	select {
	case <-stream.sentAckChan:
	case <-ctx.Done():
		t.Fatal("retry delivery missing")
	}
	close(unblock)
	<-oldDone
	session.handleC2Evidence(replacement, &agentv1.CommandEvidence{CommandId: c.CommandId, CommandDigest: c.CommandDigest})
	select {
	case err := <-newDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("old cleanup deleted replacement evidence waiter")
	}
}

func TestRTLMissionRequiresExplicitCapability(t *testing.T) {
	plan := &agentv1.MissionPlan{SchemaVersion: 1, Items: []*agentv1.MissionItem{{Command: 20}}}
	session := &DroneSession{executionCapabilities: []string{"mavlink_command_v1", "mission_upload_v1"}}
	if err := requireMissionCapabilities(session, plan); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("legacy capability accepted RTL: %v", err)
	}
	session.executionCapabilities = append(session.executionCapabilities, "mission_rtl_v1")
	if err := requireMissionCapabilities(session, plan); err != nil {
		t.Fatal(err)
	}
	plan.Items[0].Command = 21
	session.executionCapabilities = nil
	if err := requireMissionCapabilities(session, plan); err != nil {
		t.Fatalf("legacy LAND compatibility broken: %v", err)
	}
}

type blockedCommandProgress struct {
	grpc.ServerStream
	ctx     context.Context
	started chan struct{}
	release chan struct{}
}

func (s *blockedCommandProgress) Context() context.Context { return s.ctx }
func (s *blockedCommandProgress) Send(*relayv1.ExecuteCommandResponse) error {
	close(s.started)
	<-s.release
	return context.Canceled
}
func TestExecuteCommandReturnsDeadlineBeforeBlockedProgressCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	binding := &telemetryStreamBinding{stream: stream}
	session := &DroneSession{agentID: "agent", SessionID: "session", stream: binding, operationGate: makeOperationGate()}
	authenticator, err := newAgentTokenAuthenticator(map[string]string{"agent": testAgentToken})
	if err != nil {
		t.Fatal(err)
	}
	r := &Relay{agentAuthenticator: authenticator, controlAuthorizer: func(context.Context) error { return nil }, grpcSessions: map[string]*DroneSession{"agent": session}, config: &config.Config{Telemetry: config.TelemetryConfig{RelayID: "relay-test", AgentMappings: map[string]config.AgentMapping{"agent": {OperatorID: "operator", AircraftID: "aircraft"}}}}}
	now := time.Now()
	c := &agentv1.DurableCommand{CommandId: "command", OperatorId: "operator", AircraftId: "aircraft", AgentId: "agent", Context: &agentv1.OperationContext{AircraftId: "aircraft", FlightId: "flight", IntentId: "intent", IntentVersion: 1}, Definition: "ARM", DefinitionVersion: 1, Capability: "mavlink_command_v1", IssuedAtUnixMs: now.UnixMilli(), ExpiresAtUnixMs: now.Add(time.Second).UnixMilli(), RecoveryPolicy: "no_repeat_effect_v1", Execution: &agentv1.DurableCommand_Mavlink{Mavlink: &agentv1.MavlinkExecution{Command: 400, Parameters: []float32{1, 0, 0, 0, 0, 0, 0}, Observation: "armed"}}}
	c.CommandDigest, err = commanddigest.Digest(c)
	if err != nil {
		t.Fatal(err)
	}
	req := &relayv1.ExchangeCommandRequest{AgentId: "agent", Command: c, AttemptId: "command/attempt-1"}
	session.executionCapabilities = []string{"mavlink_command_v1"}
	progress := &blockedCommandProgress{ctx: ctx, started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- r.ExecuteCommand(&relayv1.ExecuteCommandRequest{AgentId: req.AgentId, Command: req.Command, AttemptId: req.AttemptId}, progress)
	}()
	select {
	case <-stream.sentAckChan:
	case <-time.After(time.Second):
		t.Fatal("command not delivered")
	}
	session.handleC2Evidence(binding, &agentv1.CommandEvidence{CommandId: c.CommandId, CommandDigest: c.CommandDigest, Events: []*agentv1.CommandEvent{{EventId: "ack", Stage: "acknowledged"}}})
	select {
	case <-progress.started:
	case <-time.After(time.Second):
		t.Fatal("progress not sent")
	}
	select {
	case err := <-done:
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("deadline result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked progress prevented RPC return")
	}
	// gRPC cancels Send after handler return. Model that transport wakeup and
	// verify that the worker removes its waiter, allowing exact recovery.
	close(progress.release)
	deadline := time.Now().Add(time.Second)
	for {
		session.pendingMu.Lock()
		pending := len(session.c2Pending)
		session.pendingMu.Unlock()
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter survived transport drainage")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDurableIdentityRemainsAfterEvidenceWaiterLeaves(t *testing.T) {
	now := time.Now()
	s := &DroneSession{}
	if err := retainDurableCommandIDLocked(s, "command", "original-digest", now); err != nil {
		t.Fatal(err)
	}
	if err := retainDurableCommandIDLocked(s, "command", "changed-digest", now); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed authority accepted: %v", err)
	}
	if err := retainDurableCommandIDLocked(s, "command", "original-digest", now); err != nil {
		t.Fatalf("exact recovery rejected: %v", err)
	}
	for _, kind := range []retainedCommandKind{retainedOperationCommand, retainedAircraftCommand, retainedMissionDeployment} {
		if err := prepareCommandIDAdmissionLocked(s, "command", kind, now); status.Code(err) != codes.AlreadyExists {
			t.Fatalf("retained durable identity admitted legacy kind %d: %v", kind, err)
		}
	}
	if err := prepareCommandIDAdmissionLocked(s, "command", retainedDurableCommand, now); err != nil {
		t.Fatalf("exact durable recovery blocked: %v", err)
	}
	if err := prepareCommandIDAdmissionLocked(s, "command", retainedAircraftCommand, now.Add(operationCommandRetention+time.Second)); err != nil {
		t.Fatalf("expired retention not released: %v", err)
	}
}
