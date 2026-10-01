//go:build integration

package relay

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type integrationAgentRegistrar struct {
	mu         sync.Mutex
	registered []string
	stopped    []string
}

func (r *integrationAgentRegistrar) RegisterAgent(_ context.Context, agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registered = append(r.registered, agentID)
	return nil
}

func (r *integrationAgentRegistrar) StopAgent(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = append(r.stopped, agentID)
}

func TestAuthenticatedAgentSessionPublishesThroughGRPC(t *testing.T) {
	const (
		agentID = "agent-1"
		token   = "integration-agent-token"
	)
	authenticator, err := newAgentTokenAuthenticator(map[string]string{agentID: token})
	if err != nil {
		t.Fatal(err)
	}
	reporter := &integrationAgentRegistrar{}
	relay := &Relay{
		grpcSessions:       make(map[string]*DroneSession),
		registryReporter:   reporter,
		agentAuthenticator: authenticator,
	}

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	agentv1.RegisterAgentGatewayServer(server, relay)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(
		dialCtx,
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := agentv1.NewAgentGatewayClient(conn)

	if _, err := client.Register(context.Background(), &agentv1.RegisterRequest{AgentId: agentID}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated Register() error = %v, want Unauthenticated", err)
	}
	authCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", bearerPrefix+token)
	registration, err := client.Register(authCtx, &agentv1.RegisterRequest{AgentId: agentID})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx := metadata.AppendToOutgoingContext(
		authCtx,
		"aero-arc-agent-id", agentID,
		"aero-arc-session-id", registration.GetSessionId(),
	)
	stream, err := client.TelemetryStream(streamCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("TelemetryStream Recv() error = %v, want EOF", err)
	}

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.registered) != 1 || reporter.registered[0] != agentID {
		t.Fatalf("registered agents = %v, want [%s]", reporter.registered, agentID)
	}
	if len(reporter.stopped) != 1 || reporter.stopped[0] != agentID {
		t.Fatalf("stopped agents = %v, want [%s]", reporter.stopped, agentID)
	}
}

// Use an actual gRPC transport: canceling a derived context alone cannot unblock
// ServerStream.Send, and cleanup must not prevent the RPC handler from returning.
func TestCommandDeadlineDrainsBackpressuredAgentStream(t *testing.T) {
	relay := &Relay{grpcSessions: make(map[string]*DroneSession)}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	agentv1.RegisterAgentGatewayServer(server, relay)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	defer listener.Close()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(65535), grpc.WithInitialConnWindowSize(65535))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := agentv1.NewAgentGatewayClient(conn)
	registration, err := client.Register(ctx, &agentv1.RegisterRequest{AgentId: "blocked-agent"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.TelemetryStream(metadata.AppendToOutgoingContext(ctx,
		"aero-arc-agent-id", "blocked-agent", "aero-arc-session-id", registration.SessionId))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.CloseSend()
	var session *DroneSession
	for {
		relay.sessionsMu.RLock()
		session = relay.grpcSessions["blocked-agent"]
		relay.sessionsMu.RUnlock()
		session.sessionMu.RLock()
		bound := session.stream != nil
		session.sessionMu.RUnlock()
		if bound {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("stream never bound")
		case <-time.After(time.Millisecond):
		}
	}
	// The client deliberately never reads. The first large message exhausts
	// flow control; later writes block in gRPC until the server handler exits.
	message := &agentv1.RelayStreamMessage{Payload: &agentv1.RelayStreamMessage_TelemetryAck{
		TelemetryAck: &agentv1.TelemetryAck{Error: strings.Repeat("x", 1024*1024)},
	}}
	deliveryCtx, cancelDelivery := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancelDelivery()
	done := make(chan error, 1)
	go func() {
		session.ownershipMu.RLock()
		defer session.ownershipMu.RUnlock()
		for i := 0; i < 100; i++ {
			if err := sendToSessionThroughWrite(deliveryCtx, session, message); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("delivery error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Send survived command deadline")
	}
	// Replacement must now proceed with the old write drained, even though
	// the original client still holds its stream open without reading.
	replacementCtx, cancelReplacement := context.WithTimeout(ctx, time.Second)
	defer cancelReplacement()
	if _, err := client.Register(replacementCtx, &agentv1.RegisterRequest{AgentId: "blocked-agent"}); err != nil {
		t.Fatalf("replacement blocked by expired write: %v", err)
	}
}
