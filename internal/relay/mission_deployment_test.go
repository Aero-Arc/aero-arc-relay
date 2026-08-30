package relay

import (
	"context"
	"encoding/hex"
	"math"
	"strings"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/aero-arc/aero-arc-protos/missiondigest"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"github.com/makinje/aero-arc-relay/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestDeployMissionDeliversOnceAndRetainsCorrelatedResult(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	request := &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command}

	type outcome struct {
		response *relayv1.DeployMissionResponse
		err      error
	}
	completed := make(chan outcome, 2)
	for range 2 {
		go func() {
			response, err := relay.DeployMission(context.Background(), request)
			completed <- outcome{response: response, err: err}
		}()
	}

	select {
	case message := <-stream.sentAckChan:
		if !proto.Equal(message.GetDeployMission(), command) {
			t.Fatalf("delivered command = %+v, want %+v", message.GetDeployMission(), command)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mission delivery")
	}
	select {
	case duplicate := <-stream.sentAckChan:
		t.Fatalf("concurrent exact retry redelivered: %+v", duplicate)
	case <-time.After(20 * time.Millisecond):
	}

	result := testAppliedMissionResult(command)
	session.handleMissionDeploymentResult(result)
	for range 2 {
		select {
		case got := <-completed:
			if got.err != nil || !proto.Equal(got.response.GetResult(), result) {
				t.Fatalf("DeployMission() = %+v, %v", got.response, got.err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for DeployMission")
		}
	}

	retry, err := relay.DeployMission(context.Background(), request)
	if err != nil || !proto.Equal(retry.GetResult(), result) {
		t.Fatalf("retained exact retry = %+v, %v", retry, err)
	}
	select {
	case duplicate := <-stream.sentAckChan:
		t.Fatalf("terminal exact retry redelivered: %+v", duplicate)
	default:
	}

	conflict := proto.Clone(command).(*agentv1.DeployMissionCommand)
	conflict.ExpiresAtUnixMs++
	if _, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: conflict}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting command ID error = %v, want AlreadyExists", err)
	}
}

func TestDeployMissionReplaysRetainedTerminalResultAfterContextAdvances(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	request := &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command}
	completed := make(chan error, 1)
	go func() {
		_, err := relay.DeployMission(context.Background(), request)
		completed <- err
	}()
	<-stream.sentAckChan
	result := testAppliedMissionResult(command)
	session.handleMissionDeploymentResult(result)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}

	session.sessionMu.Lock()
	session.FlightID = "flight-2"
	session.IntentID = "intent-2"
	session.IntentVersion = 4
	session.sessionMu.Unlock()

	replayed, err := relay.DeployMission(context.Background(), request)
	if err != nil || !proto.Equal(replayed.GetResult(), result) {
		t.Fatalf("retained replay after context advance = %+v, %v", replayed, err)
	}
	select {
	case message := <-stream.sentAckChan:
		t.Fatalf("retained replay redelivered after context advance: %+v", message)
	default:
	}

	newCommand := proto.Clone(command).(*agentv1.DeployMissionCommand)
	newCommand.CommandId = "deploy-command-2"
	_, err = relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: newCommand})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("new delivery with stale context error = %v, want FailedPrecondition", err)
	}
	select {
	case message := <-stream.sentAckChan:
		t.Fatalf("new delivery bypassed active-context fence: %+v", message)
	default:
	}
}

