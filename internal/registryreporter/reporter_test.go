package registryreporter

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	registryv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/registry/v1"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeRegistryClient struct {
	mu sync.Mutex

	relayRegistrations int
	relayHeartbeats    int
	relayRegisterErr   error
	agentRegistrations map[string]int
	agentHeartbeats    map[string]int
	agentRegisterErr   error
	heartbeatAgentErr  error
}

type independentlyBlockingRegistryClient struct {
	*fakeRegistryClient
	slowStarted chan struct{}
	releaseSlow chan struct{}
	fastBeat    chan struct{}
}

func (c *independentlyBlockingRegistryClient) HeartbeatAgent(
	ctx context.Context,
	request *registryv1.HeartbeatAgentRequest,
	_ ...grpc.CallOption,
) (*registryv1.HeartbeatAgentResponse, error) {
	if request.GetAgentId() == "slow-agent" {
		select {
		case c.slowStarted <- struct{}{}:
		default:
		}
		select {
		case <-c.releaseSlow:
			return &registryv1.HeartbeatAgentResponse{}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	select {
	case c.fastBeat <- struct{}{}:
	default:
	}
	return &registryv1.HeartbeatAgentResponse{}, nil
}

func newFakeRegistryClient() *fakeRegistryClient {
	return &fakeRegistryClient{
		agentRegistrations: make(map[string]int),
		agentHeartbeats:    make(map[string]int),
	}
}

func (f *fakeRegistryClient) RegisterRelay(context.Context, *registryv1.RegisterRelayRequest, ...grpc.CallOption) (*registryv1.RegisterRelayResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.relayRegistrations++
	err := f.relayRegisterErr
	f.relayRegisterErr = nil
	return &registryv1.RegisterRelayResponse{}, err
}

func (f *fakeRegistryClient) HeartbeatRelay(context.Context, *registryv1.HeartbeatRelayRequest, ...grpc.CallOption) (*registryv1.HeartbeatRelayResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.relayHeartbeats++
	return &registryv1.HeartbeatRelayResponse{}, nil
}

func (f *fakeRegistryClient) RegisterAgent(_ context.Context, request *registryv1.RegisterAgentRequest, _ ...grpc.CallOption) (*registryv1.RegisterAgentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentRegistrations[request.GetAgent().GetAgentId()]++
	err := f.agentRegisterErr
	f.agentRegisterErr = nil
	return &registryv1.RegisterAgentResponse{}, err
}

func (f *fakeRegistryClient) HeartbeatAgent(_ context.Context, request *registryv1.HeartbeatAgentRequest, _ ...grpc.CallOption) (*registryv1.HeartbeatAgentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentHeartbeats[request.GetAgentId()]++
	err := f.heartbeatAgentErr
	f.heartbeatAgentErr = nil
	return &registryv1.HeartbeatAgentResponse{}, err
}

func TestReporterTelemetryAdmissionDoesNotCallRegistry(t *testing.T) {
	client := newFakeRegistryClient()
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: 10 * time.Second, RequestTimeout: time.Second,
	}, client)
	if err := reporter.RegisterAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}
	if err := reporter.WriteEnvelope(context.Background(), telemetry.TelemetryEnvelope{AgentID: "agent-1"}); err != nil {
		t.Fatal(err)
	}
	if err := reporter.WriteEnvelope(context.Background(), telemetry.TelemetryEnvelope{AgentID: "agent-1"}); err != nil {
		t.Fatal(err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if got := client.agentRegistrations["agent-1"]; got != 1 {
		t.Fatalf("agent registrations = %d, want 1", got)
	}
	if got := client.agentHeartbeats["agent-1"]; got != 0 {
		t.Fatalf("telemetry admission made %d registry heartbeat calls, want 0", got)
	}
}

func TestReporterStartRegistersThenHeartbeatsRelay(t *testing.T) {
	client := newFakeRegistryClient()
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: 5 * time.Millisecond, RequestTimeout: time.Second,
	}, client)
	t.Cleanup(func() { _ = reporter.Close(context.Background()) })

	time.Sleep(15 * time.Millisecond)
	client.mu.Lock()
	registrationsBeforeStart := client.relayRegistrations
	heartbeatsBeforeStart := client.relayHeartbeats
	client.mu.Unlock()
	if registrationsBeforeStart != 0 || heartbeatsBeforeStart != 0 {
		t.Fatalf("relay was published before Start: registrations=%d heartbeats=%d", registrationsBeforeStart, heartbeatsBeforeStart)
	}

	if err := reporter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reporter.Start(context.Background()); err != nil {
		t.Fatalf("idempotent Start: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		client.mu.Lock()
		registrations := client.relayRegistrations
		heartbeats := client.relayHeartbeats
		client.mu.Unlock()
		if registrations == 1 && heartbeats > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("started reporter did not register and heartbeat relay")
}

func TestReporterStartCanRetryFailedRegistrationWithoutStartingHeartbeat(t *testing.T) {
	client := newFakeRegistryClient()
	client.relayRegisterErr = errors.New("registry unavailable")
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: 5 * time.Millisecond, RequestTimeout: time.Second,
	}, client)
	t.Cleanup(func() { _ = reporter.Close(context.Background()) })

	if err := reporter.Start(context.Background()); err == nil {
		t.Fatal("expected first Start to fail")
	}
	time.Sleep(15 * time.Millisecond)
	client.mu.Lock()
	heartbeatsAfterFailure := client.relayHeartbeats
	client.mu.Unlock()
	if heartbeatsAfterFailure != 0 {
		t.Fatalf("heartbeats after failed registration = %d, want 0", heartbeatsAfterFailure)
	}

	if err := reporter.Start(context.Background()); err != nil {
		t.Fatalf("retry Start: %v", err)
	}
	client.mu.Lock()
	registrations := client.relayRegistrations
	client.mu.Unlock()
	if registrations != 2 {
		t.Fatalf("relay registrations = %d, want 2", registrations)
	}
}

