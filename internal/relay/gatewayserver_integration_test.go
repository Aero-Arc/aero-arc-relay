//go:build integration

package relay

import (
	"context"
	"io"
	"net"
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
