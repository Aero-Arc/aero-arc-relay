package relay

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/alicebob/miniredis/v2"
	"github.com/makinje/aero-arc-relay/internal/redisconn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// testAgentGateway is a minimal AgentGateway implementation used for E2E tests.
// It populates relay.grpcSessions (keyed by drone_id) on Register and optionally
// writes a Redis mapping if Redis is configured.
//
// NOTE: The current agent proto RegisterRequest does not include a drone_id.
// For this integration test we treat agent_id as the stable drone_id.
type testAgentGateway struct {
	agentv1.UnimplementedAgentGatewayServer
	relay *Relay
}

const redisSessionKeyPrefix = "aeroarc:relay:session_by_drone:"

func (g testAgentGateway) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	agentID := req.GetAgentId()
	droneID := agentID

	sessionID := "sess-" + agentID
	now := time.Now().UTC().UnixNano()

	g.relay.grpcSessionsMu.Lock()
	g.relay.grpcSessions[droneID] = &relayv1.DroneStatus{
		DroneId:             droneID,
		SessionId:           sessionID,
		AgentId:             agentID,
		RelayId:             "relay-local",
		ConnectedAtUnixNs:   now,
		LastHeartbeatUnixNs: now,
	}
	g.relay.grpcSessionsMu.Unlock()

	// Optional Redis mapping write (no dependency required for correctness).
	if rc := redisconn.Get(); rc != nil {
		_ = rc.Underlying().Set(ctx, redisSessionKeyPrefix+droneID, sessionID, 0).Err()
	}

	return &agentv1.RegisterResponse{
		AgentId:     agentID,
		SessionId:   sessionID,
		MaxInflight: 128,
	}, nil
}

func (g testAgentGateway) TelemetryStream(stream grpc.BidiStreamingServer[agentv1.TelemetryFrame, agentv1.TelemetryAck]) error {
	// Minimal stream implementation for completeness; not required by this test flow.
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		_ = stream.Send(&agentv1.TelemetryAck{Seq: frame.GetSeq(), Status: agentv1.TelemetryAck_STATUS_OK})
	}
}

func TestRelay_E2E_RegisterThenRelayControl(t *testing.T) {
	ctx := context.Background()

	// In-memory Redis for verifying mapping writes (no external dependency).
	mr := miniredis.RunT(t)
	t.Setenv("REDIS_ADDR", mr.Addr())
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "0")

	_ = redisconn.InitFromEnv(ctx)

	// In-memory gRPC server (no hard-coded sleeps, no network flakiness).
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	t.Cleanup(s.Stop)

	r := &Relay{
		grpcSessions: make(map[string]*relayv1.DroneStatus),
	}

	agentv1.RegisterAgentGatewayServer(s, testAgentGateway{relay: r})
	relayv1.RegisterRelayControlServer(s, r)

	go func() {
		_ = s.Serve(lis)
	}()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// 1) Register fake agent
	agentClient := agentv1.NewAgentGatewayClient(conn)
	_, err = agentClient.Register(ctx, &agentv1.RegisterRequest{
		AgentId:      "drone-001",
		AgentVersion: "test",
		Platform:     "test",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// 2) Verify session exists (in-memory)
	r.grpcSessionsMu.RLock()
	_, ok := r.grpcSessions["drone-001"]
	r.grpcSessionsMu.RUnlock()
	if !ok {
		t.Fatalf("expected session for drone-001 to exist in grpcSessions")
	}

	// 3) Verify Redis mapping written
	if got, err := mr.Get(redisSessionKeyPrefix + "drone-001"); err != nil || got == "" {
		t.Fatalf("expected redis mapping key to exist, got value=%q err=%v", got, err)
	}

	// 4) Call RelayControl RPCs and validate responses
	relayClient := relayv1.NewRelayControlClient(conn)

	listResp, err := relayClient.ListActiveDrones(ctx, &relayv1.ListActiveDronesRequest{})
	if err != nil {
		t.Fatalf("ListActiveDrones: %v", err)
	}
	if listResp == nil || len(listResp.GetDrones()) != 1 {
		t.Fatalf("expected 1 active drone, got %+v", listResp)
	}
	if listResp.GetDrones()[0].GetDroneId() != "drone-001" {
		t.Fatalf("expected drone_id drone-001, got %q", listResp.GetDrones()[0].GetDroneId())
	}

	statusResp, err := relayClient.GetDroneStatus(ctx, &relayv1.GetDroneStatusRequest{DroneId: "drone-001"})
	if err != nil {
		t.Fatalf("GetDroneStatus: %v", err)
	}
	if statusResp == nil || statusResp.GetDrone() == nil {
		t.Fatalf("expected non-nil drone status")
	}
	if statusResp.GetDrone().GetDroneId() != "drone-001" {
		t.Fatalf("expected drone_id drone-001, got %q", statusResp.GetDrone().GetDroneId())
	}
}


