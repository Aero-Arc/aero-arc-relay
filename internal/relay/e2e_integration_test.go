package relay

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestRelay_E2E_RegisterThenRelayControl(t *testing.T) {
	ctx := context.Background()

	// In-memory gRPC server (no hard-coded sleeps, no network flakiness).
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	t.Cleanup(s.Stop)

	r := &Relay{
		grpcSessions: make(map[string]*relayv1.DroneStatus),
		sessionByID:  make(map[string]*relayv1.DroneStatus),
	}

	// Relay now implements AgentGateway + RelayControl backed by in-memory session state.
	agentv1.RegisterAgentGatewayServer(s, r)
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
	regResp, err := agentClient.Register(ctx, &agentv1.RegisterRequest{
		AgentId:      "drone-001",
		AgentVersion: "test",
		Platform:     "test",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// 2) Verify session exists (in-memory)
	r.grpcSessionsMu.RLock()
	ds, ok := r.grpcSessions["drone-001"]
	r.grpcSessionsMu.RUnlock()
	if !ok {
		t.Fatalf("expected session for drone-001 to exist in grpcSessions")
	}
	if ds.GetSessionId() == "" || regResp.GetSessionId() == "" {
		t.Fatalf("expected non-empty session_id in both relay session and register response")
	}

	// 3) Telemetry activity updates heartbeat
	stream, err := agentClient.TelemetryStream(ctx)
	if err != nil {
		t.Fatalf("TelemetryStream: %v", err)
	}
	err = stream.Send(&agentv1.TelemetryFrame{
		SessionId:    regResp.GetSessionId(),
		AgentId:      regResp.GetAgentId(),
		Seq:          1,
		SentAtUnixNs: time.Now().UTC().UnixNano(),
		MsgId:        0,
		MsgName:      "Heartbeat",
	})
	if err != nil {
		t.Fatalf("stream send: %v", err)
	}
	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("stream recv ack: %v", err)
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

	// 5) On disconnect/stream close, session should be removed (no stale sessions).
	_ = stream.CloseSend()
	// Force server-side handler to exit by closing the connection.
	_ = conn.Close()

	// No hard-coded sleep: spin/yield until session is removed (or timeout).
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		r.grpcSessionsMu.RLock()
		_, ok = r.grpcSessions["drone-001"]
		r.grpcSessionsMu.RUnlock()
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected session to be cleaned up after stream/connection close")
		}
		runtime.Gosched()
	}
}