func TestReporterFailedReplacementPreservesPriorHeartbeatGeneration(t *testing.T) {
	client := newFakeRegistryClient()
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: 5 * time.Millisecond, RequestTimeout: time.Second,
	}, client)
	if err := reporter.RegisterAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}
	reporter.mu.Lock()
	prior := reporter.activeAgents["agent-1"]
	reporter.mu.Unlock()

	client.mu.Lock()
	client.agentRegisterErr = errors.New("registry unavailable")
	client.mu.Unlock()
	if err := reporter.RegisterAgent(context.Background(), "agent-1"); err == nil {
		t.Fatal("replacement registration unexpectedly succeeded")
	}

	reporter.mu.Lock()
	active := reporter.activeAgents["agent-1"]
	registered := reporter.registeredAgents["agent-1"]
	reporter.mu.Unlock()
	if active != prior || registered != prior || prior.ctx.Err() != nil {
		t.Fatal("failed replacement did not preserve prior heartbeat generation")
	}

	reporter.start()
	t.Cleanup(func() {
		if err := reporter.Close(context.Background()); err != nil {
			t.Errorf("close reporter: %v", err)
		}
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		client.mu.Lock()
		heartbeats := client.agentHeartbeats["agent-1"]
		client.mu.Unlock()
		if heartbeats > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("prior generation stopped heartbeating after failed replacement")
}

func TestReporterReregistersAgentAfterRegistryExpiry(t *testing.T) {
	client := newFakeRegistryClient()
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: time.Second, RequestTimeout: time.Second,
	}, client)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	reporter.now = func() time.Time { return now }
	if err := reporter.RegisterAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}

	client.mu.Lock()
	client.heartbeatAgentErr = status.Error(codes.NotFound, "expired")
	client.mu.Unlock()
	now = now.Add(2 * time.Second)
	if err := reporter.reportAgent(context.Background(), "agent-1", false, nil); err != nil {
		t.Fatal(err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if got := client.agentRegistrations["agent-1"]; got != 2 {
		t.Fatalf("agent registrations = %d, want 2", got)
	}
}

func TestReporterRetriesImmediatelyAfterFailedHeartbeat(t *testing.T) {
	client := newFakeRegistryClient()
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: time.Minute, RequestTimeout: time.Second,
	}, client)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	reporter.now = func() time.Time { return now }
	if err := reporter.RegisterAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	client.heartbeatAgentErr = errors.New("registry unavailable")
	client.mu.Unlock()
	now = now.Add(2 * time.Minute)
	if err := reporter.reportAgent(context.Background(), "agent-1", false, nil); err == nil {
		t.Fatal("expected heartbeat error")
	}
	if err := reporter.reportAgent(context.Background(), "agent-1", false, nil); err != nil {
		t.Fatalf("immediate retry: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if got := client.agentHeartbeats["agent-1"]; got != 2 {
		t.Fatalf("agent heartbeats = %d, want 2", got)
	}
}

