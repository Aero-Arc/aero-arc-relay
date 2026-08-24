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
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/makinje/aero-arc-relay/internal/mock"
	"github.com/makinje/aero-arc-relay/internal/outputs"
	"github.com/makinje/aero-arc-relay/internal/telemetrywriter"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type recordingAgentRegistrar struct {
	agentID string
	err     error
	stopped []string
}

type replacementAgentRegistrar struct {
	mu               sync.Mutex
	registrations    int
	failRegistration int
	stopped          int
}

func (r *replacementAgentRegistrar) RegisterAgent(context.Context, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registrations++
	if r.registrations == r.failRegistration {
		return errors.New("registry unavailable")
	}
	return nil
}

func (r *replacementAgentRegistrar) StopAgent(string) {
	r.mu.Lock()
	r.stopped++
	r.mu.Unlock()
}

func (r *replacementAgentRegistrar) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registrations, r.stopped
}

const testAgentToken = "test-agent-token"

const testWALGenerationID = "0195f6a8-86d1-7be7-a104-3a814dc19f9e"

const nilWALGenerationID = "00000000-0000-0000-0000-000000000000"

func relayWithRegistryReporter(t *testing.T, reporter agentRegistryReporter) *Relay {
	t.Helper()
	authenticator, err := newAgentTokenAuthenticator(map[string]string{"agent-1": testAgentToken})
	if err != nil {
		t.Fatal(err)
	}
	return &Relay{
		grpcSessions:       make(map[string]*DroneSession),
		registryReporter:   reporter,
		agentAuthenticator: authenticator,
	}
}

func authenticatedAgentContext(agentID string) context.Context {
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("authorization", bearerPrefix+testAgentToken),
	)
}

type blockingStopAgentRegistrar struct {
	stopStarted chan struct{}
	releaseStop chan struct{}
}

type blockingRegisterAgentRegistrar struct {
	registerStarted chan struct{}
	releaseRegister chan struct{}
}

type blockingReplacementAgentRegistrar struct {
	mu              sync.Mutex
	registrations   int
	registerStarted chan struct{}
	releaseRegister chan struct{}
}

