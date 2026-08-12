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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/mock"
	"github.com/makinje/aero-arc-relay/internal/outputs"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
)

func relayWithSinks(testSinks ...sinks.Sink) *Relay {
	router := outputs.NewRouter()
	for i, sink := range testSinks {
		router.AddConsumer(
			outputs.NewSinkConsumer(fmt.Sprintf("test-%d", i), sink),
			outputs.MessageFilter{Include: []string{"*"}},
		)
	}
	return &Relay{sinks: testSinks, router: router}
}

func sinkErrorMetricValue(t *testing.T, sink string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "aero_relay_sink_errors_total" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "sink" && label.GetValue() == sink {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestRelayCreation(t *testing.T) {
	cfg := &config.Config{
		Sinks:   config.SinksConfig{},
		Logging: config.LoggingConfig{Level: "info", Format: "text"},
	}

	_, err := New(cfg)
	if err == nil {
		t.Error("Expected error when no sinks are configured")
	}

	tempDir := t.TempDir()
	cfg.Sinks.File = &config.FileConfig{
		Path:             tempDir,
		Prefix:           "telemetry",
		Format:           "json",
		RotationInterval: time.Hour,
		MessageFilterConfig: config.MessageFilterConfig{
			IncludeMessages: []string{"*"},
		},
	}

	relay, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create relay: %v", err)
	}
	for _, sink := range relay.sinks {
		_ = sink.Close(context.Background())
	}
}

func TestRelayCreationWithOnlyInternalOutput(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
	}{
		{
			name: "registry",
			config: &config.Config{
				Registry: config.RegistryConfig{Enabled: true},
			},
		},
		{
			name: "telemetry",
			config: &config.Config{
				Telemetry: config.TelemetryConfig{Enabled: true, Backend: "noop"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relay := &Relay{config: tt.config}
			reporter := &closeTrackingConsumer{}
			err := relay.initializeOutputsWith(
				func() (outputs.EnvelopeConsumer, error) { return reporter, nil },
				func() (outputs.EnvelopeConsumer, error) { return reporter, nil },
				func() error { return nil },
			)
			if err != nil {
				t.Fatalf("New() with only %s output: %v", tt.name, err)
			}
			if !relay.ready() {
				t.Fatalf("relay with only %s output is not ready", tt.name)
			}
			if len(relay.sinks) != 0 {
				t.Fatalf("relay with only %s output has %d generic sinks", tt.name, len(relay.sinks))
			}
		})
	}
}

func TestInitializeOutputsClosesConsumersWhenSinkInitializationFails(t *testing.T) {
	consumer := &closeTrackingConsumer{}
	sinkErr := errors.New("sink initialization failed")
	relay := &Relay{
		config: &config.Config{
			Telemetry: config.TelemetryConfig{Enabled: true},
		},
	}

	err := relay.initializeOutputsWith(
		func() (outputs.EnvelopeConsumer, error) { return nil, errors.New("unused") },
		func() (outputs.EnvelopeConsumer, error) { return consumer, nil },
		func() error { return sinkErr },
	)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("initializeOutputsWith() error = %v, want %v", err, sinkErr)
	}
	if !consumer.closed {
		t.Fatal("initialized telemetry consumer was not closed")
	}
}

func TestRelayCloseIsIdempotentAndRetainsFirstError(t *testing.T) {
	closeErr := errors.New("close failed")
	consumer := &closeTrackingConsumer{closeErr: closeErr}
	router := outputs.NewRouter()
	router.AddConsumer(consumer, outputs.MessageFilter{Include: []string{"*"}})
	relay := &Relay{router: router}

	if err := relay.Close(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("first Close() error = %v, want %v", err, closeErr)
	}
	if err := relay.Close(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("second Close() error = %v, want retained %v", err, closeErr)
	}
	if consumer.closeCalls != 1 {
		t.Fatalf("consumer Close() calls = %d, want 1", consumer.closeCalls)
	}
}

func TestRelayStartBindFailureDoesNotPublishAndClosesOutputs(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.Addr().(*net.TCPAddr).Port
	reporter := &closeTrackingConsumer{}
	relay := relayForStartTest(&config.Config{GrpcPort: port}, reporter)

	err = relay.Start(context.Background())
	if !errors.Is(err, ErrCreatingTCPListener) {
		t.Fatalf("Start() error = %v, want ErrCreatingTCPListener", err)
	}
	if reporter.startCalls != 0 {
		t.Fatalf("registry publication calls = %d, want 0", reporter.startCalls)
	}
	if reporter.closeCalls != 1 {
		t.Fatalf("reporter close calls = %d, want 1", reporter.closeCalls)
	}
}

