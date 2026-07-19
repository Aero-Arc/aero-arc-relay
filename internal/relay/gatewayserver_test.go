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
	"strings"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/makinje/aero-arc-relay/internal/mock"
	"github.com/makinje/aero-arc-relay/internal/outputs"
	"github.com/makinje/aero-arc-relay/internal/telemetrywriter"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestRegister(t *testing.T) {
	// Setup
	relay := &Relay{
		grpcSessions: make(map[string]*DroneSession),
	}

	req := &agentv1.RegisterRequest{
		AgentId: "  agent-123  ",
	}
	const agentID = "agent-123"

	// Execute
	resp, err := relay.Register(context.Background(), req)
	// Verify
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if resp.AgentId != agentID {
		t.Errorf("Expected AgentId %s, got %s", agentID, resp.AgentId)
	}
	if resp.SessionId == "" {
		t.Error("Expected non-empty SessionId")
	}

	// Verify session storage
	relay.sessionsMu.RLock()
	session, ok := relay.grpcSessions[agentID]
	_, untrimmedExists := relay.grpcSessions[req.AgentId]
	relay.sessionsMu.RUnlock()

	if !ok {
		t.Fatal("Session was not stored in map")
	}
	if session.agentID != agentID {
		t.Errorf("Expected session agentID %s, got %s", agentID, session.agentID)
	}
	if untrimmedExists {
		t.Fatal("Session was stored under the untrimmed agent ID")
	}
}