func (r *blockingReplacementAgentRegistrar) RegisterAgent(ctx context.Context, _ string) error {
	r.mu.Lock()
	r.registrations++
	call := r.registrations
	r.mu.Unlock()
	if call == 1 {
		return nil
	}
	if call == 2 {
		select {
		case r.registerStarted <- struct{}{}:
		default:
		}
		select {
		case <-r.releaseRegister:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (*blockingReplacementAgentRegistrar) StopAgent(string) {}

func (r *blockingRegisterAgentRegistrar) RegisterAgent(ctx context.Context, _ string) error {
	select {
	case r.registerStarted <- struct{}{}:
	default:
	}
	select {
	case <-r.releaseRegister:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (*blockingRegisterAgentRegistrar) StopAgent(string) {}

func (*blockingStopAgentRegistrar) RegisterAgent(context.Context, string) error { return nil }

func (r *blockingStopAgentRegistrar) StopAgent(string) {
	close(r.stopStarted)
	<-r.releaseStop
}

func (r *recordingAgentRegistrar) StopAgent(agentID string) {
	r.stopped = append(r.stopped, agentID)
}

func (r *recordingAgentRegistrar) RegisterAgent(_ context.Context, agentID string) error {
	r.agentID = agentID
	return r.err
}

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

func TestRegisterAuthenticatesClaimedAgentBeforeCreatingSession(t *testing.T) {
	authenticator, err := newAgentTokenAuthenticator(map[string]string{"agent-1": testAgentToken})
	if err != nil {
		t.Fatal(err)
	}
	relay := &Relay{
		grpcSessions:       make(map[string]*DroneSession),
		agentAuthenticator: authenticator,
	}

	for name, ctx := range map[string]context.Context{
		"missing": context.Background(),
		"wrong": metadata.NewIncomingContext(
			context.Background(), metadata.Pairs("authorization", bearerPrefix+"wrong-token"),
		),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := relay.Register(ctx, &agentv1.RegisterRequest{AgentId: "agent-1"})
			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf("Register() error = %v, want Unauthenticated", err)
			}
		})
	}
	if len(relay.grpcSessions) != 0 {
		t.Fatal("unauthenticated registration created a session")
	}

	response, err := relay.Register(
		authenticatedAgentContext("agent-1"),
		&agentv1.RegisterRequest{AgentId: "agent-1"},
	)
	if err != nil {
		t.Fatalf("authenticated Register() error = %v", err)
	}
	if response.GetSessionId() == "" {
		t.Fatal("authenticated registration returned no session ID")
	}
}

func TestRegisterDoesNotPublishAgentBeforeTelemetryStream(t *testing.T) {
	reporter := &recordingAgentRegistrar{}
	relay := relayWithRegistryReporter(t, reporter)
	if _, err := relay.Register(authenticatedAgentContext("agent-1"), &agentv1.RegisterRequest{AgentId: "agent-1"}); err != nil {
		t.Fatal(err)
	}
	if reporter.agentID != "" {
		t.Fatalf("registered agent = %q before its telemetry stream connected", reporter.agentID)
	}
}

func TestRegisterSucceedsWhileRegistryIsUnavailable(t *testing.T) {
	reporter := &recordingAgentRegistrar{err: errors.New("registry unavailable")}
	relay := relayWithRegistryReporter(t, reporter)
	if _, err := relay.Register(authenticatedAgentContext("agent-1"), &agentv1.RegisterRequest{AgentId: "agent-1"}); err != nil {
		t.Fatalf("Register error = %v", err)
	}
	if len(relay.grpcSessions) != 1 {
		t.Fatal("registration handshake did not publish its local session")
	}
}

func TestTelemetryStreamPublishesOnlyActiveAgentAndStopsOnDisconnect(t *testing.T) {
	reporter := &recordingAgentRegistrar{}
	relay := relayWithRegistryReporter(t, reporter)
	const agentID = "agent-1"
	relay.grpcSessions[agentID] = &DroneSession{
		agentID: agentID, SessionID: "session-1", pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	stream, cancel := newAgentTelemetryStream(agentID, "session-1")
	defer cancel()
	close(stream.recvChan)

	if err := relay.TelemetryStream(stream); err != nil {
		t.Fatalf("TelemetryStream error = %v", err)
	}
	if reporter.agentID != agentID {
		t.Fatalf("registered agent = %q, want %q", reporter.agentID, agentID)
	}
	if len(reporter.stopped) != 1 || reporter.stopped[0] != agentID {
		t.Fatalf("stopped agents = %v, want [%s]", reporter.stopped, agentID)
	}
}

func TestTelemetryStreamRejectsAgentWhenRegistryPublicationFails(t *testing.T) {
	reporter := &recordingAgentRegistrar{err: errors.New("registry unavailable")}
	relay := relayWithRegistryReporter(t, reporter)
	const agentID = "agent-1"
	relay.grpcSessions[agentID] = &DroneSession{
		agentID: agentID, SessionID: "session-1", pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	stream, cancel := newAgentTelemetryStream(agentID, "session-1")
	defer cancel()

	err := relay.TelemetryStream(stream)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("TelemetryStream error = %v, want Unavailable", err)
	}
	if len(relay.grpcSessions) != 0 {
		t.Fatal("failed registry publication left an active local session")
	}
	if len(reporter.stopped) != 1 || reporter.stopped[0] != agentID {
		t.Fatalf("stopped agents = %v, want [%s]", reporter.stopped, agentID)
	}
}

func TestTelemetryStreamFailedReplacementPreservesAcceptedStream(t *testing.T) {
	reporter := &replacementAgentRegistrar{failRegistration: 2}
	relay := relayWithRegistryReporter(t, reporter)
	const agentID = "agent-1"
	session := &DroneSession{
		agentID: agentID, SessionID: "session-1", pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = session

	original, cancelOriginal := newAgentTelemetryStream(agentID, session.SessionID)
	defer cancelOriginal()
	originalErr := make(chan error, 1)
	go func() { originalErr <- relay.TelemetryStream(original) }()
	waitForStreamGeneration(t, session, 1)

	replacement, cancelReplacement := newAgentTelemetryStream(agentID, session.SessionID)
	defer cancelReplacement()
	if err := relay.TelemetryStream(replacement); status.Code(err) != codes.Unavailable {
		t.Fatalf("replacement TelemetryStream() error = %v, want Unavailable", err)
	}

	relay.sessionsMu.RLock()
	currentSession := relay.grpcSessions[agentID]
	relay.sessionsMu.RUnlock()
	session.sessionMu.RLock()
	currentStream := session.stream
	session.sessionMu.RUnlock()
	if currentSession != session || currentStream == nil || currentStream.stream != original {
		t.Fatal("failed replacement did not restore the previously accepted stream")
	}
	if _, stopped := reporter.counts(); stopped != 0 {
		t.Fatalf("failed replacement stopped prior liveness %d times, want 0", stopped)
	}

	original.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: session.SessionID, Seq: 51,
		MsgName: "Heartbeat", WalId: testWALGenerationID, SentAtUnixNs: time.Now().UnixNano(),
	})
	select {
	case message := <-original.sentAckChan:
		if ack := message.GetTelemetryAck(); ack == nil || ack.Seq != 51 || ack.Status != agentv1.TelemetryAck_STATUS_OK {
			t.Fatalf("original stream ACK = %#v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("restored original stream did not remain active")
	}

	close(original.recvChan)
	if err := <-originalErr; err != nil {
		t.Fatalf("original stream close error = %v", err)
	}
	if _, stopped := reporter.counts(); stopped != 1 {
		t.Fatalf("accepted stream cleanup stopped liveness %d times, want 1", stopped)
	}
}

func TestPendingRegistryReplacementDoesNotBlockAcceptedStreamACK(t *testing.T) {
	reporter := &blockingReplacementAgentRegistrar{
		registerStarted: make(chan struct{}, 1),
		releaseRegister: make(chan struct{}),
	}
	relay := relayWithRegistryReporter(t, reporter)
	const agentID = "agent-1"
	session := &DroneSession{
		agentID: agentID, SessionID: "session-1", pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = session

	original, cancelOriginal := newAgentTelemetryStream(agentID, session.SessionID)
	defer cancelOriginal()
	originalErr := make(chan error, 1)
	go func() { originalErr <- relay.TelemetryStream(original) }()
	waitForStreamGeneration(t, session, 1)

	replacement, cancelReplacement := newAgentTelemetryStream(agentID, session.SessionID)
	defer cancelReplacement()
	replacementErr := make(chan error, 1)
	go func() { replacementErr <- relay.TelemetryStream(replacement) }()
	select {
	case <-reporter.registerStarted:
	case <-time.After(time.Second):
		t.Fatal("replacement registry publication did not start")
	}

	original.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: session.SessionID, Seq: 52,
		MsgName: "Heartbeat", WalId: testWALGenerationID, SentAtUnixNs: time.Now().UnixNano(),
	})
	select {
	case message := <-original.sentAckChan:
		if ack := message.GetTelemetryAck(); ack == nil || ack.Seq != 52 || ack.Status != agentv1.TelemetryAck_STATUS_OK {
			t.Fatalf("original stream ACK = %#v", ack)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("registry publication blocked the accepted stream ACK")
	}

	close(reporter.releaseRegister)
	close(replacement.recvChan)
	if err := <-replacementErr; err != nil {
		t.Fatalf("replacement stream close error = %v", err)
	}
	close(original.recvChan)
	if err := <-originalErr; err != nil {
		t.Fatalf("original stream close error = %v", err)
	}
}

func TestSupersededPendingPublicationCannotReplaceAcceptedStream(t *testing.T) {
	reporter := &blockingRegisterAgentRegistrar{
		registerStarted: make(chan struct{}, 1),
		releaseRegister: make(chan struct{}),
	}
	relay := relayWithRegistryReporter(t, reporter)
	const agentID = "agent-1"
	original := &telemetryStreamBinding{generation: 1}
	session := &DroneSession{
		agentID: agentID, SessionID: "session-1", stream: original, streamGeneration: 1,
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = session

	_, first, _, err := relay.updateStream(agentID, session.SessionID, &mockTelemetryStream{})
	if err != nil {
		t.Fatal(err)
	}
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- relay.registerActiveAgent(context.Background(), agentID, session, first, original)
	}()
	<-reporter.registerStarted

	_, second, _, err := relay.updateStream(agentID, session.SessionID, &mockTelemetryStream{})
	if err != nil {
		t.Fatal(err)
	}
	close(reporter.releaseRegister)
	if err := <-firstResult; !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("superseded publication error = %v, want ErrSessionNotFound", err)
	}
	relay.deleteStream(agentID, session, first)

	if err := relay.registerActiveAgent(context.Background(), agentID, session, second, original); err != nil {
		t.Fatalf("latest publication: %v", err)
	}
	session.sessionMu.RLock()
	current := session.stream
	session.sessionMu.RUnlock()
	if current != second || first.closed == false || original.closed {
		t.Fatalf("stream state after supersession: current=%p firstClosed=%t originalClosed=%t", current, first.closed, original.closed)
	}
}

func TestFailedReplacementDoesNotResurrectClosedStream(t *testing.T) {
	reporter := &recordingAgentRegistrar{err: errors.New("registry unavailable")}
	relay := relayWithRegistryReporter(t, reporter)
	const agentID = "agent-1"
	original := &telemetryStreamBinding{generation: 1, closed: true}
	session := &DroneSession{
		agentID: agentID, SessionID: "session-1", stream: original, streamGeneration: 1,
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions[agentID] = session
	replacement := &telemetryStreamBinding{generation: 2}
	session.stream = replacement
	session.streamGeneration = 2

	if err := relay.registerActiveAgent(context.Background(), agentID, session, replacement, original); err == nil {
		t.Fatal("registerActiveAgent() unexpectedly succeeded")
	}

	session.sessionMu.RLock()
	current := session.stream
	session.sessionMu.RUnlock()
	if current != replacement {
		t.Fatal("failed replacement resurrected an already closed predecessor")
	}
}

func TestTelemetryStreamRejectsUnboundSessionBeforeRegistryPublication(t *testing.T) {
	reporter := &recordingAgentRegistrar{}
	relay := relayWithRegistryReporter(t, reporter)
	relay.grpcSessions["agent-1"] = &DroneSession{
		agentID: "agent-1", SessionID: "current-session", pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	stream, cancel := newAgentTelemetryStream("agent-1", "stolen-session")
	defer cancel()

	err := relay.TelemetryStream(stream)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("TelemetryStream() error = %v, want Unauthenticated", err)
	}
	if reporter.agentID != "" {
		t.Fatalf("unbound session published agent %q", reporter.agentID)
	}
	if _, ok := relay.grpcSessions["agent-1"]; !ok {
		t.Fatal("unbound stream removed the legitimate session")
	}
}

func TestRegistryPublicationRejectsReplacedSession(t *testing.T) {
	reporter := &recordingAgentRegistrar{}
	binding := &telemetryStreamBinding{generation: 1}
	oldSession := &DroneSession{
		agentID: "agent-1", SessionID: "old-session", stream: binding, streamGeneration: 1,
		pending: make(map[string]chan *agentv1.OperationContextCommandAck), retired: true,
	}
	relay := &Relay{
		grpcSessions: map[string]*DroneSession{
			"agent-1": {agentID: "agent-1", SessionID: "replacement-session"},
		},
		registryReporter: reporter,
	}

	err := relay.registerActiveAgent(context.Background(), "agent-1", oldSession, binding, nil)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("registerActiveAgent() error = %v, want ErrSessionNotFound", err)
	}
	if reporter.agentID != "" {
		t.Fatalf("replaced session published agent %q", reporter.agentID)
	}
}

func TestRegistryPublicationLinearizesBeforeSessionReplacement(t *testing.T) {
	reporter := &blockingRegisterAgentRegistrar{
		registerStarted: make(chan struct{}, 1),
		releaseRegister: make(chan struct{}),
	}
	relay := relayWithRegistryReporter(t, reporter)
	binding := &telemetryStreamBinding{generation: 1}
	session := &DroneSession{
		agentID: "agent-1", SessionID: "old-session", stream: binding, streamGeneration: 1,
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions["agent-1"] = session

	published := make(chan error, 1)
	go func() {
		published <- relay.registerActiveAgent(context.Background(), "agent-1", session, binding, nil)
	}()
	<-reporter.registerStarted
	replaced := make(chan error, 1)
	go func() {
		_, err := relay.Register(
			authenticatedAgentContext("agent-1"),
			&agentv1.RegisterRequest{AgentId: "agent-1"},
		)
		replaced <- err
	}()
	select {
	case err := <-replaced:
		t.Fatalf("replacement completed before publication linearized: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(reporter.releaseRegister)
	if err := <-published; err != nil {
		t.Fatalf("registerActiveAgent() error = %v", err)
	}
	if err := <-replaced; err != nil {
		t.Fatalf("replacement Register() error = %v", err)
	}
}

func TestStreamCleanupStopsOldLivenessBeforePublishingReplacementSession(t *testing.T) {
	reporter := &blockingStopAgentRegistrar{stopStarted: make(chan struct{}), releaseStop: make(chan struct{})}
	const agentID = "agent-1"
	binding := &telemetryStreamBinding{generation: 1}
	session := &DroneSession{
		agentID: agentID, SessionID: "old-session", stream: binding, streamGeneration: 1,
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay := relayWithRegistryReporter(t, reporter)
	relay.grpcSessions[agentID] = session
	cleanupDone := make(chan struct{})
	go func() {
		relay.deleteStream(agentID, session, binding)
		close(cleanupDone)
	}()
	<-reporter.stopStarted

	registerDone := make(chan error, 1)
	go func() {
		_, err := relay.Register(authenticatedAgentContext(agentID), &agentv1.RegisterRequest{AgentId: agentID})
		registerDone <- err
	}()
	select {
	case err := <-registerDone:
		t.Fatalf("replacement registration completed before old liveness stopped: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(reporter.releaseStop)
	<-cleanupDone
	if err := <-registerDone; err != nil {
		t.Fatalf("replacement registration failed: %v", err)
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

			stream, cancel := newAgentTelemetryStream(agentID, "admission-session")
			defer cancel()
			errChannel := make(chan error, 1)
			go func() {
				errChannel <- relay.TelemetryStream(stream)
			}()
			waitForStreamGeneration(t, session, 1)

			stream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
				AgentId: agentID, SessionId: session.SessionID, Seq: 44, MsgName: "Heartbeat", WalId: testWALGenerationID, SentAtUnixNs: time.Now().UnixNano(),
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

func TestTelemetryStreamValidatesWALGenerationIDBeforeRouting(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		walID         string
		wantStatus    agentv1.TelemetryAck_Status
		wantError     string
		wantSinkCount int
		wantWALID     string
	}{
		{name: "unsupported message missing ID", path: "unsupported", wantStatus: agentv1.TelemetryAck_STATUS_PERMANENT_ERROR, wantError: "required"},
		{name: "unsupported message invalid ID", path: "unsupported", walID: "not-a-uuid", wantStatus: agentv1.TelemetryAck_STATUS_PERMANENT_ERROR, wantError: "invalid"},
		{name: "unsupported message nil ID", path: "unsupported", walID: nilWALGenerationID, wantStatus: agentv1.TelemetryAck_STATUS_PERMANENT_ERROR, wantError: "invalid"},
		{name: "unsupported message valid ID", path: "unsupported", walID: testWALGenerationID, wantStatus: agentv1.TelemetryAck_STATUS_OK, wantSinkCount: 1, wantWALID: testWALGenerationID},
		{name: "unsupported message uppercase ID", path: "unsupported", walID: strings.ToUpper(testWALGenerationID), wantStatus: agentv1.TelemetryAck_STATUS_OK, wantSinkCount: 1, wantWALID: testWALGenerationID},
		{name: "noop missing ID", path: "noop", wantStatus: agentv1.TelemetryAck_STATUS_PERMANENT_ERROR, wantError: "required"},
		{name: "noop invalid ID", path: "noop", walID: "not-a-uuid", wantStatus: agentv1.TelemetryAck_STATUS_PERMANENT_ERROR, wantError: "invalid"},
		{name: "noop nil ID", path: "noop", walID: nilWALGenerationID, wantStatus: agentv1.TelemetryAck_STATUS_PERMANENT_ERROR, wantError: "invalid"},
		{name: "noop valid ID", path: "noop", walID: testWALGenerationID, wantStatus: agentv1.TelemetryAck_STATUS_OK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var genericSink *mock.MockSink
			var relay *Relay
			messageName := "Heartbeat"
			if tt.path == "unsupported" {
				genericSink = mock.NewMockSink()
				relay = relayWithSinks(genericSink)
				messageName = "Attitude"
			} else {
				relay = &Relay{router: outputs.NewRouter()}
				relay.router.AddConsumer(telemetrywriter.NewNoopWriter(), telemetryMessageFilter())
			}

			const agentID = "wal-admission-agent"
			session := &DroneSession{
				agentID: agentID, SessionID: "wal-admission-session",
				pending: make(map[string]chan *agentv1.OperationContextCommandAck),
			}
			relay.grpcSessions = map[string]*DroneSession{agentID: session}
			stream, cancel := newAgentTelemetryStream(agentID, session.SessionID)
			defer cancel()
			streamErr := make(chan error, 1)
			go func() { streamErr <- relay.TelemetryStream(stream) }()
			waitForStreamGeneration(t, session, 1)

			stream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
				AgentId: agentID, SessionId: session.SessionID, Seq: 71,
				MsgName: messageName, WalId: tt.walID, SentAtUnixNs: time.Now().UnixNano(),
			})
			select {
			case message := <-stream.sentAckChan:
				ack := message.GetTelemetryAck()
				if ack == nil {
					t.Fatal("stream response did not contain a telemetry ACK")
				}
				if ack.Status != tt.wantStatus {
					t.Fatalf("ACK status = %v, want %v; error=%q", ack.Status, tt.wantStatus, ack.Error)
				}
				if tt.wantError != "" && !strings.Contains(ack.Error, tt.wantError) {
					t.Fatalf("ACK error = %q, want substring %q", ack.Error, tt.wantError)
				}
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for telemetry ACK")
			}

			if genericSink != nil && genericSink.GetMessageCount() != tt.wantSinkCount {
				t.Fatalf("generic sink count = %d, want %d", genericSink.GetMessageCount(), tt.wantSinkCount)
			}
			if genericSink != nil && tt.wantWALID != "" {
				if got := genericSink.GetMessages()[0].WALID; got != tt.wantWALID {
					t.Fatalf("routed WAL generation ID = %q, want %q", got, tt.wantWALID)
				}
			}
			close(stream.recvChan)
			select {
			case err := <-streamErr:
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
	paddedAgentID := "  " + agentID + "  "
	relay.grpcSessions[agentID] = &DroneSession{
		agentID:   agentID,
		SessionID: "session-stream-test",
		pending:   make(map[string]chan *agentv1.OperationContextCommandAck),
	}

	// Setup Mock Stream
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"aero-arc-agent-id", paddedAgentID,
			"aero-arc-session-id", "session-stream-test",
		),
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
		AgentId:      paddedAgentID,
		SessionId:    "session-stream-test",
		MsgName:      "Heartbeat",
		WalId:        testWALGenerationID,
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
		if msg.AgentID != agentID {
			t.Errorf("Expected canonical AgentID %s, got %s", agentID, msg.AgentID)
		}
	}

	// Test Case 3: Reject a named frame without its durable capture timestamp.
	missingTimestampFrame := &agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: "session-stream-test", Seq: 3, MsgName: "Heartbeat", WalId: testWALGenerationID,
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
		metadata.Pairs(
			"aero-arc-agent-id", agentID,
			"aero-arc-session-id", oldSession.SessionID,
		),
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
		WalId:        testWALGenerationID,
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

	oldStream, cancelOld := newAgentTelemetryStream(agentID, "shared-session")
	defer cancelOld()
	oldErr := make(chan error, 1)
	go func() {
		oldErr <- relay.TelemetryStream(oldStream)
	}()
	waitForStreamGeneration(t, session, 1)

	replacementStream, cancelReplacement := newAgentTelemetryStream(agentID, "shared-session")
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
		WalId:        testWALGenerationID,
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

	oldStream, cancelOld := newAgentTelemetryStream(agentID, "shared-session")
	defer cancelOld()
	oldStream.sendStarted = make(chan struct{}, 1)
	oldStream.sendBlock = make(chan struct{})
	oldErr := make(chan error, 1)
	go func() {
		oldErr <- relay.TelemetryStream(oldStream)
	}()
	waitForStreamGeneration(t, session, 1)

	oldStream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: session.SessionID, Seq: 45, MsgName: "Heartbeat", WalId: testWALGenerationID, SentAtUnixNs: time.Now().UnixNano(),
	})
	select {
	case <-oldStream.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("old stream did not block while sending its ACK")
	}

	replacementStream, cancelReplacement := newAgentTelemetryStream(agentID, "shared-session")
	defer cancelReplacement()
	replacementErr := make(chan error, 1)
	go func() {
		replacementErr <- relay.TelemetryStream(replacementStream)
	}()
	waitForStreamGeneration(t, session, 2)

	replacementStream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: session.SessionID, Seq: 46, MsgName: "Heartbeat", WalId: testWALGenerationID, SentAtUnixNs: time.Now().UnixNano(),
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

	stream, cancel := newAgentTelemetryStream(agentID, "admission-owner-session")
	defer cancel()
	streamErr := make(chan error, 1)
	go func() {
		streamErr <- relay.TelemetryStream(stream)
	}()
	waitForStreamGeneration(t, session, 1)

	stream.recvChan <- telemetryStreamMessage(&agentv1.TelemetryFrame{
		AgentId: agentID, SessionId: session.SessionID, Seq: 47,
		MsgName: "Heartbeat", WalId: testWALGenerationID, SentAtUnixNs: time.Now().UnixNano(),
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

	oldStream, cancelOld := newAgentTelemetryStream(agentID, "shared-session")
	defer cancelOld()
	oldErr := make(chan error, 1)
	go func() {
		oldErr <- relay.TelemetryStream(oldStream)
	}()
	waitForStreamGeneration(t, session, 1)

	replacementStream, cancelReplacement := newAgentTelemetryStream(agentID, "shared-session")
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
	oldState := &operationCommandState{
		expected: &agentv1.OperationContext{
			FlightId: "old-flight", IntentId: "old-intent", IntentVersion: 7,
		},
		done: make(chan struct{}),
	}
	oldSession := &DroneSession{
		agentID:   agentID,
		SessionID: "old-session",
		pending: map[string]chan *agentv1.OperationContextCommandAck{
			"shared-command": oldPending,
		},
		operationCommands: map[string]*operationCommandState{
			"shared-command": oldState,
		},
	}
	relay.grpcSessions[agentID] = oldSession

	oldStream, cancelOld := newAgentTelemetryStream(agentID, "old-session")
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
	select {
	case <-oldState.done:
		if status.Code(oldState.err) != codes.Aborted {
			t.Fatalf("old pending command error = %v, want Aborted", oldState.err)
		}
	default:
		t.Fatal("replacement did not abort the old pending command")
	}
	replacementPending := make(chan *agentv1.OperationContextCommandAck, 1)
	replacementSession.pendingMu.Lock()
	replacementSession.pending["shared-command"] = replacementPending
	replacementSession.operationCommands["shared-command"] = &operationCommandState{
		expected: &agentv1.OperationContext{
			FlightId: "replacement-flight", IntentId: "replacement-intent", IntentVersion: 8,
		},
		done: make(chan struct{}),
	}
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
		t.Fatalf("retired pending command received late ACK %#v", got)
	case <-replacementPending:
		t.Fatal("replacement session received a command ACK from the old stream")
	case <-time.After(20 * time.Millisecond):
	}

	oldSession.sessionMu.RLock()
	oldFlightID := oldSession.FlightID
	oldIntentID := oldSession.IntentID
	oldIntentVersion := oldSession.IntentVersion
	oldSession.sessionMu.RUnlock()
	if oldFlightID != "" || oldIntentID != "" || oldIntentVersion != 0 {
		t.Fatalf("late ACK changed retired session context to (%q, %q, %d)", oldFlightID, oldIntentID, oldIntentVersion)
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

func TestRegisterReplacementRestoresAcknowledgedOperationContext(t *testing.T) {
	relay := relayWithSinks(mock.NewMockSink())
	relay.grpcSessions = make(map[string]*DroneSession)
	old := &DroneSession{
		agentID: "agent-1", SessionID: "old-session",
		FlightID: "flight-1", IntentID: "intent-1", IntentVersion: 7,
		pending: make(map[string]chan *agentv1.OperationContextCommandAck),
	}
	relay.grpcSessions["agent-1"] = old
	if _, err := relay.Register(context.Background(), &agentv1.RegisterRequest{AgentId: "agent-1"}); err != nil {
		t.Fatal(err)
	}
	relay.sessionsMu.RLock()
	replacement := relay.grpcSessions["agent-1"]
	relay.sessionsMu.RUnlock()
	if replacement == old {
		t.Fatal("registration did not replace the old session")
	}
	if got := droneStatus(replacement); got.GetFlightId() != "flight-1" || got.GetIntentId() != "intent-1" || got.GetIntentVersion() != 7 {
		t.Fatalf("replacement context = %#v, want flight-1/intent-1/7", got)
	}
}

func TestOperationContextCommandACKRequiresPendingCommand(t *testing.T) {
	session := &DroneSession{
		FlightID:      "existing-flight",
		IntentID:      "existing-intent",
		IntentVersion: 3,
		pending:       make(map[string]chan *agentv1.OperationContextCommandAck),
	}

	session.handleOperationContextCommandAck(&agentv1.OperationContextCommandAck{
		CommandId: "unsolicited-command",
		Status:    agentv1.OperationContextCommandAck_STATUS_APPLIED,
		ActiveContext: &agentv1.OperationContext{
			FlightId: "attacker-flight", IntentId: "attacker-intent", IntentVersion: 99,
		},
	})

	session.sessionMu.RLock()
	defer session.sessionMu.RUnlock()
	if session.FlightID != "existing-flight" ||
		session.IntentID != "existing-intent" ||
		session.IntentVersion != 3 {
		t.Fatalf(
			"unsolicited ACK changed context to (%q, %q, %d)",
			session.FlightID, session.IntentID, session.IntentVersion,
		)
	}
}

func newAgentTelemetryStream(agentID, sessionID string) (*mockTelemetryStream, context.CancelFunc) {
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"aero-arc-agent-id", agentID,
			"aero-arc-session-id", sessionID,
			"authorization", bearerPrefix+testAgentToken,
		),
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
		metadata.Pairs(
			"aero-arc-agent-id", agentID,
			"aero-arc-session-id", "missing-session",
		),
	)

	stream := &mockTelemetryStream{
		ctx: ctx,
	}

	err := relay.TelemetryStream(stream)
	if err == nil {
		t.Error("Expected error for unregistered agent")
	}
}
