/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

// Package relay runs the MAVLink relay: it manages drone sessions, exposes
// gRPC gateway/control services, forwards telemetry to sinks, and serves metrics.
package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/outputs"
	"github.com/makinje/aero-arc-relay/internal/registryreporter"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/internal/telemetrywriter"
	telemetryinflux "github.com/makinje/aero-arc-relay/internal/telemetrywriter/influx"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Relay manages MAVLink connections and data forwarding to sinks
type Relay struct {
	config             *config.Config
	sinks              []sinks.Sink
	router             *outputs.Router
	connections        sync.Map // map[string]*gomavlib.Node
	outputsInitialized bool
	grpcServer         *grpc.Server
	grpcSessions       map[string]*DroneSession
	sessionsMu         sync.RWMutex
	relayv1.UnimplementedRelayControlServer
	agentv1.UnimplementedAgentGatewayServer
}

type DroneSession struct {
	stream           *telemetryStreamBinding
	streamGeneration uint64
	agentID          string
	SessionID        string
	ConnectedAt      time.Time
	LastHeartbeat    time.Time
	Position         *common.MessageGlobalPositionInt
	Attitude         *common.MessageAttitude
	VfrHud           *common.MessageVfrHud
	SystemStatus     *common.MessageSysStatus
	FlightID         string
	IntentID         string
	IntentVersion    uint32
	sessionMu        sync.RWMutex
	pendingMu        sync.Mutex
	pending          map[string]chan *agentv1.OperationContextCommandAck
	ownershipMu      sync.RWMutex
	retired          bool
}

type telemetryStreamBinding struct {
	stream     agentv1.AgentGateway_TelemetryStreamServer
	generation uint64
	sendMu     contextMutex
}

// contextMutex serializes stream sends while allowing callers that have not
// started a send to stop waiting when their request context is cancelled.
type contextMutex struct {
	once  sync.Once
	token chan struct{}
}

func (m *contextMutex) Lock(ctx context.Context) error {
	m.once.Do(func() {
		m.token = make(chan struct{}, 1)
		m.token <- struct{}{}
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.token:
		if err := ctx.Err(); err != nil {
			m.token <- struct{}{}
			return err
		}
		return nil
	}
}

func (m *contextMutex) Unlock() {
	m.token <- struct{}{}
}

var (
	relayMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_relay_messages_total",
		Help: "Telemetry messages handled by the relay.",
	}, []string{"source", "message_type"})

	relaySinkWriteErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aero_relay_sink_errors_total",
		Help: "Errors returned while forwarding telemetry to sinks.",
	}, []string{"sink"})
)

// New creates a new relay instance
func New(cfg *config.Config) (*Relay, error) {
	relay := &Relay{
		config:       cfg,
		sinks:        make([]sinks.Sink, 0),
		grpcSessions: make(map[string]*DroneSession),
	}

	if err := relay.initializeOutputs(); err != nil {
		return nil, fmt.Errorf("failed to initialize outputs: %w", err)
	}

	return relay, nil
}

