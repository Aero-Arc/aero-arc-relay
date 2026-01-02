package relay

import (
	"context"
	"io"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/makinje/aero-arc-relay/internal/mock"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestRegister(t *testing.T) {
	// Setup
	relay := &Relay{
		grpcSessions: make(map[string]*DroneSession),
	}

	req := &agentv1.RegisterRequest{
		AgentId: "agent-123",
	}

	// Execute
	resp, err := relay.Register(context.Background(), req)
	// Verify
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if resp.AgentId != req.AgentId {
		t.Errorf("Expected AgentId %s, got %s", req.AgentId, resp.AgentId)
	}
	if resp.SessionId == "" {
		t.Error("Expected non-empty SessionId")
	}

	// Verify session storage
	relay.sessionsMu.RLock()
	session, ok := relay.grpcSessions[req.AgentId]
	relay.sessionsMu.RUnlock()

	if !ok {
		t.Fatal("Session was not stored in map")
	}
	if session.agentID != req.AgentId {
		t.Errorf("Expected session agentID %s, got %s", req.AgentId, session.agentID)
	}
}

// mockTelemetryStream implements agentv1.AgentGateway_TelemetryStreamServer
type mockTelemetryStream struct {
	grpc.ServerStream
	ctx         context.Context
	recvChan    chan *agentv1.TelemetryFrame
	sentAckChan chan *agentv1.TelemetryAck
	errChan     chan error
}

func (m *mockTelemetryStream) Context() context.Context {
	return m.ctx
}

func (m *mockTelemetryStream) Recv() (*agentv1.TelemetryFrame, error) {
	select {
	case msg, ok := <-m.recvChan:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	case err := <-m.errChan:
		return nil, err
	case <-m.ctx.Done():
		return nil, m.ctx.Err()
	}
}

func (m *mockTelemetryStream) Send(ack *agentv1.TelemetryAck) error {
	select {
	case m.sentAckChan <- ack:
		return nil
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func TestTelemetryStream(t *testing.T) {
	// Setup Relay with mock sink
	mockSink := mock.NewMockSink()
	relay := &Relay{
		grpcSessions: make(map[string]*DroneSession),
		sinks:        []sinks.Sink{mockSink},
	}

	// Pre-register session (usually required but updated via stream)
	agentID := "agent-stream-test"
	relay.grpcSessions[agentID] = &DroneSession{
		agentID:   "drone-stream-test",
		SessionID: agentID,
	}

	// Setup Mock Stream
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("aero-arc-agent-id", agentID),
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream := &mockTelemetryStream{
		ctx:         ctx,
		recvChan:    make(chan *agentv1.TelemetryFrame, 10),
		sentAckChan: make(chan *agentv1.TelemetryAck, 10),
		errChan:     make(chan error, 1),
	}

	// Run handler in goroutine
	errChan := make(chan error)
	go func() {
		errChan <- relay.TelemetryStream(stream)
	}()

	// Test Case 1: Send Frame
	frame := &agentv1.TelemetryFrame{
		AgentId: "frame-1",
		MsgName: "Heartbeat",
		Fields: map[string]string{
			"type": "1",
		},
	}
	stream.recvChan <- frame

	// Verify ACK
	select {
	case ack := <-stream.sentAckChan:
		if ack.Seq != frame.Seq {
			t.Errorf("Expected ACK for frame %v, got %v", frame.Seq, ack.Seq)
		}
		if ack.Status != agentv1.TelemetryAck_STATUS_OK {
			t.Errorf("Expected OK status, got %v", ack.Status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for ACK")
	}

	// Verify Processing (Sink)
	// Allow some time for async processing if any (currently sync in handler)
	time.Sleep(100 * time.Millisecond) // Give sinks time to process
	if mockSink.GetMessageCount() != 1 {
		t.Errorf("Expected 1 message in sink, got %d", mockSink.GetMessageCount())
	} else {
		msg := mockSink.GetMessages()[0]
		if msg.AgentID != frame.AgentId {
			t.Errorf("Expected DroneID %s, got %s", frame.AgentId, msg.AgentID)
		}
	}

	// Test Case 2: Clean Shutdown
	close(stream.recvChan) // Trigger io.EOF

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Expected nil error on EOF, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for handler to return")
	}

	// Verify Stream updated in Session
	relay.sessionsMu.RLock()
	session := relay.grpcSessions[agentID]
	relay.sessionsMu.RUnlock()

	session.sessionMu.RLock()
	if session.stream == nil {
		t.Error("Expected stream to be stored in session")
	}
	session.sessionMu.RUnlock()
}

func TestTelemetryStream_MissingMetadata(t *testing.T) {
	relay := &Relay{
		grpcSessions: make(map[string]*DroneSession),
	}

	// No metadata
	stream := &mockTelemetryStream{
		ctx: context.Background(),
	}

	err := relay.TelemetryStream(stream)
	if err == nil {
		t.Error("Expected error for missing metadata")
	}
}

func TestTelemetryStream_MissingAgentID(t *testing.T) {
	relay := &Relay{
		grpcSessions: make(map[string]*DroneSession),
	}

	// Empty metadata
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs())
	stream := &mockTelemetryStream{
		ctx: ctx,
	}

	err := relay.TelemetryStream(stream)
	if err == nil {
		t.Error("Expected error for missing agent ID header")
	}
}

func TestTelemetryStream_UnregisteredAgent(t *testing.T) {
	relay := &Relay{
		grpcSessions: make(map[string]*DroneSession),
	}

	agentID := "unregistered-agent"
	// Setup Mock Stream with valid metadata but invalid session (not registered)
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("aero-arc-agent-id", agentID),
	)

	stream := &mockTelemetryStream{
		ctx: ctx,
	}

	err := relay.TelemetryStream(stream)
	if err == nil {
		t.Error("Expected error for unregistered agent")
	}
}