func TestDeployMissionTemporaryErrorRetryRedispatchesOnceThenApplies(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	request := &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command}
	type outcome struct {
		response *relayv1.DeployMissionResponse
		err      error
	}

	first := make(chan outcome, 1)
	go func() {
		response, err := relay.DeployMission(context.Background(), request)
		first <- outcome{response: response, err: err}
	}()
	if message := <-stream.sentAckChan; !proto.Equal(message.GetDeployMission(), command) {
		t.Fatalf("initial delivered command = %+v, want %+v", message.GetDeployMission(), command)
	}
	temporary := &agentv1.MissionDeploymentResult{
		CommandId: command.GetCommandId(), Binding: proto.Clone(command.GetBinding()).(*agentv1.MissionBinding),
		Status: agentv1.MissionDeploymentResult_STATUS_TEMPORARY_ERROR, Message: "fresh heartbeat required",
		CompletedAtUnixMs: time.Now().UnixMilli(),
	}
	session.handleMissionDeploymentResult(temporary)
	if got := <-first; got.err != nil || !proto.Equal(got.response.GetResult(), temporary) {
		t.Fatalf("initial temporary result = %+v, %v", got.response, got.err)
	}

	retried := make(chan outcome, 2)
	for range 2 {
		go func() {
			response, err := relay.DeployMission(context.Background(), request)
			retried <- outcome{response: response, err: err}
		}()
	}
	if message := <-stream.sentAckChan; !proto.Equal(message.GetDeployMission(), command) {
		t.Fatalf("retry delivered command = %+v, want %+v", message.GetDeployMission(), command)
	}
	select {
	case duplicate := <-stream.sentAckChan:
		t.Fatalf("concurrent exact retry redelivered: %+v", duplicate)
	case <-time.After(20 * time.Millisecond):
	}

	uncorrelated := testAppliedMissionResult(command)
	uncorrelated.CommandId = "another-command"
	session.handleMissionDeploymentResult(uncorrelated)
	select {
	case got := <-retried:
		t.Fatalf("uncorrelated result completed retry: %+v, %v", got.response, got.err)
	case <-time.After(20 * time.Millisecond):
	}
	applied := testAppliedMissionResult(command)
	session.handleMissionDeploymentResult(applied)
	for range 2 {
		select {
		case got := <-retried:
			if got.err != nil || !proto.Equal(got.response.GetResult(), applied) {
				t.Fatalf("retry result = %+v, %v", got.response, got.err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for coalesced mission retry")
		}
	}

	replayed, err := relay.DeployMission(context.Background(), request)
	if err != nil || !proto.Equal(replayed.GetResult(), applied) {
		t.Fatalf("terminal replay = %+v, %v", replayed, err)
	}
	select {
	case duplicate := <-stream.sentAckChan:
		t.Fatalf("terminal replay redelivered: %+v", duplicate)
	default:
	}
}

func TestDeployMissionRetryableResultRequiresCurrentContextBeforeRedispatch(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	request := &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command}
	completed := make(chan error, 1)
	go func() {
		_, err := relay.DeployMission(context.Background(), request)
		completed <- err
	}()
	<-stream.sentAckChan
	temporary := &agentv1.MissionDeploymentResult{
		CommandId: command.GetCommandId(), Binding: proto.Clone(command.GetBinding()).(*agentv1.MissionBinding),
		Status: agentv1.MissionDeploymentResult_STATUS_TEMPORARY_ERROR, Message: "retry later",
		CompletedAtUnixMs: time.Now().UnixMilli(),
	}
	session.handleMissionDeploymentResult(temporary)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}

	session.sessionMu.Lock()
	session.FlightID = "flight-2"
	session.IntentID = "intent-2"
	session.IntentVersion = 4
	session.sessionMu.Unlock()

	_, err := relay.DeployMission(context.Background(), request)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("retryable redispatch with stale context error = %v, want FailedPrecondition", err)
	}
	select {
	case message := <-stream.sentAckChan:
		t.Fatalf("retryable result was redispatched with stale context: %+v", message)
	default:
	}
}