// Start begins the relay operation
func (r *Relay) Start(ctx context.Context) error {
	slog.Info("Starting aero-arc-relay...")

	// Wait for context cancellation or signal to shut down
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.GrpcPort))
	if err != nil {
		slog.LogAttrs(ctx, slog.LevelError, "ErrCreatingTCPListener", slog.String("error", err.Error()))
		return ErrCreatingTCPListener
	}

	var creds credentials.TransportCredentials
	var homeDir string

	creds, err = credentials.NewServerTLSFromFile(r.config.TLSCertPath, r.config.TLSKeyPath)
	if r.config.Debug {
		homeDir, err = os.UserHomeDir()
		if err != nil {
			slog.LogAttrs(ctx, slog.LevelError, ErrGettingHomeDir.Error(), slog.String("error", err.Error()))
			return ErrGettingHomeDir
		}

		certPath := fmt.Sprintf("%s/%s", homeDir, DebugTLSCertPath)
		keyPath := fmt.Sprintf("%s/%s", homeDir, DebugTLSKeyPath)
		creds, err = credentials.NewServerTLSFromFile(certPath, keyPath)
	}

	if err != nil {
		slog.LogAttrs(ctx, slog.LevelError, "ErrCreatingTLSCredentials", slog.String("error", err.Error()))
		return ErrCreatingTLSCredentials
	}

	r.grpcServer = grpc.NewServer(grpc.Creds(creds))

	// Register gRPC servers
	relayv1.RegisterRelayControlServer(r.grpcServer, r)
	agentv1.RegisterAgentGatewayServer(r.grpcServer, r)

	// Start gRPC server in non blocking goroutine
	go func() {
		slog.LogAttrs(context.Background(), slog.LevelInfo, "serving gRPC server", slog.String("port", fmt.Sprintf(":%d", r.config.GrpcPort)))
		if err := r.grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			slog.LogAttrs(context.Background(), slog.LevelError, "failed to serve gRPC server", slog.String("error", err.Error()))
		}
		slog.LogAttrs(context.Background(), slog.LevelInfo, "gRPC server stopped")
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	http.Handle("/readyz", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.ready() {
			http.Error(w, `{"status":"not ready"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))

	metricsServer := &http.Server{
		Addr:    ":2112",
		Handler: nil,
	}

	shutdown := func() {
		// Shutdown gRPC server
		stopped := make(chan struct{})
		go func() {
			if r.grpcServer != nil {
				slog.Info("shutting down gRPC server")
				r.grpcServer.GracefulStop()
			}
			close(stopped)
		}()

		select {
		case <-stopped:
			slog.Info("gRPC server stopped")
		case <-time.After(10 * time.Second):
			slog.Info("gRPC server shutdown timed out")
			r.grpcServer.Stop()
		}

		// Shutdown outputs with timeout. Closing the router drains the normalized
		// telemetry queue and flushes pending backend batches.
		baseCtx := context.Background()
		sinkCtx, cancel := context.WithTimeout(baseCtx, 30*time.Second)
		if err := r.Close(sinkCtx); err != nil {
			slog.LogAttrs(context.Background(), slog.LevelWarn,
				"Error closing outputs", slog.String("error", err.Error()))
		}
		cancel() // Release resources

		// Shutdown HTTP server
		httpCtx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
		defer cancel()
		if err := metricsServer.Shutdown(httpCtx); err != nil {
			slog.LogAttrs(context.Background(), slog.LevelWarn,
				"Metrics server error when shutting down", slog.String("error", err.Error()))
		}
	}

	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.LogAttrs(context.Background(), slog.LevelInfo, "metrics server stopped", slog.String("error", err.Error()))
		}
	}()

	go func() {
		<-ctx.Done()
		signals <- syscall.SIGTERM
	}()

	for signal := range signals {
		if signal == os.Interrupt || signal == syscall.SIGTERM {
			slog.Info("Received signal to shut down relay...")
			shutdown()
			break
		}
	}

	return nil
}

// Close drains and closes all configured outputs. It is separate from the
// network-server lifecycle so embedders can guarantee that asynchronous
// telemetry batches are flushed during controlled shutdown.
func (r *Relay) Close(ctx context.Context) error {
	if r.router != nil {
		return r.router.Close(ctx)
	}
	var closeErr error
	for _, sink := range r.sinks {
		if err := sink.Close(ctx); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

func (r *Relay) ready() bool {
	return r.outputsInitialized
}

// initializeOutputs sets up internal relay outputs and configured data sinks.
func (r *Relay) initializeOutputs() error {
	return r.initializeOutputsWith(r.newTelemetryWriter, r.initializeSinks)
}

func (r *Relay) initializeOutputsWith(
	newTelemetryWriter func() (outputs.EnvelopeConsumer, error),
	initializeSinks func() error,
) (err error) {
	r.router = outputs.NewRouter()
	defer func() {
		if err == nil {
			return
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if closeErr := r.router.Close(closeCtx); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("clean up initialized outputs: %w", closeErr))
		}
	}()

	if r.config.Registry.Enabled {
		r.router.AddConsumer(
			registryreporter.NewNoopReporter(),
			registryMessageFilter(),
		)
	}

	if r.config.Telemetry.Enabled {
		consumer, err := newTelemetryWriter()
		if err != nil {
			return err
		}
		r.router.AddConsumer(consumer, telemetryMessageFilter())
	}

	if err := initializeSinks(); err != nil {
		return err
	}
	if !r.router.HasConsumers() {
		return fmt.Errorf("no outputs configured")
	}
	r.outputsInitialized = true
	return nil
}

func (r *Relay) newTelemetryWriter() (outputs.EnvelopeConsumer, error) {
	if r.config.Telemetry.Backend == "noop" {
		return telemetrywriter.NewNoopWriter(), nil
	}
	if strings.TrimSpace(r.config.Telemetry.RelayID) == "" {
		return nil, fmt.Errorf("normalized telemetry relay ID is required")
	}
	if r.config.Telemetry.Backend != "influxdb3" {
		return nil, fmt.Errorf("unsupported normalized telemetry backend %q", r.config.Telemetry.Backend)
	}
	if r.config.Telemetry.InfluxDB == nil {
		return nil, fmt.Errorf("normalized telemetry InfluxDB 3 configuration is required")
	}
	backend, err := telemetryinflux.New(telemetryinflux.Config{
		Host:     r.config.Telemetry.InfluxDB.Host,
		Token:    r.config.Telemetry.InfluxDB.Token,
		Database: r.config.Telemetry.InfluxDB.Database,
		Timeout:  r.config.Telemetry.WriteTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize normalized telemetry InfluxDB 3 backend: %w", err)
	}
	maxRetries := 3
	if r.config.Telemetry.MaxRetries != nil {
		maxRetries = *r.config.Telemetry.MaxRetries
	}
	writer, err := telemetrywriter.NewWriter(telemetrywriter.Config{
		QueueCapacity:  r.config.Telemetry.QueueCapacity,
		Workers:        r.config.Telemetry.Workers,
		BatchSize:      r.config.Telemetry.BatchSize,
		FlushInterval:  r.config.Telemetry.FlushInterval,
		EnqueueTimeout: r.config.Telemetry.EnqueueTimeout,
		WriteTimeout:   r.config.Telemetry.WriteTimeout,
		MaxRetries:     maxRetries,
		RetryBackoff:   r.config.Telemetry.RetryBackoff,
	}, backend, nil)
	if err != nil {
		_ = backend.Close(context.Background())
		return nil, fmt.Errorf("initialize normalized telemetry writer: %w", err)
	}
	return writer, nil
}

// registryMessageFilter defines the telemetry required by Aero Arc registry reporting.
func registryMessageFilter() outputs.MessageFilter {
	return outputs.MessageFilter{Include: []string{
		"Heartbeat",
		"GlobalPositionInt",
	}}
}

// telemetryMessageFilter defines the normalized hot telemetry maintained by Aero Arc.
func telemetryMessageFilter() outputs.MessageFilter {
	return outputs.MessageFilter{Include: []string{
		"Heartbeat",
		"GlobalPositionInt",
		"BatteryStatus",
		"SysStatus",
		"VfrHud",
		"ExtendedSysState",
		"GpsRawInt",
		"SystemTime",
	}}
}

// initializeSinks sets up all configured generic data sinks.
func (r *Relay) initializeSinks() error {
	// Initialize S3 sink if configured
	if r.config.Sinks.S3 != nil {
		s3Sink, err := sinks.NewS3Sink(r.config.Sinks.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 sink: %w", err)
		}
		r.sinks = append(r.sinks, s3Sink)
		r.addSinkConsumer("s3", s3Sink, r.config.Sinks.S3.MessageFilterConfig)
	}

	// Initialize GCS sink if configured
	if r.config.Sinks.GCS != nil {
		gcsSink, err := sinks.NewGCSSink(r.config.Sinks.GCS)
		if err != nil {
			return fmt.Errorf("failed to create GCS sink: %w", err)
		}
		r.sinks = append(r.sinks, gcsSink)
		r.addSinkConsumer("gcs", gcsSink, r.config.Sinks.GCS.MessageFilterConfig)
	}

	// Initialize BigQuery sink if configured
	if r.config.Sinks.BigQuery != nil {
		bigquerySink, err := sinks.NewBigQuerySink(r.config.Sinks.BigQuery)
		if err != nil {
			return fmt.Errorf("failed to create BigQuery sink: %w", err)
		}
		r.sinks = append(r.sinks, bigquerySink)
		r.addSinkConsumer("bigquery", bigquerySink, r.config.Sinks.BigQuery.MessageFilterConfig)
	}

	// Initialize Timestream sink if configured
	if r.config.Sinks.Timestream != nil {
		timestreamSink, err := sinks.NewTimestreamSink(r.config.Sinks.Timestream)
		if err != nil {
			return fmt.Errorf("failed to create Timestream sink: %w", err)
		}
		r.sinks = append(r.sinks, timestreamSink)
		r.addSinkConsumer("timestream", timestreamSink, r.config.Sinks.Timestream.MessageFilterConfig)
	}

	// Initialize InfluxDB sink if configured
	if r.config.Sinks.InfluxDB != nil {
		influxdbSink, err := sinks.NewInfluxDBSink(r.config.Sinks.InfluxDB)
		if err != nil {
			return fmt.Errorf("failed to create InfluxDB sink: %w", err)
		}
		r.sinks = append(r.sinks, influxdbSink)
		r.addSinkConsumer("influxdb", influxdbSink, r.config.Sinks.InfluxDB.MessageFilterConfig)
	}

	// Initialize Prometheus sink if configured
	if r.config.Sinks.Prometheus != nil {
		prometheusSink, err := sinks.NewPrometheusSink(r.config.Sinks.Prometheus)
		if err != nil {
			return fmt.Errorf("failed to create Prometheus sink: %w", err)
		}
		r.sinks = append(r.sinks, prometheusSink)
		r.addSinkConsumer("prometheus", prometheusSink, r.config.Sinks.Prometheus.MessageFilterConfig)
	}

	// Initialize Elasticsearch sink if configured
	if r.config.Sinks.Elasticsearch != nil {
		elasticsearchSink, err := sinks.NewElasticsearchSink(r.config.Sinks.Elasticsearch)
		if err != nil {
			return fmt.Errorf("failed to create Elasticsearch sink: %w", err)
		}
		r.sinks = append(r.sinks, elasticsearchSink)
		r.addSinkConsumer("elasticsearch", elasticsearchSink, r.config.Sinks.Elasticsearch.MessageFilterConfig)
	}

	// Initialize Kafka sink if configured
	if r.config.Sinks.Kafka != nil {
		kafkaSink, err := sinks.NewKafkaSink(r.config.Sinks.Kafka)
		if err != nil {
			return fmt.Errorf("failed to create Kafka sink: %w", err)
		}
		r.sinks = append(r.sinks, kafkaSink)
		r.addSinkConsumer("kafka", kafkaSink, r.config.Sinks.Kafka.MessageFilterConfig)
	}

	// Initialize file sink if configured
	if r.config.Sinks.File != nil {
		fileSink, err := sinks.NewFileSink(r.config.Sinks.File)
		if err != nil {
			return fmt.Errorf("failed to create file sink: %w", err)
		}
		r.sinks = append(r.sinks, fileSink)
		r.addSinkConsumer("file", fileSink, r.config.Sinks.File.MessageFilterConfig)
	}

	return nil
}

func (r *Relay) addSinkConsumer(name string, sink sinks.Sink, filter config.MessageFilterConfig) {
	if r.router == nil {
		return
	}
	r.router.AddConsumer(outputs.NewSinkConsumer(name, sink), filterFromConfig(filter))
}

func filterFromConfig(filter config.MessageFilterConfig) outputs.MessageFilter {
	return outputs.MessageFilter{
		Include: filter.IncludeMessages,
		Exclude: filter.ExcludeMessages,
	}
}

// handleTelemetryMessage processes incoming telemetry messages
func (r *Relay) handleTelemetryMessage(ctx context.Context, msg telemetry.TelemetryEnvelope) error {
	relayMessagesTotal.WithLabelValues(msg.AgentID, msg.MsgName).Inc()
	var telemetryErr error
	for _, routeErr := range r.router.Route(ctx, msg) {
		relaySinkWriteErrorsTotal.WithLabelValues(routeErr.Consumer).Inc()
		if routeErr.Consumer == telemetrywriter.ConsumerName {
			telemetryErr = routeErr.Err
		}
	}
	return telemetryErr
}
