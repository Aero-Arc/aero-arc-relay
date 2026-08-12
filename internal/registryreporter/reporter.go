/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at http://mozilla.org/MPL/2.0/.
*/

// Package registryreporter publishes relay and agent liveness to the Aero Arc
// registry control plane. It never forwards telemetry payloads.
package registryreporter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	registryv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/registry/v1"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const ConsumerName = "registry"

type Config struct {
	Address           string
	RelayID           string
	AdvertiseAddress  string
	RelayGRPCPort     int
	HeartbeatInterval time.Duration
	RequestTimeout    time.Duration
	TLSEnabled        bool
	TLSCAFile         string
	TLSServerName     string
}

type registryClient interface {
	RegisterRelay(context.Context, *registryv1.RegisterRelayRequest, ...grpc.CallOption) (*registryv1.RegisterRelayResponse, error)
	HeartbeatRelay(context.Context, *registryv1.HeartbeatRelayRequest, ...grpc.CallOption) (*registryv1.HeartbeatRelayResponse, error)
	RegisterAgent(context.Context, *registryv1.RegisterAgentRequest, ...grpc.CallOption) (*registryv1.RegisterAgentResponse, error)
	HeartbeatAgent(context.Context, *registryv1.HeartbeatAgentRequest, ...grpc.CallOption) (*registryv1.HeartbeatAgentResponse, error)
}

type Reporter struct {
	config Config
	client registryClient
	conn   *grpc.ClientConn

	workerCtx context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	mu                sync.Mutex
	started           bool
	activeAgents      map[string]*agentGeneration
	registeredAgents  map[string]*agentGeneration
	agentLastReported map[string]time.Time
	agentAdmissions   map[string]*agentAdmission
	now               func() time.Time
}

type agentGeneration struct {
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
}

type agentAdmission struct {
	cancel context.CancelFunc
}

func New(ctx context.Context, config Config) (*Reporter, error) {
	config = config.withDefaults()
	if err := config.validate(); err != nil {
		return nil, err
	}
	transportCredentials, err := credentialsFor(config)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(
		config.Address,
		grpc.WithTransportCredentials(transportCredentials),
	)
	if err != nil {
		return nil, fmt.Errorf("create registry client: %w", err)
	}
	dialCtx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	conn.Connect()
	if err := waitForReady(dialCtx, conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("connect to registry: %w", err)
	}
	reporter := newWithClient(config, registryv1.NewAeroRegistryClient(conn))
	reporter.conn = conn
	if err := reporter.registerRelay(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reporter.start()
	return reporter, nil
}

func waitForReady(ctx context.Context, conn *grpc.ClientConn) error {
	for {
		state := conn.GetState()
		switch state {
		case connectivity.Ready:
			return nil
		case connectivity.Shutdown:
			return errors.New("registry connection shut down before becoming ready")
		default:
			if !conn.WaitForStateChange(ctx, state) {
				return ctx.Err()
			}
		}
	}
}

func newWithClient(config Config, client registryClient) *Reporter {
	config = config.withDefaults()
	workerCtx, cancel := context.WithCancel(context.Background())
	return &Reporter{
		config:            config,
		client:            client,
		workerCtx:         workerCtx,
		cancel:            cancel,
		activeAgents:      make(map[string]*agentGeneration),
		registeredAgents:  make(map[string]*agentGeneration),
		agentLastReported: make(map[string]time.Time),
		agentAdmissions:   make(map[string]*agentAdmission),
		now:               time.Now,
	}
}

func (c Config) withDefaults() Config {
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 10 * time.Second
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 5 * time.Second
	}
	return c
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Address) == "" {
		return errors.New("registry address is required")
	}
	if strings.TrimSpace(c.RelayID) == "" {
		return errors.New("registry relay ID is required")
	}
	if strings.TrimSpace(c.AdvertiseAddress) == "" {
		return errors.New("registry relay advertise address is required")
	}
	if c.RelayGRPCPort <= 0 {
		return errors.New("registry relay gRPC port must be positive")
	}
	return nil
}