func TestDeployMissionValidatesCanonicalPlanAndBinding(t *testing.T) {
	valid := testMissionCommand(t)
	tests := map[string]struct {
		mutate func(*agentv1.DeployMissionCommand)
		code   codes.Code
	}{
		"digest mismatch": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Binding.MissionDigest = strings.Repeat("0", 64) },
			code:   codes.InvalidArgument,
		},
		"unsupported frame": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].Frame = 2 },
			code:   codes.InvalidArgument,
		},
		"unsupported command": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].Command = 999 },
			code:   codes.InvalidArgument,
		},
		"bad sequence": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].Sequence = 1 },
			code:   codes.InvalidArgument,
		},
		"dynamic current flag": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].Current = true },
			code:   codes.InvalidArgument,
		},
		"autocontinue disabled": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].Autocontinue = false },
			code:   codes.InvalidArgument,
		},
		"nonzero param1": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].Param1 = 1 },
			code:   codes.InvalidArgument,
		},
		"negative zero param2": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].Param2 = math.Copysign(0, -1) },
			code:   codes.InvalidArgument,
		},
		"waypoint param4": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[1].Param4 = 1 },
			code:   codes.InvalidArgument,
		},
		"land param4 zero": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[2].Param4 = 0 },
			code:   codes.InvalidArgument,
		},
		"relative altitude frame": {
			mutate: func(c *agentv1.DeployMissionCommand) {
				c.Plan.Items[0].Frame = uint32(common.MAV_FRAME_GLOBAL_RELATIVE_ALT)
			},
			code: codes.InvalidArgument,
		},
		"loiter command": {
			mutate: func(c *agentv1.DeployMissionCommand) {
				c.Plan.Items[0].Command = uint32(common.MAV_CMD_NAV_LOITER_TIME)
			},
			code: codes.InvalidArgument,
		},
		"altitude not centimeter stable": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].AltitudeM = math.Float32frombits(0x3f800001) },
			code:   codes.InvalidArgument,
		},
		"negative zero altitude": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].AltitudeM = math.Float32frombits(0x80000000) },
			code:   codes.InvalidArgument,
		},
		"non-finite altitude": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].AltitudeM = float32(math.Inf(1)) },
			code:   codes.InvalidArgument,
		},
		"altitude outside ArduPilot signed centimeter range": {
			mutate: func(c *agentv1.DeployMissionCommand) { c.Plan.Items[0].AltitudeM = 83887 },
			code:   codes.InvalidArgument,
		},
		"too many items": {
			mutate: func(c *agentv1.DeployMissionCommand) {
				c.Plan.Items = make([]*agentv1.MissionItem, maxMissionItems+1)
				for i := range c.Plan.Items {
					c.Plan.Items[i] = &agentv1.MissionItem{Sequence: uint32(i), Frame: 3, Command: 16}
				}
			},
			code: codes.InvalidArgument,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			command := proto.Clone(valid).(*agentv1.DeployMissionCommand)
			tt.mutate(command)
			if name != "digest mismatch" && name != "too many items" {
				digest, err := missionPlanDigest(command.GetPlan())
				if err != nil {
					t.Fatal(err)
				}
				command.Binding.MissionDigest = digest
			}
			if err := validateDeployMissionCommand(command); status.Code(err) != tt.code {
				t.Fatalf("validateDeployMissionCommand() = %v, want %v", err, tt.code)
			}
		})
	}
}

func TestMissionPlanCanonicalV1MatchesContractGoldenVector(t *testing.T) {
	plan := &agentv1.MissionPlan{SchemaVersion: 1, Items: []*agentv1.MissionItem{{
		Sequence: 0, Frame: 0, Command: 16, Current: false, Autocontinue: true,
		LatitudeE7: -353632620, LongitudeE7: 1491652370, AltitudeM: 20.1,
	}}}
	canonical, err := missiondigest.CanonicalBytes(plan)
	if err != nil {
		t.Fatal(err)
	}
	const wantCanonicalHex = "6165726f6172632d6d697373696f6e2d706c616e2d7631000000000100000000000000000000001000010000000000000000000000000000000000000000000000000000000000000000eaebfe9458e8cf1241a0cccd"
	if got := hex.EncodeToString(canonical); got != wantCanonicalHex {
		t.Fatalf("canonical bytes = %q, want %q", got, wantCanonicalHex)
	}
	const wantDigest = "6efa96b36af29a800d53ee7d7baf57d4b24f00d9ce2b408327281e74824acf4f"
	if got, err := missionPlanDigest(plan); err != nil || got != wantDigest {
		t.Fatalf("missionPlanDigest() = %q, %v, want %q", got, err, wantDigest)
	}
}

func TestDeployMissionForwardsExpiredCommandToAgentDurableFence(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	command.IssuedAtUnixMs = time.Now().Add(-2 * time.Second).UnixMilli()
	command.ExpiresAtUnixMs = time.Now().Add(-time.Second).UnixMilli()
	type outcome struct {
		response *relayv1.DeployMissionResponse
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		response, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
		completed <- outcome{response: response, err: err}
	}()
	select {
	case message := <-stream.sentAckChan:
		if !proto.Equal(message.GetDeployMission(), command) {
			t.Fatalf("expired reconciliation command = %+v, want %+v", message.GetDeployMission(), command)
		}
	case <-time.After(time.Second):
		t.Fatal("expired command was not forwarded to the Agent's durable reconciliation fence")
	}
	rejected := &agentv1.MissionDeploymentResult{
		CommandId: command.GetCommandId(), Binding: proto.Clone(command.GetBinding()).(*agentv1.MissionBinding),
		Status: agentv1.MissionDeploymentResult_STATUS_REJECTED, Message: "first effect expired",
		CompletedAtUnixMs: time.Now().UnixMilli(),
	}
	session.handleMissionDeploymentResult(rejected)
	got := <-completed
	if got.err != nil || !proto.Equal(got.response.GetResult(), rejected) {
		t.Fatalf("DeployMission() = %+v, %v, want Agent expiry rejection", got.response, got.err)
	}
}

