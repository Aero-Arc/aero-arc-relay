// Package relay runs the MAVLink relay: it manages drone sessions, exposes
// gRPC gateway/control services, forwards telemetry to sinks, and serves metrics.
package relay

import (
	"context"
	"fmt"
	"log"
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
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Relay manages MAVLink connections and data forwarding to sinks
type Relay struct {
	config           *config.Config
	sinks            []sinks.Sink
	connections      sync.Map // map[string]*gomavlib.Node
	sinksInitialized bool
	grpcServer       *grpc.Server
	grpcSessions     map[string]*DroneSession
	sessionsMu       sync.RWMutex
	relayv1.UnimplementedRelayControlServer
	agentv1.UnimplementedAgentGatewayServer
}

type DroneSession struct {
	stream        agentv1.AgentGateway_TelemetryStreamServer
	agentID       string
	SessionID     string
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	Position      *common.MessageGlobalPositionInt
	Attitude      *common.MessageAttitude
	VfrHud        *common.MessageVfrHud
	SystemStatus  *common.MessageSysStatus
	sessionMu     sync.RWMutex
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

	// Initialize sinks
	if err := relay.initializeSinks(); err != nil {
		return nil, fmt.Errorf("failed to initialize sinks: %w", err)
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

		// Shutdown sinks with timeout
		baseCtx := context.Background()
		for _, sink := range r.sinks {
			sinkCtx, cancel := context.WithTimeout(baseCtx, 30*time.Second)
			if err := sink.Close(sinkCtx); err != nil {
				slog.LogAttrs(context.Background(), slog.LevelWarn,
					"Error closing sink", slog.String("error", err.Error()))
			}
			cancel() // Release resources
		}

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

func (r *Relay) ready() bool {
	return r.sinksInitialized
}

// initializeSinks sets up all configured data sinks
func (r *Relay) initializeSinks() error {
	// Initialize S3 sink if configured
	if r.config.Sinks.S3 != nil {
		s3Sink, err := sinks.NewS3Sink(r.config.Sinks.S3)
		if err != nil {
			return fmt.Errorf("failed to create S3 sink: %w", err)
		}
		r.sinks = append(r.sinks, s3Sink)
	}

	// Initialize GCS sink if configured
	if r.config.Sinks.GCS != nil {
		gcsSink, err := sinks.NewGCSSink(r.config.Sinks.GCS)
		if err != nil {
			return fmt.Errorf("failed to create GCS sink: %w", err)
		}
		r.sinks = append(r.sinks, gcsSink)
	}

	// Initialize BigQuery sink if configured
	if r.config.Sinks.BigQuery != nil {
		bigquerySink, err := sinks.NewBigQuerySink(r.config.Sinks.BigQuery)
		if err != nil {
			return fmt.Errorf("failed to create BigQuery sink: %w", err)
		}
		r.sinks = append(r.sinks, bigquerySink)
	}

	// Initialize Timestream sink if configured
	if r.config.Sinks.Timestream != nil {
		timestreamSink, err := sinks.NewTimestreamSink(r.config.Sinks.Timestream)
		if err != nil {
			return fmt.Errorf("failed to create Timestream sink: %w", err)
		}
		r.sinks = append(r.sinks, timestreamSink)
	}

	// Initialize InfluxDB sink if configured
	if r.config.Sinks.InfluxDB != nil {
		influxdbSink, err := sinks.NewInfluxDBSink(r.config.Sinks.InfluxDB)
		if err != nil {
			return fmt.Errorf("failed to create InfluxDB sink: %w", err)
		}
		r.sinks = append(r.sinks, influxdbSink)
	}

	// Initialize Prometheus sink if configured
	if r.config.Sinks.Prometheus != nil {
		prometheusSink, err := sinks.NewPrometheusSink(r.config.Sinks.Prometheus)
		if err != nil {
			return fmt.Errorf("failed to create Prometheus sink: %w", err)
		}
		r.sinks = append(r.sinks, prometheusSink)
	}

	// Initialize Elasticsearch sink if configured
	if r.config.Sinks.Elasticsearch != nil {
		elasticsearchSink, err := sinks.NewElasticsearchSink(r.config.Sinks.Elasticsearch)
		if err != nil {
			return fmt.Errorf("failed to create Elasticsearch sink: %w", err)
		}
		r.sinks = append(r.sinks, elasticsearchSink)
	}

	// Initialize Kafka sink if configured
	if r.config.Sinks.Kafka != nil {
		kafkaSink, err := sinks.NewKafkaSink(r.config.Sinks.Kafka)
		if err != nil {
			return fmt.Errorf("failed to create Kafka sink: %w", err)
		}
		r.sinks = append(r.sinks, kafkaSink)
	}

	// Initialize file sink if configured
	if r.config.Sinks.File != nil {
		fileSink, err := sinks.NewFileSink(r.config.Sinks.File)
		if err != nil {
			return fmt.Errorf("failed to create file sink: %w", err)
		}
		r.sinks = append(r.sinks, fileSink)
	}

	if len(r.sinks) == 0 {
		return fmt.Errorf("no sinks configured")
	}
	r.sinksInitialized = true
	return nil
}

// handleTelemetryMessage processes incoming telemetry messages
func (r *Relay) handleTelemetryMessage(msg telemetry.TelemetryEnvelope) {
	relayMessagesTotal.WithLabelValues(msg.AgentID, msg.MsgName).Inc()

	// Forward to all sinks
	for _, sink := range r.sinks {
		if err := sink.WriteMessage(msg); err != nil {
			relaySinkWriteErrorsTotal.WithLabelValues(sinkNameForMetrics(sink)).Inc()
			log.Printf("Failed to write message to sink: %v", err)
		}
	}
}

func sinkNameForMetrics(s sinks.Sink) string {
	typeName := fmt.Sprintf("%T", s)
	if idx := strings.LastIndex(typeName, "."); idx != -1 {
		return typeName[idx+1:]
	}
	return typeName
}
