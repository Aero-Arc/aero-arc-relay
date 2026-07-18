/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

package relay

import (
	"context"
	"io"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/makinje/aero-arc-relay/internal/mock"
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
	recvChan    chan *agentv1.AgentStreamMessage
	sentAckChan chan *agentv1.RelayStreamMessage
	errChan     chan error
}

func (m *mockTelemetryStream) Context() context.Context {
	return m.ctx
}

func (m *mockTelemetryStream) Recv() (*agentv1.AgentStreamMessage, error) {
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

func (m *mockTelemetryStream) Send(ack *agentv1.RelayStreamMessage) error {
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
	relay := relayWithSinks(mockSink)
	relay.grpcSessions = make(map[string]*DroneSession)

	// Pre-register session (usually required but updated via stream)
	agentID := "agent-stream-test"
	relay.grpcSessions[agentID] = &DroneSession{
		agentID:   agentID,
		SessionID: "session-stream-test",
		pending:   make(map[string]chan *agentv1.OperationContextCommandAck),
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
		recvChan:    make(chan *agentv1.AgentStreamMessage, 10),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 10),
		errChan:     make(chan error, 1),
	}

	// Run handler in goroutine
	errChan := make(chan error)
	go func() {
		errChan <- relay.TelemetryStream(stream)
	}()

	// Test Case 1: Send Frame
	frame := &agentv1.TelemetryFrame{
		AgentId:   agentID,
		SessionId: "session-stream-test",
		MsgName:   "Heartbeat",
		Fields: map[string]string{
			"type": "1",
		},
	}
	stream.recvChan <- telemetryStreamMessage(frame)

	// Verify ACK
	select {
	case message := <-stream.sentAckChan:
		ack := message.GetTelemetryAck()
		if ack == nil {
			t.Fatal("expected telemetry ACK payload")
		}
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

	// Test Case 2: Reject a frame without a message name and keep the stream open.
	unnamedFrame := &agentv1.TelemetryFrame{AgentId: agentID, SessionId: "session-stream-test", Seq: 2}
	stream.recvChan <- telemetryStreamMessage(unnamedFrame)
	select {
	case message := <-stream.sentAckChan:
		ack := message.GetTelemetryAck()
		if ack == nil {
			t.Fatal("expected telemetry ACK payload")
		}
		if ack.Status != agentv1.TelemetryAck_STATUS_PERMANENT_ERROR {
			t.Errorf("Expected permanent error status, got %v", ack.Status)
		}
		if ack.Error == "" {
			t.Error("Expected validation error for unnamed frame")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for unnamed frame ACK")
	}
	if mockSink.GetMessageCount() != 1 {
		t.Errorf("Unnamed frame reached sink; message count = %d, want 1", mockSink.GetMessageCount())
	}

	// Test Case 3: Clean Shutdown
	close(stream.recvChan) // Trigger io.EOF

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Expected nil error on EOF, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for handler to return")
	}

	// Verify the disconnected session is no longer reported as active.
	relay.sessionsMu.RLock()
	_, ok := relay.grpcSessions[agentID]
	relay.sessionsMu.RUnlock()
	if ok {
		t.Error("expected session to be removed after stream closes")
	}
}

func TestTelemetryStream_OldStreamDoesNotDeleteReplacementSession(t *testing.T) {
	relay := relayWithSinks(mock.NewMockSink())
	relay.grpcSessions = make(map[string]*DroneSession)
	agentID := "reconnecting-agent"
	oldSession := &DroneSession{
		agentID:   agentID,
		SessionID: "old-session",
		pending:   make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = oldSession

	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("aero-arc-agent-id", agentID),
	)
	stream := &mockTelemetryStream{
		ctx:         ctx,
		recvChan:    make(chan *agentv1.AgentStreamMessage, 1),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
		errChan:     make(chan error, 1),
	}
	errChan := make(chan error, 1)
	go func() {
		errChan <- relay.TelemetryStream(stream)
	}()

	stream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId:   agentID,
		SessionId: oldSession.SessionID,
		MsgName:   "Heartbeat",
	})
	select {
	case <-stream.sentAckChan:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for old stream to become active")
	}

	replacement := &DroneSession{
		agentID:   agentID,
		SessionID: "replacement-session",
		pending:   make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.sessionsMu.Lock()
	relay.grpcSessions[agentID] = replacement
	relay.sessionsMu.Unlock()
	close(stream.recvChan)

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("old stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for old stream to close")
	}

	relay.sessionsMu.RLock()
	current := relay.grpcSessions[agentID]
	relay.sessionsMu.RUnlock()
	if current != replacement {
		t.Fatal("old stream cleanup removed or replaced the current session")
	}
}

func telemetryStreamMessage(frame *agentv1.TelemetryFrame) *agentv1.AgentStreamMessage {
	return &agentv1.AgentStreamMessage{
		Payload: &agentv1.AgentStreamMessage_TelemetryFrame{TelemetryFrame: frame},
	}
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