func TestDeployMissionRejectsUnsafeDeliveryWindow(t *testing.T) {
	tests := map[string]func(*agentv1.DeployMissionCommand){
		"future issue time": func(command *agentv1.DeployMissionCommand) {
			command.IssuedAtUnixMs = time.Now().Add(time.Minute).UnixMilli()
			command.ExpiresAtUnixMs = time.Now().Add(2 * time.Minute).UnixMilli()
		},
		"long validity window": func(command *agentv1.DeployMissionCommand) {
			command.ExpiresAtUnixMs = time.Now().Add(maxMissionCommandWindow + time.Minute).UnixMilli()
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			relay, _, stream := testMissionRelay(t)
			command := testMissionCommand(t)
			mutate(command)
			_, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("DeployMission() = %v, want InvalidArgument", err)
			}
			select {
			case message := <-stream.sentAckChan:
				t.Fatalf("unsafe mission was delivered: %+v", message)
			default:
			}
		})
	}
}

func TestDeployMissionCallerDeadlineRedispatchesOutcomeUnknownOnSameStream(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		_, err := relay.DeployMission(ctx, &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
		completed <- err
	}()
	<-stream.sentAckChan
	if err := <-completed; status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("DeployMission() = %v, want DeadlineExceeded", err)
	}
	type outcome struct {
		response *relayv1.DeployMissionResponse
		err      error
	}
	retried := make(chan outcome, 1)
	go func() {
		response, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
		retried <- outcome{response: response, err: err}
	}()
	select {
	case message := <-stream.sentAckChan:
		if !proto.Equal(message.GetDeployMission(), command) {
			t.Fatalf("deadline retry delivered = %+v, want %+v", message.GetDeployMission(), command)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline retry was not redelivered for Agent recovery")
	}
	applied := testAppliedMissionResult(command)
	applied.Status = agentv1.MissionDeploymentResult_STATUS_ALREADY_APPLIED
	session.handleMissionDeploymentResult(applied)
	got := <-retried
	if got.err != nil || !proto.Equal(got.response.GetResult(), applied) {
		t.Fatalf("retry after waiter deadline = %+v, %v", got.response, got.err)
	}
}

func TestDeployMissionOutcomeUnknownRecoversAfterCommandExpiry(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	command.ExpiresAtUnixMs = time.Now().Add(50 * time.Millisecond).UnixMilli()
	request := &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command}
	type outcome struct {
		response *relayv1.DeployMissionResponse
		err      error
	}
	first := make(chan outcome, 1)
	go func() {
		response, err := relay.DeployMission(context.Background(), request)
		first <- outcome{response: response, err: err}
	}()
	<-stream.sentAckChan
	unknown := uncertainMissionDeploymentResult(command, time.Now(), "upload outcome unknown")
	session.handleMissionDeploymentResult(unknown)
	if got := <-first; got.err != nil || got.response.GetResult().GetStatus() != agentv1.MissionDeploymentResult_STATUS_OUTCOME_UNKNOWN {
		t.Fatalf("initial unknown result = %+v, %v", got.response, got.err)
	}
	time.Sleep(60 * time.Millisecond)
	retried := make(chan outcome, 1)
	go func() {
		response, err := relay.DeployMission(context.Background(), request)
		retried <- outcome{response: response, err: err}
	}()
	select {
	case <-stream.sentAckChan:
	case <-time.After(time.Second):
		t.Fatal("expired uncertain command was not redelivered for readback recovery")
	}
	reconciled := testAppliedMissionResult(command)
	reconciled.Status = agentv1.MissionDeploymentResult_STATUS_ALREADY_APPLIED
	session.handleMissionDeploymentResult(reconciled)
	got := <-retried
	if got.err != nil || !proto.Equal(got.response.GetResult(), reconciled) {
		t.Fatalf("expired outcome recovery = %+v, %v", got.response, got.err)
	}
}