func TestRelayStartTLSFailureDoesNotPublishAndClosesListener(t *testing.T) {
	port := unusedTCPPort(t)
	reporter := &closeTrackingConsumer{}
	relay := relayForStartTest(&config.Config{
		GrpcPort: port, TLSCertPath: filepath.Join(t.TempDir(), "missing.crt"), TLSKeyPath: filepath.Join(t.TempDir(), "missing.key"),
	}, reporter)

	err := relay.Start(context.Background())
	if !errors.Is(err, ErrCreatingTLSCredentials) {
		t.Fatalf("Start() error = %v, want ErrCreatingTLSCredentials", err)
	}
	if reporter.startCalls != 0 {
		t.Fatalf("registry publication calls = %d, want 0", reporter.startCalls)
	}
	if reporter.closeCalls != 1 {
		t.Fatalf("reporter close calls = %d, want 1", reporter.closeCalls)
	}
	listener, listenErr := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if listenErr != nil {
		t.Fatalf("startup failure leaked listener: %v", listenErr)
	}
	_ = listener.Close()
}

func TestRelayStartPublishesOnlyAfterListenerAndTLSAreReady(t *testing.T) {
	port := unusedTCPPort(t)
	certPath, keyPath := writeTestCertificate(t)
	ctx, cancel := context.WithCancel(context.Background())
	reporter := &closeTrackingConsumer{}
	reporter.onStart = func() error {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			_ = listener.Close()
			return errors.New("relay listener was not bound before registry publication")
		}
		cancel()
		return nil
	}
	relay := relayForStartTest(&config.Config{
		GrpcPort: port, TLSCertPath: certPath, TLSKeyPath: keyPath,
	}, reporter)

	if err := relay.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if reporter.startCalls != 1 {
		t.Fatalf("registry publication calls = %d, want 1", reporter.startCalls)
	}
	if reporter.closeCalls != 1 {
		t.Fatalf("reporter close calls = %d, want 1", reporter.closeCalls)
	}
}

func TestRelayStartRegistryFailureClosesReporterAndListener(t *testing.T) {
	port := unusedTCPPort(t)
	certPath, keyPath := writeTestCertificate(t)
	reporter := &closeTrackingConsumer{startErr: errors.New("registry unavailable")}
	relay := relayForStartTest(&config.Config{
		GrpcPort: port, TLSCertPath: certPath, TLSKeyPath: keyPath,
	}, reporter)

	err := relay.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "registry unavailable") {
		t.Fatalf("Start() error = %v, want registry failure", err)
	}
	if reporter.startCalls != 1 || reporter.closeCalls != 1 {
		t.Fatalf("reporter lifecycle starts=%d closes=%d, want 1/1", reporter.startCalls, reporter.closeCalls)
	}
	listener, listenErr := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if listenErr != nil {
		t.Fatalf("registry failure leaked listener: %v", listenErr)
	}
	_ = listener.Close()
}

func relayForStartTest(cfg *config.Config, reporter *closeTrackingConsumer) *Relay {
	router := outputs.NewRouter()
	router.AddConsumer(reporter, outputs.MessageFilter{Include: []string{"*"}})
	return &Relay{
		config: cfg, router: router, grpcSessions: make(map[string]*DroneSession),
		registryReporter: reporter, registryStarter: reporter, outputsInitialized: true,
	}
}

func unusedTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func writeTestCertificate(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "relay.crt")
	keyPath := filepath.Join(dir, "relay.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestNormalizedTelemetryRequiresRelayID(t *testing.T) {
	cfg := &config.Config{
		Telemetry: config.TelemetryConfig{
			Enabled: true,
			Backend: "influxdb3",
			InfluxDB: &config.NormalizedInfluxDBConfig{
				Host: "http://localhost:8181", Token: "token", Database: "telemetry",
			},
		},
	}
	if _, err := New(cfg); err == nil {
		t.Fatal("New() accepted normalized telemetry without a relay ID")
	}
}

func TestInternalOutputMessageFilters(t *testing.T) {
	tests := []struct {
		name     string
		filter   outputs.MessageFilter
		included []string
		excluded string
	}{
		{
			name:     "registry",
			filter:   registryMessageFilter(),
			included: []string{"Heartbeat", "GlobalPositionInt"},
			excluded: "Attitude",
		},
		{
			name:     "telemetry",
			filter:   telemetryMessageFilter(),
			included: []string{"Heartbeat", "GlobalPositionInt", "BatteryStatus", "SysStatus", "VFR_HUD", "ExtendedSysState", "GpsRawInt", "SystemTime"},
			excluded: "Attitude",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, message := range tt.included {
				if !tt.filter.Allows(message) {
					t.Errorf("internal %s filter rejects required message %q", tt.name, message)
				}
			}
			if tt.filter.Allows(tt.excluded) {
				t.Errorf("internal %s filter allows unrelated message %q", tt.name, tt.excluded)
			}
		})
	}
}