// mockTelemetryStream implements agentv1.AgentGateway_TelemetryStreamServer
type mockTelemetryStream struct {
	grpc.ServerStream
	ctx         context.Context
	recvChan    chan *agentv1.AgentStreamMessage
	sentAckChan chan *agentv1.RelayStreamMessage
	errChan     chan error
	sendStarted chan struct{}
	sendBlock   chan struct{}
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
	if m.sendStarted != nil {
		select {
		case m.sendStarted <- struct{}{}:
		default:
		}
	}
	if m.sendBlock != nil {
		select {
		case <-m.sendBlock:
		case <-m.ctx.Done():
			return m.ctx.Err()
		}
	}
	select {
	case m.sentAckChan <- ack:
		return nil
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func TestTelemetryStream_ACKReflectsTelemetryAdmissionFailure(t *testing.T) {
	tests := []struct {
		name       string
		writeErr   error
		wantStatus agentv1.TelemetryAck_Status
	}{
		{name: "queue full", writeErr: telemetrywriter.ErrQueueFull, wantStatus: agentv1.TelemetryAck_STATUS_RETRY_WITH_BACKOFF},
		{name: "normalization", writeErr: telemetrywriter.ErrNormalize, wantStatus: agentv1.TelemetryAck_STATUS_PERMANENT_ERROR},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relay := relayWithSinks(mock.NewMockSink())
			relay.router.AddConsumer(
				&errorEnvelopeConsumer{name: telemetrywriter.ConsumerName, err: tt.writeErr},
				outputs.MessageFilter{Include: []string{"*"}},
			)
			relay.grpcSessions = make(map[string]*DroneSession)
			agentID := "admission-agent"
			session := &DroneSession{
				agentID:   agentID,
				SessionID: "admission-session",
				pending:   make(map[string]chan *agentv1.OperationContextCommandAck),
			}
			relay.grpcSessions[agentID] = session

			stream, cancel := newAgentTelemetryStream(agentID)
			defer cancel()
			errChannel := make(chan error, 1)
			go func() {
				errChannel <- relay.TelemetryStream(stream)
			}()
			waitForStreamGeneration(t, session, 1)

			stream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
				AgentId: agentID, SessionId: session.SessionID, Seq: 44, MsgName: "Heartbeat", SentAtUnixNs: time.Now().UnixNano(),
			})
			select {
			case message := <-stream.sentAckChan:
				ack := message.GetTelemetryAck()
				if ack == nil {
					t.Fatal("stream response did not contain a telemetry ACK")
					return
				}
				if ack.Status != tt.wantStatus {
					t.Fatalf("ACK status = %v, want %v", ack.Status, tt.wantStatus)
				}
				if ack.Error == "" {
					t.Fatal("failure ACK did not include the admission error")
				}
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for telemetry ACK")
			}

			close(stream.recvChan)
			select {
			case err := <-errChannel:
				if err != nil {
					t.Fatalf("stream returned an error on EOF: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for telemetry stream to close")
			}
		})
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
		AgentId:      agentID,
		SessionId:    "session-stream-test",
		MsgName:      "Heartbeat",
		SentAtUnixNs: time.Now().UnixNano(),
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

	// Test Case 3: Reject a named frame without its durable capture timestamp.
	missingTimestampFrame := &agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: "session-stream-test", Seq: 3, MsgName: "Heartbeat",
	}
	stream.recvChan <- telemetryStreamMessage(missingTimestampFrame)
	select {
	case message := <-stream.sentAckChan:
		ack := message.GetTelemetryAck()
		if ack == nil || ack.Status != agentv1.TelemetryAck_STATUS_PERMANENT_ERROR {
			t.Fatalf("missing timestamp ACK = %#v, want permanent error", ack)
		}
		if !strings.Contains(ack.Error, "capture timestamp") {
			t.Fatalf("missing timestamp ACK error = %q", ack.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for missing timestamp ACK")
	}
	if mockSink.GetMessageCount() != 1 {
		t.Fatalf("missing timestamp frame reached sink; count = %d", mockSink.GetMessageCount())
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
		AgentId:      agentID,
		SessionId:    oldSession.SessionID,
		MsgName:      "Heartbeat",
		SentAtUnixNs: time.Now().UnixNano(),
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

func TestTelemetryStream_ReplacementKeepsACKAndCleanupOnReceivingStream(t *testing.T) {
	relay := relayWithSinks(mock.NewMockSink())
	relay.grpcSessions = make(map[string]*DroneSession)
	agentID := "reconnecting-agent"
	session := &DroneSession{
		agentID:   agentID,
		SessionID: "shared-session",
		pending:   make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = session

	oldStream, cancelOld := newAgentTelemetryStream(agentID)
	defer cancelOld()
	oldErr := make(chan error, 1)
	go func() {
		oldErr <- relay.TelemetryStream(oldStream)
	}()
	waitForStreamGeneration(t, session, 1)

	replacementStream, cancelReplacement := newAgentTelemetryStream(agentID)
	defer cancelReplacement()
	replacementErr := make(chan error, 1)
	go func() {
		replacementErr <- relay.TelemetryStream(replacementStream)
	}()
	waitForStreamGeneration(t, session, 2)

	oldStream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId:      agentID,
		SessionId:    session.SessionID,
		Seq:          42,
		MsgName:      "Heartbeat",
		SentAtUnixNs: time.Now().UnixNano(),
	})
	select {
	case message := <-oldStream.sentAckChan:
		if ack := message.GetTelemetryAck(); ack == nil || ack.Seq != 42 {
			t.Fatalf("old stream received ACK %#v, want telemetry ACK for sequence 42", ack)
		}
	case <-replacementStream.sentAckChan:
		t.Fatal("replacement stream received an ACK for a frame read by the old stream")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ACK on the receiving stream")
	}

	close(oldStream.recvChan)
	select {
	case err := <-oldErr:
		if err != nil {
			t.Fatalf("old stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for old stream to close")
	}
	relay.sessionsMu.RLock()
	current := relay.grpcSessions[agentID]
	relay.sessionsMu.RUnlock()
	if current != session {
		t.Fatal("old stream cleanup removed the replacement stream's session")
	}

	close(replacementStream.recvChan)
	select {
	case err := <-replacementErr:
		if err != nil {
			t.Fatalf("replacement stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for replacement stream to close")
	}
	relay.sessionsMu.RLock()
	_, ok := relay.grpcSessions[agentID]
	relay.sessionsMu.RUnlock()
	if ok {
		t.Fatal("active replacement stream cleanup did not remove its session")
	}
}

func TestTelemetryStream_ReplacementDoesNotWaitForBlockedOldSend(t *testing.T) {
	relay := relayWithSinks(mock.NewMockSink())
	relay.grpcSessions = make(map[string]*DroneSession)
	agentID := "blocked-stream-agent"
	session := &DroneSession{
		agentID:   agentID,
		SessionID: "shared-session",
		pending:   make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = session

	oldStream, cancelOld := newAgentTelemetryStream(agentID)
	defer cancelOld()
	oldStream.sendStarted = make(chan struct{}, 1)
	oldStream.sendBlock = make(chan struct{})
	oldErr := make(chan error, 1)
	go func() {
		oldErr <- relay.TelemetryStream(oldStream)
	}()
	waitForStreamGeneration(t, session, 1)

	oldStream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: session.SessionID, Seq: 45, MsgName: "Heartbeat", SentAtUnixNs: time.Now().UnixNano(),
	})
	select {
	case <-oldStream.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("old stream did not block while sending its ACK")
	}

	replacementStream, cancelReplacement := newAgentTelemetryStream(agentID)
	defer cancelReplacement()
	replacementErr := make(chan error, 1)
	go func() {
		replacementErr <- relay.TelemetryStream(replacementStream)
	}()
	waitForStreamGeneration(t, session, 2)

	replacementStream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: session.SessionID, Seq: 46, MsgName: "Heartbeat", SentAtUnixNs: time.Now().UnixNano(),
	})
	select {
	case message := <-replacementStream.sentAckChan:
		ack := message.GetTelemetryAck()
		if ack == nil || ack.Seq != 46 || ack.Status != agentv1.TelemetryAck_STATUS_OK {
			t.Fatalf("replacement stream ACK = %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement stream could not send while the old stream send was blocked")
	}

	close(oldStream.sendBlock)
	select {
	case <-oldStream.sentAckChan:
	case <-time.After(time.Second):
		t.Fatal("old stream did not finish sending after it was unblocked")
	}
	close(oldStream.recvChan)
	select {
	case err := <-oldErr:
		if err != nil {
			t.Fatalf("old stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for old stream to close")
	}

	close(replacementStream.recvChan)
	select {
	case err := <-replacementErr:
		if err != nil {
			t.Fatalf("replacement stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for replacement stream to close")
	}
}

func TestTelemetryStream_KeepsSessionOwnershipThroughAdmission(t *testing.T) {
	consumer := &blockingEnvelopeConsumer{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	relay := relayWithSinks()
	relay.router.AddConsumer(consumer, outputs.MessageFilter{Include: []string{"*"}})
	relay.grpcSessions = make(map[string]*DroneSession)
	agentID := "admission-owner-agent"
	session := &DroneSession{
		agentID: agentID, SessionID: "admission-owner-session",
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = session

	stream, cancel := newAgentTelemetryStream(agentID)
	defer cancel()
	streamErr := make(chan error, 1)
	go func() {
		streamErr <- relay.TelemetryStream(stream)
	}()
	waitForStreamGeneration(t, session, 1)

	stream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: session.SessionID, Seq: 47,
		MsgName: "Heartbeat", SentAtUnixNs: time.Now().UnixNano(),
	})
	select {
	case <-consumer.started:
	case <-time.After(time.Second):
		t.Fatal("frame admission did not start")
	}

	registerStarted := make(chan struct{})
	registerDone := make(chan error, 1)
	go func() {
		close(registerStarted)
		_, err := relay.Register(context.Background(), &agentv1.RegisterRequest{AgentId: agentID})
		registerDone <- err
	}()
	<-registerStarted
	select {
	case err := <-registerDone:
		t.Fatalf("replacement registration completed during frame admission: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	unrelatedDone := make(chan error, 1)
	go func() {
		_, err := relay.Register(context.Background(), &agentv1.RegisterRequest{AgentId: "unrelated-agent"})
		unrelatedDone <- err
	}()
	select {
	case err := <-unrelatedDone:
		if err != nil {
			t.Fatalf("unrelated Register() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("one session's admission blocked unrelated registration")
	}

	close(consumer.release)
	select {
	case message := <-stream.sentAckChan:
		ack := message.GetTelemetryAck()
		if ack == nil || ack.Seq != 47 || ack.Status != agentv1.TelemetryAck_STATUS_OK {
			t.Fatalf("admitted frame ACK = %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for admitted frame ACK")
	}
	select {
	case err := <-registerDone:
		if err != nil {
			t.Fatalf("replacement Register() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement registration did not finish after admission")
	}

	session.ownershipMu.RLock()
	retired := session.retired
	session.ownershipMu.RUnlock()
	if !retired {
		t.Fatal("replaced session was not retired")
	}

	close(stream.recvChan)
	select {
	case err := <-streamErr:
		if err != nil {
			t.Fatalf("stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for old stream to close")
	}
}

func TestTelemetryStream_RejectsOldStreamAfterActiveReplacementCloses(t *testing.T) {
	mockSink := mock.NewMockSink()
	relay := relayWithSinks(mockSink)
	relay.grpcSessions = make(map[string]*DroneSession)
	agentID := "reconnecting-agent"
	session := &DroneSession{
		agentID:   agentID,
		SessionID: "shared-session",
		pending:   make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = session

	oldStream, cancelOld := newAgentTelemetryStream(agentID)
	defer cancelOld()
	oldErr := make(chan error, 1)
	go func() {
		oldErr <- relay.TelemetryStream(oldStream)
	}()
	waitForStreamGeneration(t, session, 1)

	replacementStream, cancelReplacement := newAgentTelemetryStream(agentID)
	defer cancelReplacement()
	replacementErr := make(chan error, 1)
	go func() {
		replacementErr <- relay.TelemetryStream(replacementStream)
	}()
	waitForStreamGeneration(t, session, 2)

	close(replacementStream.recvChan)
	select {
	case err := <-replacementErr:
		if err != nil {
			t.Fatalf("replacement stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for replacement stream to close")
	}

	oldStream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId:   agentID,
		SessionId: session.SessionID,
		Seq:       43,
		MsgName:   "Heartbeat",
	})
	select {
	case message := <-oldStream.sentAckChan:
		ack := message.GetTelemetryAck()
		if ack == nil {
			t.Fatal("old stream response did not contain a telemetry ACK")
		}
		if ack.Status != agentv1.TelemetryAck_STATUS_PERMANENT_ERROR {
			t.Fatalf("old stream ACK status = %v, want permanent error", ack.Status)
		}
		if ack.Error == "" {
			t.Fatal("old stream rejection did not include an error")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for stale-stream rejection ACK")
	}
	if got := mockSink.GetMessageCount(); got != 0 {
		t.Fatalf("stale stream routed %d telemetry messages, want 0", got)
	}

	close(oldStream.recvChan)
	select {
	case err := <-oldErr:
		if err != nil {
			t.Fatalf("old stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for old stream to close")
	}
}

func TestTelemetryStream_CommandACKStaysBoundToReceivingSession(t *testing.T) {
	relay := relayWithSinks(mock.NewMockSink())
	relay.grpcSessions = make(map[string]*DroneSession)
	agentID := "reconnecting-agent"
	oldPending := make(chan *agentv1.OperationContextCommandAck, 1)
	oldSession := &DroneSession{
		agentID:   agentID,
		SessionID: "old-session",
		pending: map[string]chan *agentv1.OperationContextCommandAck{
			"shared-command": oldPending,
		},
	}
	relay.grpcSessions[agentID] = oldSession

	oldStream, cancelOld := newAgentTelemetryStream(agentID)
	defer cancelOld()
	oldErr := make(chan error, 1)
	go func() {
		oldErr <- relay.TelemetryStream(oldStream)
	}()
	waitForStreamGeneration(t, oldSession, 1)

	if _, err := relay.Register(context.Background(), &agentv1.RegisterRequest{AgentId: agentID}); err != nil {
		t.Fatalf("Register() replacement error = %v", err)
	}
	relay.sessionsMu.RLock()
	replacementSession := relay.grpcSessions[agentID]
	relay.sessionsMu.RUnlock()
	if replacementSession == oldSession {
		t.Fatal("registration did not replace the old session")
	}
	replacementPending := make(chan *agentv1.OperationContextCommandAck, 1)
	replacementSession.pendingMu.Lock()
	replacementSession.pending["shared-command"] = replacementPending
	replacementSession.pendingMu.Unlock()

	commandAck := &agentv1.OperationContextCommandAck{
		CommandId: "shared-command",
		Status:    agentv1.OperationContextCommandAck_STATUS_APPLIED,
		ActiveContext: &agentv1.OperationContext{
			FlightId: "old-flight", IntentId: "old-intent", IntentVersion: 7,
		},
	}
	oldStream.recvChan <- &agentv1.AgentStreamMessage{
		Payload: &agentv1.AgentStreamMessage_OperationContextCommandAck{
			OperationContextCommandAck: commandAck,
		},
	}
	select {
	case got := <-oldPending:
		if got != commandAck {
			t.Fatalf("old pending command received ACK %#v, want %#v", got, commandAck)
		}
	case <-replacementPending:
		t.Fatal("replacement session received a command ACK from the old stream")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for command ACK on the receiving session")
	}

	oldSession.sessionMu.RLock()
	oldFlightID := oldSession.FlightID
	oldIntentID := oldSession.IntentID
	oldIntentVersion := oldSession.IntentVersion
	oldSession.sessionMu.RUnlock()
	if oldFlightID != "old-flight" || oldIntentID != "old-intent" || oldIntentVersion != 7 {
		t.Fatalf("old session context = (%q, %q, %d)", oldFlightID, oldIntentID, oldIntentVersion)
	}
	replacementSession.sessionMu.RLock()
	replacementFlightID := replacementSession.FlightID
	replacementIntentID := replacementSession.IntentID
	replacementIntentVersion := replacementSession.IntentVersion
	replacementSession.sessionMu.RUnlock()
	if replacementFlightID != "" || replacementIntentID != "" || replacementIntentVersion != 0 {
		t.Fatalf(
			"replacement session context was changed to (%q, %q, %d)",
			replacementFlightID, replacementIntentID, replacementIntentVersion,
		)
	}
	replacementSession.pendingMu.Lock()
	_, replacementStillPending := replacementSession.pending["shared-command"]
	replacementSession.pendingMu.Unlock()
	if !replacementStillPending {
		t.Fatal("old stream ACK removed the replacement session's pending command")
	}

	close(oldStream.recvChan)
	select {
	case err := <-oldErr:
		if err != nil {
			t.Fatalf("old stream returned an error on EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for old stream to close")
	}
	relay.sessionsMu.RLock()
	current := relay.grpcSessions[agentID]
	relay.sessionsMu.RUnlock()
	if current != replacementSession {
		t.Fatal("old stream cleanup removed the replacement session")
	}
}

func newAgentTelemetryStream(agentID string) (*mockTelemetryStream, context.CancelFunc) {
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("aero-arc-agent-id", agentID),
	)
	ctx, cancel := context.WithCancel(ctx)
	return &mockTelemetryStream{
		ctx:         ctx,
		recvChan:    make(chan *agentv1.AgentStreamMessage, 1),
		sentAckChan: make(chan *agentv1.RelayStreamMessage, 1),
		errChan:     make(chan error, 1),
	}, cancel
}

func waitForStreamGeneration(t *testing.T, session *DroneSession, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session.sessionMu.RLock()
		generation := session.streamGeneration
		session.sessionMu.RUnlock()
		if generation >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("stream generation did not reach %d", want)
}

func telemetryStreamMessage(frame *agentv1.TelemetryFrame) *agentv1.AgentStreamMessage {
	return &agentv1.AgentStreamMessage{
		Payload: &agentv1.AgentStreamMessage_TelemetryFrame{TelemetryFrame: frame},
	}
}

type errorEnvelopeConsumer struct {
	name string
	err  error
}

func (c *errorEnvelopeConsumer) Name() string { return c.name }

func (c *errorEnvelopeConsumer) WriteEnvelope(context.Context, telemetry.TelemetryEnvelope) error {
	return c.err
}

func (c *errorEnvelopeConsumer) Close(context.Context) error { return nil }

type blockingEnvelopeConsumer struct {
	started chan struct{}
	release chan struct{}
}

func (c *blockingEnvelopeConsumer) Name() string { return "blocking-admission" }

func (c *blockingEnvelopeConsumer) WriteEnvelope(ctx context.Context, _ telemetry.TelemetryEnvelope) error {
	select {
	case c.started <- struct{}{}:
	default:
	}
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *blockingEnvelopeConsumer) Close(context.Context) error { return nil }

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