func TestDeployMissionRetainedResultSurvivesCommandExpiry(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	command.ExpiresAtUnixMs = time.Now().Add(100 * time.Millisecond).UnixMilli()
	type outcome struct {
		response *relayv1.DeployMissionResponse
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		response, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
		completed <- outcome{response: response, err: err}
	}()
	<-stream.sentAckChan
	result := testAppliedMissionResult(command)
	session.handleMissionDeploymentResult(result)
	if got := <-completed; got.err != nil {
		t.Fatal(got.err)
	}
	time.Sleep(110 * time.Millisecond)
	retry, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
	if err != nil || !proto.Equal(retry.GetResult(), result) {
		t.Fatalf("expired retained retry = %+v, %v", retry, err)
	}
}

func TestDeployMissionRequiresMappingAndReconciledExactContext(t *testing.T) {
	tests := map[string]struct {
		mutate func(*Relay, *DroneSession)
		code   codes.Code
	}{
		"missing mapping": {
			mutate: func(r *Relay, _ *DroneSession) { r.config.Telemetry.AgentMappings = nil },
			code:   codes.FailedPrecondition,
		},
		"aircraft mapping mismatch": {
			mutate: func(r *Relay, _ *DroneSession) {
				r.config.Telemetry.AgentMappings["agent-1"] = config.AgentMapping{OperatorID: "operator-1", AircraftID: "other"}
			},
			code: codes.FailedPrecondition,
		},
		"unreconciled": {
			mutate: func(_ *Relay, s *DroneSession) { s.operationContextUnreconciled = true },
			code:   codes.FailedPrecondition,
		},
		"legacy context missing aircraft": {
			mutate: func(_ *Relay, s *DroneSession) { s.AircraftID = "" },
			code:   codes.FailedPrecondition,
		},
		"context aircraft mismatch": {
			mutate: func(_ *Relay, s *DroneSession) { s.AircraftID = "other" },
			code:   codes.FailedPrecondition,
		},
		"flight mismatch": {
			mutate: func(_ *Relay, s *DroneSession) { s.FlightID = "other" },
			code:   codes.FailedPrecondition,
		},
		"intent version mismatch": {
			mutate: func(_ *Relay, s *DroneSession) { s.IntentVersion++ },
			code:   codes.FailedPrecondition,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			relay, session, stream := testMissionRelay(t)
			tt.mutate(relay, session)
			_, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: testMissionCommand(t)})
			if status.Code(err) != tt.code {
				t.Fatalf("DeployMission() error = %v, want %v", err, tt.code)
			}
			select {
			case message := <-stream.sentAckChan:
				t.Fatalf("invalid mission was delivered: %+v", message)
			default:
			}
		})
	}
}

func TestDeployMissionRejectsMalformedCorrelatedResult(t *testing.T) {
	relay, session, stream := testMissionRelay(t)
	command := testMissionCommand(t)
	completed := make(chan error, 1)
	go func() {
		_, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
		completed <- err
	}()
	<-stream.sentAckChan
	result := testAppliedMissionResult(command)
	result.OnboardMissionDigest = strings.Repeat("f", 64)
	session.handleMissionDeploymentResult(result)
	if err := <-completed; status.Code(err) != codes.Internal {
		t.Fatalf("DeployMission() malformed result error = %v, want Internal", err)
	}
	_, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
	if status.Code(err) != codes.Internal {
		t.Fatalf("retained malformed result error = %v, want Internal", err)
	}
}