func TestHandleTelemetryMessage(t *testing.T) {
	mockSink := mock.NewMockSink()
	relay := relayWithSinks(mockSink)

	msg := telemetry.TelemetryEnvelope{
		AgentID:        "test-agent",
		Source:         "test-agent",
		TimestampRelay: time.Now().UTC(),
		MsgName:        "Heartbeat",
		Fields: map[string]any{
			"type": "AUTO",
		},
	}

	if err := relay.handleTelemetryMessage(context.Background(), msg); err != nil {
		t.Fatalf("handleTelemetryMessage() error = %v", err)
	}

	if mockSink.GetMessageCount() != 1 {
		t.Fatalf("Expected 1 message, got %d", mockSink.GetMessageCount())
	}

	received := mockSink.GetMessages()[0]
	if received.AgentID != "test-agent" {
		t.Errorf("Expected AgentID 'test-agent', got '%s'", received.AgentID)
	}
	if received.MsgName != "Heartbeat" {
		t.Errorf("Expected MsgName 'Heartbeat', got '%s'", received.MsgName)
	}
}

func TestHandleTelemetryMessageMultipleSinks(t *testing.T) {
	relay := relayWithSinks(mock.NewMockSink(), mock.NewMockSink())

	msg := telemetry.TelemetryEnvelope{
		AgentID:        "test-agent",
		Source:         "test-agent",
		TimestampRelay: time.Now().UTC(),
		MsgName:        "Status",
	}

	if err := relay.handleTelemetryMessage(context.Background(), msg); err != nil {
		t.Fatalf("handleTelemetryMessage() error = %v", err)
	}

	for i, sink := range relay.sinks {
		mockSink := sink.(*mock.MockSink)
		if mockSink.GetMessageCount() != 1 {
			t.Errorf("Sink %d: Expected 1 message, got %d", i, mockSink.GetMessageCount())
		}
	}
}

func TestBuildTelemetryFrameEnvelope(t *testing.T) {
	relay := &Relay{}
	session := &DroneSession{
		agentID:       "agent-1",
		SessionID:     "session-1",
		FlightID:      "authoritative-flight",
		IntentID:      "authoritative-intent",
		IntentVersion: 4,
	}

	before := time.Now().UTC()
	agentTime := time.Date(2026, 7, 12, 12, 30, 0, 123, time.UTC)
	frame := &agentv1.TelemetryFrame{
		AgentId:            "agent-1",
		SessionId:          "session-1",
		FlightId:           "stale-frame-flight",
		IntentId:           "stale-frame-intent",
		IntentVersion:      1,
		Seq:                99,
		SentAtUnixNs:       agentTime.UnixNano(),
		DeviceTimestampSec: 42.5,
		Dialect:            "common",
		MsgId:              42,
		MsgName:            "Status",
		Fields: map[string]string{
			"mode": "AUTO",
		},
	}

	envelope := relay.buildTelemetryFrameEnvelope(session, frame)
	after := time.Now().UTC()

	if envelope.AgentID != "agent-1" {
		t.Errorf("Expected AgentID 'agent-1', got '%s'", envelope.AgentID)
	}
	if envelope.MsgID != 42 {
		t.Errorf("Expected MsgID 42, got %d", envelope.MsgID)
	}
	if envelope.MsgName != "Status" {
		t.Errorf("Expected MsgName 'Status', got '%s'", envelope.MsgName)
	}
	if envelope.SessionID != "session-1" || envelope.FlightID != "authoritative-flight" {
		t.Errorf("session/flight metadata = %q/%q", envelope.SessionID, envelope.FlightID)
	}
	if envelope.IntentID != "authoritative-intent" || envelope.IntentVersion != 4 {
		t.Errorf("intent metadata = %q/%d", envelope.IntentID, envelope.IntentVersion)
	}
	if envelope.WALSequence != 99 || envelope.Dialect != "common" {
		t.Errorf("sequence/dialect metadata = %d/%q", envelope.WALSequence, envelope.Dialect)
	}
	if !envelope.TimestampAgent.Equal(agentTime) || envelope.TimestampDevice != 42.5 {
		t.Errorf("agent/device timestamps = %v/%v", envelope.TimestampAgent, envelope.TimestampDevice)
	}
	if got := envelope.Fields["mode"]; got != "AUTO" {
		t.Errorf("Expected field 'mode' to be 'AUTO', got '%v'", got)
	}
	if envelope.TimestampRelay.Before(before) || envelope.TimestampRelay.After(after) {
		t.Errorf("TimestampRelay %v not within expected range", envelope.TimestampRelay)
	}
	if len(envelope.Raw) == 0 {
		t.Error("Expected Raw payload to be set")
	}
}