func TestReporterHeartbeatsIdleActiveAgent(t *testing.T) {
	client := newFakeRegistryClient()
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: 5 * time.Millisecond, RequestTimeout: time.Second,
	}, client)
	if err := reporter.RegisterAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}
	reporter.start()
	t.Cleanup(func() {
		if err := reporter.Close(context.Background()); err != nil {
			t.Errorf("close reporter: %v", err)
		}
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		client.mu.Lock()
		heartbeats := client.agentHeartbeats["agent-1"]
		client.mu.Unlock()
		if heartbeats > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("idle active agent was not heartbeated")
}

func TestReporterSlowAgentDoesNotDelayAnotherAgentHeartbeat(t *testing.T) {
	client := &independentlyBlockingRegistryClient{
		fakeRegistryClient: newFakeRegistryClient(),
		slowStarted:        make(chan struct{}, 1),
		releaseSlow:        make(chan struct{}),
		fastBeat:           make(chan struct{}, 1),
	}
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: 5 * time.Millisecond, RequestTimeout: 200 * time.Millisecond,
	}, client)
	if err := reporter.RegisterAgent(context.Background(), "slow-agent"); err != nil {
		t.Fatal(err)
	}
	if err := reporter.RegisterAgent(context.Background(), "fast-agent"); err != nil {
		t.Fatal(err)
	}
	reporter.start()
	t.Cleanup(func() {
		select {
		case <-client.releaseSlow:
		default:
			close(client.releaseSlow)
		}
		if err := reporter.Close(context.Background()); err != nil {
			t.Errorf("close reporter: %v", err)
		}
	})

	select {
	case <-client.slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow agent heartbeat did not start")
	}
	select {
	case <-client.fastBeat:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("fast agent heartbeat was delayed by the blocked agent")
	}
}

func TestReporterStopsHeartbeatingDisconnectedAgent(t *testing.T) {
	client := newFakeRegistryClient()
	reporter := newWithClient(Config{
		RelayID: "relay-1", HeartbeatInterval: time.Second, RequestTimeout: time.Second,
	}, client)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	reporter.now = func() time.Time { return now }
	if err := reporter.RegisterAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}
	reporter.StopAgent("agent-1")
	reporter.mu.Lock()
	activeAgents := len(reporter.activeAgents)
	reporter.mu.Unlock()
	if activeAgents != 0 {
		t.Fatalf("active agent bookkeeping after stop = %d, want 0", activeAgents)
	}
	now = now.Add(2 * time.Second)
	if err := reporter.WriteEnvelope(context.Background(), telemetry.TelemetryEnvelope{AgentID: "agent-1"}); err != nil {
		t.Fatal(err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if got := client.agentHeartbeats["agent-1"]; got != 0 {
		t.Fatalf("agent heartbeats after stop = %d, want 0", got)
	}
}

func TestConfigValidation(t *testing.T) {
	valid := Config{Address: "registry:50051", RelayID: "relay-1", AdvertiseAddress: "relay-1", RelayGRPCPort: 50051}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"address":   func(c *Config) { c.Address = "" },
		"relay ID":  func(c *Config) { c.RelayID = "" },
		"advertise": func(c *Config) { c.AdvertiseAddress = "" },
		"port":      func(c *Config) { c.RelayGRPCPort = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			if err := config.validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