func credentialsFor(config Config) (credentials.TransportCredentials, error) {
	if !config.TLSEnabled {
		return insecure.NewCredentials(), nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.TLSServerName}
	if config.TLSCAFile != "" {
		pem, err := os.ReadFile(config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("read registry CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system certificate pool: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("registry CA file contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	return credentials.NewTLS(tlsConfig), nil
}

func (r *Reporter) Name() string { return ConsumerName }

// WriteEnvelope participates in output routing without putting Registry RPC
// latency on the telemetry ACK path. Per-Agent background workers own all
// liveness renewal, and telemetry payloads are never copied into Registry.
func (r *Reporter) WriteEnvelope(_ context.Context, envelope telemetry.TelemetryEnvelope) error {
	agentID := strings.TrimSpace(envelope.AgentID)
	if agentID == "" {
		return errors.New("registry heartbeat requires agent ID")
	}
	return nil
}

// RegisterAgent makes an agent visible while its telemetry stream is active.
func (r *Reporter) RegisterAgent(ctx context.Context, agentID string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return errors.New("registry registration requires agent ID")
	}
	generationCtx, cancel := context.WithCancel(r.workerCtx)
	generation := &agentGeneration{ctx: generationCtx, cancel: cancel}
	callCtx, cancelCall := context.WithTimeout(r.workerCtx, r.config.RequestTimeout)
	stopCallerCancellation := context.AfterFunc(ctx, cancelCall)
	defer stopCallerCancellation()
	admission := &agentAdmission{cancel: cancelCall}

	r.mu.Lock()
	previousAdmission := r.agentAdmissions[agentID]
	r.agentAdmissions[agentID] = admission
	r.mu.Unlock()
	if previousAdmission != nil {
		previousAdmission.cancel()
	}

	err := r.registerAgent(callCtx, agentID)
	cancelCall()
	if err != nil {
		r.mu.Lock()
		if r.agentAdmissions[agentID] == admission {
			delete(r.agentAdmissions, agentID)
		}
		r.mu.Unlock()
		generation.cancel()
		return fmt.Errorf("register agent %s with registry: %w", agentID, err)
	}

	r.mu.Lock()
	if r.agentAdmissions[agentID] != admission {
		r.mu.Unlock()
		generation.cancel()
		return errors.New("agent registration was superseded")
	}
	delete(r.agentAdmissions, agentID)
	previous := r.activeAgents[agentID]
	r.activeAgents[agentID] = generation
	r.registeredAgents[agentID] = generation
	r.agentLastReported[agentID] = r.now().UTC()
	r.mu.Unlock()
	if previous != nil {
		previous.cancel()
	}
	r.startAgentHeartbeat(agentID, generation)
	return nil
}

// StopAgent stops local heartbeats. Registry TTL expiry removes the remote
// record even if the relay cannot reach the registry during disconnect.
func (r *Reporter) StopAgent(agentID string) {
	agentID = strings.TrimSpace(agentID)
	r.mu.Lock()
	admission := r.agentAdmissions[agentID]
	delete(r.agentAdmissions, agentID)
	generation := r.activeAgents[agentID]
	delete(r.activeAgents, agentID)
	delete(r.registeredAgents, agentID)
	delete(r.agentLastReported, agentID)
	r.mu.Unlock()
	if admission != nil {
		admission.cancel()
	}
	if generation != nil {
		generation.cancel()
	}
}

func (r *Reporter) reportAgent(ctx context.Context, agentID string, forceRegister bool, expectedGeneration *agentGeneration) error {
	now := r.now().UTC()
	r.mu.Lock()
	generation, active := r.activeAgents[agentID]
	if !active || expectedGeneration != nil && generation != expectedGeneration {
		r.mu.Unlock()
		return nil
	}
	registered := r.registeredAgents[agentID] == generation
	lastReported := r.agentLastReported[agentID]
	if !forceRegister && registered && now.Sub(lastReported) < r.config.HeartbeatInterval {
		r.mu.Unlock()
		return nil
	}
	// Reserve this reporting interval so concurrent messages cannot produce a
	// heartbeat stampede. A failed request removes the reservation.
	r.agentLastReported[agentID] = now
	r.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, r.config.RequestTimeout)
	defer cancel()
	var err error
	if forceRegister || !registered {
		err = r.registerAgent(callCtx, agentID)
	} else {
		_, err = r.client.HeartbeatAgent(callCtx, &registryv1.HeartbeatAgentRequest{AgentId: agentID})
		if status.Code(err) == codes.NotFound {
			err = r.registerAgent(callCtx, agentID)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		if r.activeAgents[agentID] == generation {
			delete(r.agentLastReported, agentID)
		}
		return fmt.Errorf("report agent %s to registry: %w", agentID, err)
	}
	if r.activeAgents[agentID] == generation {
		r.registeredAgents[agentID] = generation
	}
	return nil
}

func (r *Reporter) registerAgent(ctx context.Context, agentID string) error {
	_, err := r.client.RegisterAgent(ctx, &registryv1.RegisterAgentRequest{
		Agent:   &registryv1.Agent{AgentId: agentID},
		RelayId: r.config.RelayID,
	})
	return err
}

func (r *Reporter) registerRelay(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, r.config.RequestTimeout)
	defer cancel()
	_, err := r.client.RegisterRelay(callCtx, &registryv1.RegisterRelayRequest{Relay: &registryv1.Relay{
		RelayId:  r.config.RelayID,
		Address:  r.config.AdvertiseAddress,
		GrpcPort: int32(r.config.RelayGRPCPort),
	}})
	if err != nil {
		return fmt.Errorf("register relay %s: %w", r.config.RelayID, err)
	}
	return nil
}

func (r *Reporter) start() {
	r.mu.Lock()
	r.started = true
	agents := make(map[string]*agentGeneration, len(r.activeAgents))
	for agentID, generation := range r.activeAgents {
		agents[agentID] = generation
	}
	r.mu.Unlock()
	for agentID, generation := range agents {
		r.startAgentHeartbeat(agentID, generation)
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(r.config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-r.workerCtx.Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(r.workerCtx, r.config.RequestTimeout)
				_, err := r.client.HeartbeatRelay(ctx, &registryv1.HeartbeatRelayRequest{RelayId: r.config.RelayID})
				if status.Code(err) == codes.NotFound {
					err = r.registerRelay(ctx)
				}
				cancel()
				if err != nil && !isCanceled(err) {
					slog.WarnContext(r.workerCtx, "registry relay heartbeat failed; will retry",
						slog.String("relay_id", r.config.RelayID),
						slog.String("error", err.Error()),
					)
				}
			}
		}
	}()
}

func (r *Reporter) startAgentHeartbeat(agentID string, generation *agentGeneration) {
	r.mu.Lock()
	if !r.started || generation.started || r.activeAgents[agentID] != generation {
		r.mu.Unlock()
		return
	}
	generation.started = true
	r.wg.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(r.config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-generation.ctx.Done():
				return
			case <-ticker.C:
				if err := r.reportAgent(generation.ctx, agentID, false, generation); err != nil && !isCanceled(err) {
					slog.WarnContext(generation.ctx, "registry agent heartbeat failed; will retry",
						slog.String("agent_id", agentID),
						slog.String("error", err.Error()),
					)
				}
			}
		}
	}()
}

func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled
}

func (r *Reporter) Close(ctx context.Context) error {
	r.closeOnce.Do(r.cancel)
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("stop registry reporter: %w", ctx.Err())
	}
	if r.conn != nil {
		if err := r.conn.Close(); err != nil {
			return fmt.Errorf("close registry connection: %w", err)
		}
	}
	return nil
}