func TestHandleTelemetryFrame(t *testing.T) {
	mockSink := mock.NewMockSink()
	relay := relayWithSinks(mockSink)
	session := &DroneSession{agentID: "agent-2", SessionID: "session-2"}

	frame := &agentv1.TelemetryFrame{
		AgentId: "agent-2",
		MsgId:   7,
		MsgName: "Heartbeat",
		Fields: map[string]string{
			"type": "AUTO",
		},
	}

	if err := relay.handleTelemetryFrame(context.Background(), session, frame); err != nil {
		t.Fatalf("handleTelemetryFrame() error = %v", err)
	}

	if mockSink.GetMessageCount() != 1 {
		t.Fatalf("Expected 1 message, got %d", mockSink.GetMessageCount())
	}

	msg := mockSink.GetMessages()[0]
	if msg.AgentID != "agent-2" {
		t.Errorf("Expected AgentID 'agent-2', got '%s'", msg.AgentID)
	}
	if msg.MsgName != "Heartbeat" {
		t.Errorf("Expected MsgName 'Heartbeat', got '%s'", msg.MsgName)
	}
}

func TestConcurrentTelemetryHandling(t *testing.T) {
	relay := relayWithSinks(mock.NewMockSink())

	numMessages := 100
	var wg sync.WaitGroup
	wg.Add(numMessages)

	for i := 0; i < numMessages; i++ {
		go func(id int) {
			defer wg.Done()
			msg := telemetry.TelemetryEnvelope{
				AgentID:        "test-agent",
				Source:         "test-agent",
				TimestampRelay: time.Now().UTC(),
				MsgName:        fmt.Sprintf("Status-%d", id),
			}
			if err := relay.handleTelemetryMessage(context.Background(), msg); err != nil {
				t.Errorf("handleTelemetryMessage() error = %v", err)
			}
		}(i)
	}

	wg.Wait()

	mockSink := relay.sinks[0].(*mock.MockSink)
	if mockSink.GetMessageCount() != numMessages {
		t.Errorf("Expected %d messages, got %d", numMessages, mockSink.GetMessageCount())
	}
}

func TestRelayErrorHandling(t *testing.T) {
	failingSink := &failingSink{}
	relay := relayWithSinks(failingSink, mock.NewMockSink())
	errorsBefore := sinkErrorMetricValue(t, "test-0")

	msg := telemetry.TelemetryEnvelope{
		AgentID:        "test-agent",
		Source:         "test-agent",
		TimestampRelay: time.Now().UTC(),
		MsgName:        "Heartbeat",
	}

	if err := relay.handleTelemetryMessage(context.Background(), msg); err != nil {
		t.Fatalf("generic sink failure changed telemetry admission result: %v", err)
	}

	mockSink := relay.sinks[1].(*mock.MockSink)
	if mockSink.GetMessageCount() != 1 {
		t.Errorf("Expected 1 message in working sink, got %d", mockSink.GetMessageCount())
	}
	if got := sinkErrorMetricValue(t, "test-0"); got != errorsBefore+1 {
		t.Errorf("Expected sink error metric to increase by 1, got %v before and %v after", errorsBefore, got)
	}
}

type failingSink struct {
	closed bool
}

type closeTrackingConsumer struct {
	closed     bool
	closeCalls int
	closeErr   error
	startCalls int
	startErr   error
	onStart    func() error
}

func (c *closeTrackingConsumer) Name() string { return "close-tracking" }

func (c *closeTrackingConsumer) WriteEnvelope(context.Context, telemetry.TelemetryEnvelope) error {
	return nil
}

func (c *closeTrackingConsumer) RegisterAgent(context.Context, string) error { return nil }

func (c *closeTrackingConsumer) StopAgent(string) {}

func (c *closeTrackingConsumer) Start(context.Context) error {
	c.startCalls++
	if c.onStart != nil {
		if err := c.onStart(); err != nil {
			return err
		}
	}
	return c.startErr
}

func (c *closeTrackingConsumer) Close(context.Context) error {
	c.closed = true
	c.closeCalls++
	return c.closeErr
}

func (f *failingSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	if f.closed {
		return nil
	}
	return fmt.Errorf("simulated sink failure")
}

func (f *failingSink) Close(ctx context.Context) error {
	f.closed = true
	return nil
}