func TestDeployMissionSessionReplacementReturnsOutcomeUnknownWithoutRedelivery(t *testing.T) {
	relay, session, oldStream := testMissionRelay(t)
	command := testMissionCommand(t)
	completed := make(chan *relayv1.DeployMissionResponse, 1)
	errors := make(chan error, 1)
	go func() {
		response, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
		completed <- response
		errors <- err
	}()
	<-oldStream.sentAckChan

	newStream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 1)}
	session.controlStreamMu.Lock()
	session.abortPendingCommandsForStreamReplacement()
	session.sessionMu.Lock()
	session.stream = &telemetryStreamBinding{stream: newStream, generation: 2}
	session.sessionMu.Unlock()
	session.controlStreamMu.Unlock()

	response := <-completed
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	if got := response.GetResult().GetStatus(); got != agentv1.MissionDeploymentResult_STATUS_OUTCOME_UNKNOWN {
		t.Fatalf("replacement result = %v, want OUTCOME_UNKNOWN", got)
	}
	retry, err := relay.DeployMission(context.Background(), &relayv1.DeployMissionRequest{AgentId: "agent-1", Command: command})
	if err != nil || retry.GetResult().GetStatus() != agentv1.MissionDeploymentResult_STATUS_OUTCOME_UNKNOWN {
		t.Fatalf("retry after replacement = %+v, %v", retry, err)
	}
	select {
	case message := <-newStream.sentAckChan:
		t.Fatalf("mission was delivered to replacement stream: %+v", message)
	default:
	}
}

func TestDeployMissionAuthenticatesBeforeValidation(t *testing.T) {
	want := status.Error(codes.PermissionDenied, "denied")
	relay := &Relay{controlAuthorizer: func(context.Context) error { return want }}
	_, err := relay.DeployMission(context.Background(), nil)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("DeployMission() = %v, want PermissionDenied", err)
	}
}

func testMissionRelay(t *testing.T) (*Relay, *DroneSession, *mockTelemetryStream) {
	t.Helper()
	stream := &mockTelemetryStream{ctx: context.Background(), sentAckChan: make(chan *agentv1.RelayStreamMessage, 4)}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "session-1", stream: &telemetryStreamBinding{stream: stream, generation: 1},
		AircraftID: "aircraft-1", FlightID: "flight-1", IntentID: "intent-1", IntentVersion: 3,
	}
	relay := &Relay{
		config: &config.Config{Telemetry: config.TelemetryConfig{AgentMappings: map[string]config.AgentMapping{
			"agent-1": {OperatorID: "operator-1", AircraftID: "aircraft-1"},
		}}},
		controlAuthorizer: func(context.Context) error { return nil },
		grpcSessions:      map[string]*DroneSession{"agent-1": session},
	}
	return relay, session, stream
}

func testMissionCommand(t *testing.T) *agentv1.DeployMissionCommand {
	t.Helper()
	plan := &agentv1.MissionPlan{SchemaVersion: 1, Items: []*agentv1.MissionItem{
		{Sequence: 0, Frame: 0, Command: 22, Autocontinue: true, LatitudeE7: 389000000, LongitudeE7: -770000000, AltitudeM: 20},
		{Sequence: 1, Frame: 0, Command: 16, Autocontinue: true, LatitudeE7: 389001000, LongitudeE7: -770001000, AltitudeM: 30},
		{Sequence: 2, Frame: 0, Command: 21, Autocontinue: true, Param4: 1, LatitudeE7: 389000000, LongitudeE7: -770000000},
	}}
	digest, err := missionPlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	command := &agentv1.DeployMissionCommand{
		CommandId: "deploy-command-1",
		Binding: &agentv1.MissionBinding{
			MissionId: "mission-1", MissionVersion: 2, MissionDigest: digest, DeploymentId: "deployment-1",
			OperatorId: "operator-1", AircraftId: "aircraft-1", FlightId: "flight-1", IntentId: "intent-1", IntentVersion: 3,
		},
		Plan: plan, IssuedAtUnixMs: now.UnixMilli(), ExpiresAtUnixMs: now.Add(time.Minute).UnixMilli(),
	}
	if err := validateDeployMissionCommand(command); err != nil {
		t.Fatalf("test mission command is invalid: %v", err)
	}
	return command
}

func testAppliedMissionResult(command *agentv1.DeployMissionCommand) *agentv1.MissionDeploymentResult {
	return &agentv1.MissionDeploymentResult{
		CommandId: command.GetCommandId(), Binding: proto.Clone(command.GetBinding()).(*agentv1.MissionBinding),
		Status: agentv1.MissionDeploymentResult_STATUS_APPLIED, UploadedItemCount: uint32(len(command.GetPlan().GetItems())),
		OnboardMissionDigest: command.GetBinding().GetMissionDigest(), CompletedAtUnixMs: time.Now().UnixMilli(),
	}
}
