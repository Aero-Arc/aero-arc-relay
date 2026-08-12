//go:build integration

package registryreporter

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	registryv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/registry/v1"
	"google.golang.org/grpc"
)

type recordingRegistryServer struct {
	registryv1.UnimplementedAeroRegistryServer
	mu           sync.Mutex
	relay        *registryv1.Relay
	agents       map[string]string
	relayBeats   int
	agentBeats   int
	notification chan struct{}
}

func (s *recordingRegistryServer) RegisterRelay(_ context.Context, request *registryv1.RegisterRelayRequest) (*registryv1.RegisterRelayResponse, error) {
	s.mu.Lock()
	s.relay = request.GetRelay()
	s.mu.Unlock()
	s.notify()
	return &registryv1.RegisterRelayResponse{}, nil
}

func (s *recordingRegistryServer) HeartbeatRelay(context.Context, *registryv1.HeartbeatRelayRequest) (*registryv1.HeartbeatRelayResponse, error) {
	s.mu.Lock()
	s.relayBeats++
	s.mu.Unlock()
	s.notify()
	return &registryv1.HeartbeatRelayResponse{}, nil
}

func (s *recordingRegistryServer) RegisterAgent(_ context.Context, request *registryv1.RegisterAgentRequest) (*registryv1.RegisterAgentResponse, error) {
	s.mu.Lock()
	s.agents[request.GetAgent().GetAgentId()] = request.GetRelayId()
	s.mu.Unlock()
	s.notify()
	return &registryv1.RegisterAgentResponse{}, nil
}

func (s *recordingRegistryServer) HeartbeatAgent(context.Context, *registryv1.HeartbeatAgentRequest) (*registryv1.HeartbeatAgentResponse, error) {
	s.mu.Lock()
	s.agentBeats++
	s.mu.Unlock()
	s.notify()
	return &registryv1.HeartbeatAgentResponse{}, nil
}

func (s *recordingRegistryServer) notify() {
	select {
	case s.notification <- struct{}{}:
	default:
	}
}

func TestReporterPublishesThroughGRPC(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	recording := &recordingRegistryServer{agents: make(map[string]string), notification: make(chan struct{}, 16)}
	registryv1.RegisterAeroRegistryServer(server, recording)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	reporter, err := New(context.Background(), Config{
		Address: listener.Addr().String(), RelayID: "relay-1", AdvertiseAddress: "relay.internal",
		RelayGRPCPort: 50051, HeartbeatInterval: 20 * time.Millisecond, RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reporter.Close(context.Background()) })
	if err := reporter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reporter.RegisterAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for {
		recording.mu.Lock()
		ready := recording.relay != nil && recording.relayBeats > 0 && recording.agentBeats > 0 &&
			recording.agents["agent-1"] == "relay-1"
		recording.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-recording.notification:
		case <-deadline:
			t.Fatal("timed out waiting for relay and agent registry state")
		}
	}
}
