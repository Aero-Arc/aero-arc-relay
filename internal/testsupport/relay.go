//go:build integration

package testsupport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/relay"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Relay struct {
	Address string
	Conn    *grpc.ClientConn

	relay    *relay.Relay
	server   *grpc.Server
	listener net.Listener
	serveErr chan error
	stopOnce sync.Once
	stopErr  error
}

func StartRelay(t *testing.T, influx *InfluxDB, agentID, aircraftID, relayID string) *Relay {
	t.Helper()
	cfg := &config.Config{
		Telemetry: config.TelemetryConfig{
			Enabled:        true,
			Backend:        "influxdb3",
			QueueCapacity:  32,
			Workers:        1,
			BatchSize:      8,
			FlushInterval:  100 * time.Millisecond,
			EnqueueTimeout: time.Second,
			WriteTimeout:   5 * time.Second,
			RetryBackoff:   50 * time.Millisecond,
			RelayID:        relayID,
			AgentMappings: map[string]config.AgentMapping{
				agentID: {AircraftID: aircraftID},
			},
			InfluxDB: &config.NormalizedInfluxDBConfig{
				Host:     influx.URL,
				Token:    influx.Token,
				Database: influx.Database,
			},
		},
	}
	instance, err := relay.New(cfg)
	if err != nil {
		t.Fatalf("construct Relay: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = instance.Close(closeCtx)
		t.Fatalf("listen for Relay gRPC server: %v", err)
	}
	server := grpc.NewServer()
	agentv1.RegisterAgentGatewayServer(server, instance)
	relayv1.RegisterRelayControlServer(server, instance)
	fixture := &Relay{
		Address:  listener.Addr().String(),
		relay:    instance,
		server:   server,
		listener: listener,
		serveErr: make(chan error, 1),
	}
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			fixture.serveErr <- err
		}
		close(fixture.serveErr)
	}()

	dialCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixture.Conn, err = grpc.DialContext(
		dialCtx,
		fixture.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		_ = fixture.Shutdown(context.Background())
		t.Fatalf("connect to Relay at %s: %v", fixture.Address, err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := fixture.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shut down Relay at %s: %v", fixture.Address, err)
		}
	})
	t.Logf("Relay ready: endpoint=%s influx_endpoint=%s database=%s", fixture.Address, influx.URL, influx.Database)
	return fixture
}

func (r *Relay) Shutdown(ctx context.Context) error {
	r.stopOnce.Do(func() {
		if r.Conn != nil {
			r.stopErr = errors.Join(r.stopErr, r.Conn.Close())
		}
		stopped := make(chan struct{})
		go func() {
			r.server.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-ctx.Done():
			r.server.Stop()
			r.stopErr = errors.Join(r.stopErr, fmt.Errorf("graceful gRPC shutdown: %w", ctx.Err()))
		}
		if err, ok := <-r.serveErr; ok {
			r.stopErr = errors.Join(r.stopErr, fmt.Errorf("serve Relay gRPC: %w", err))
		}
		r.stopErr = errors.Join(r.stopErr, r.relay.Close(ctx))
	})
	return r.stopErr
}
