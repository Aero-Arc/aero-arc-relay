package relay

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bluenviron/gomavlib/v2"
	"github.com/bluenviron/gomavlib/v2/pkg/dialect"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Relay manages MAVLink connections and data forwarding to sinks
type Relay struct {
	config           *config.Config
	sinks            []sinks.Sink
	connections      sync.Map // map[string]*gomavlib.Node
	sinksInitialized bool

	// Control-plane session state (authoritative) for RelayControl APIs.
	// Keyed by drone_id.
	grpcSessions   map[string]*relayv1.DroneStatus
	grpcSessionsMu sync.RWMutex

	// Fast lookup for endpoint configuration by endpoint name.
	endpointByName map[string]config.MAVLinkEndpoint

	// Required by the generated RelayControlServer interface for forward compatibility.
	// Must be embedded by value (not pointer).
	relayv1.UnimplementedRelayControlServer
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
		grpcSessions: make(map[string]*relayv1.DroneStatus),
		endpointByName: func() map[string]config.MAVLinkEndpoint {
			m := make(map[string]config.MAVLinkEndpoint, len(cfg.MAVLink.Endpoints))
			for _, ep := range cfg.MAVLink.Endpoints {
				m[ep.Name] = ep
			}
			return m
		}(),
	}

	// Initialize sinks
	if err := relay.initializeSinks(); err != nil {
		return nil, fmt.Errorf("failed to initialize sinks: %w", err)
	}

	return relay, nil
}

// Start begins the relay operation
func (r *Relay) Start(ctx context.Context) error {
	log.Println("Starting aero-arc-relay...")

	// Initialize MAVLink node with all endpoints
	processed, errs := r.initializeMAVLinkNode(r.config.MAVLink.Dialect)
	if len(errs) > 0 {
		return fmt.Errorf("failed to initialize one or more MAVLink nodes: %v", errs)
	}

	// Start new goroutines for extracting messages from the nodes
	for _, name := range processed {
		go func(name string) {
			r.processMessages(ctx, name)
		}(name)
	}

	// Wait for context cancellation or signal to shut down
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	http.Handle("/readyz", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.ready() {
			http.Error(w, `{"status":"not ready"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))

	metricsServer := &http.Server{
		Addr:    ":2112",
		Handler: nil,
	}

	shutdown := func() {
		// Close MAVLink connections
		r.connections.Range(func(key, value any) bool {
			node, ok := value.(*gomavlib.Node)
			if !ok {
				return true
			}

			node.Close()
			return true
		})

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
			log.Println("Received signal to shut down relay...")
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

// initializeMAVLinkNode sets up a single MAVLink node with all endpoints
func (r *Relay) initializeMAVLinkNode(dialect *dialect.Dialect) ([]string, []error) {
	var errs []error
	if len(r.config.MAVLink.Endpoints) == 0 {
		return nil, []error{fmt.Errorf("no MAVLink endpoints configured")}
	}

	// Convert all endpoints to gomavlib endpoint configurations
	processed := []string{}
	for _, endpoint := range r.config.MAVLink.Endpoints {
		endpointConf, err := r.createEndpointConf(endpoint)
		if err != nil {
			return nil, []error{fmt.Errorf("failed to create endpoint config for %s: %w", endpoint.Name, err)}
		}
		node, err := gomavlib.NewNode(gomavlib.NodeConf{
			Endpoints:   []gomavlib.EndpointConf{endpointConf},
			Dialect:     dialect,
			OutVersion:  gomavlib.V2,
			OutSystemID: 255,
		})
		// TODO handle failures but don't return and jump to the next endpoint.
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to create MAVLink node: %w", err))
			continue
		}
		r.connections.Store(endpoint.Name, node)
		processed = append(processed, endpoint.Name)
	}

	return processed, errs
}

// createEndpointConf converts a config endpoint to gomavlib endpoint configuration
func (r *Relay) createEndpointConf(endpoint config.MAVLinkEndpoint) (gomavlib.EndpointConf, error) {
	switch endpoint.Protocol {
	case config.MAVLinkEndpointProtocolUDP:
		address := fmt.Sprintf("%s:%d", "0.0.0.0", endpoint.Port)
		return &gomavlib.EndpointUDPServer{
			Address: address,
		}, nil

	case config.MAVLinkEndpointProtocolTCP:
		address := fmt.Sprintf("%s:%d", "0.0.0.0", endpoint.Port)
		return &gomavlib.EndpointTCPServer{
			Address: address,
		}, nil
	case config.MAVLinkEndpointProtocolSerial:
		return &gomavlib.EndpointSerial{
			Device: fmt.Sprintf("/dev/ttyUSB%d", endpoint.Port),
			Baud:   endpoint.BaudRate,
		}, nil
	default:
		return nil, fmt.Errorf("%w: %s", config.ErrInvalidProtocol, endpoint.Protocol)
	}
}

// processMessages processes incoming MAVLink messages
func (r *Relay) processMessages(ctx context.Context, endpoint string) {
	slog.LogAttrs(context.Background(), slog.LevelInfo, "processing messages for endpoint", slog.String("endpoint", endpoint))
	conn, ok := r.connections.Load(endpoint)
	if !ok {
		slog.LogAttrs(context.Background(), slog.LevelError, "endpoint connection not found. returning from processMessages", slog.String("endpoint", endpoint))
		return
	}
	node, ok := conn.(*gomavlib.Node)
	if !ok {
		slog.LogAttrs(context.Background(), slog.LevelError, "endpoint connection is not a valid MAVLink node. returning from processMessages", slog.String("endpoint", endpoint))
		return
	}

	for evt := range node.Events() {
		select {
		case <-ctx.Done():
			return
		default:
			if frameEvt, ok := evt.(*gomavlib.EventFrame); ok {
				// Mark session as active and refresh last-seen on any inbound frame.
				r.touchSessionHeartbeat(endpoint)
				r.handleFrame(frameEvt, endpoint)
				continue
			}

			if _, ok := evt.(*gomavlib.EventChannelOpen); ok {
				slog.LogAttrs(context.Background(), slog.LevelInfo, "channel open for endpoint", slog.String("endpoint", endpoint))
				r.markSessionConnected(endpoint)
				continue
			}

			if _, ok := evt.(*gomavlib.EventChannelClose); ok {
				slog.LogAttrs(context.Background(), slog.LevelInfo, "channel closed for endpoint", slog.String("endpoint", endpoint))
				r.markSessionDisconnected(endpoint)
				continue
			}

			slog.LogAttrs(context.Background(), slog.LevelError, "unsupported event type", slog.String("event_type", fmt.Sprintf("%T", evt)))
		}
	}
}

func (r *Relay) droneIDForEndpoint(endpointName string) string {
	ep, ok := r.endpointByName[endpointName]
	if !ok {
		// Fallback to endpoint name; this keeps the control-plane usable even if
		// endpoint indexing drifts in future refactors.
		return endpointName
	}
	if ep.DroneID != "" {
		return ep.DroneID
	}
	return endpointName
}

func (r *Relay) markSessionConnected(endpointName string) {
	droneID := r.droneIDForEndpoint(endpointName)
	now := time.Now().UTC().UnixNano()

	r.grpcSessionsMu.Lock()
	defer r.grpcSessionsMu.Unlock()

	existing := r.grpcSessions[droneID]
	if existing == nil {
		existing = &relayv1.DroneStatus{DroneId: droneID}
	}
	existing.ConnectedAtUnixNs = now
	existing.LastHeartbeatUnixNs = now
	r.grpcSessions[droneID] = existing
}

func (r *Relay) touchSessionHeartbeat(endpointName string) {
	droneID := r.droneIDForEndpoint(endpointName)
	now := time.Now().UTC().UnixNano()

	r.grpcSessionsMu.Lock()
	defer r.grpcSessionsMu.Unlock()

	existing := r.grpcSessions[droneID]
	if existing == nil {
		existing = &relayv1.DroneStatus{DroneId: droneID, ConnectedAtUnixNs: now}
	}
	existing.LastHeartbeatUnixNs = now
	r.grpcSessions[droneID] = existing
}

func (r *Relay) markSessionDisconnected(endpointName string) {
	droneID := r.droneIDForEndpoint(endpointName)
	r.grpcSessionsMu.Lock()
	defer r.grpcSessionsMu.Unlock()
	delete(r.grpcSessions, droneID)
}

// handleFrame processes a MAVLink frame
func (r *Relay) handleFrame(evt *gomavlib.EventFrame, endpoint string) {
	// Determine source endpoint name from the frame
	switch msg := evt.Frame.GetMessage().(type) {
	case *common.MessageHeartbeat:
		r.handleHeartbeat(msg, endpoint)
	case *common.MessageGlobalPositionInt:
		r.handleGlobalPosition(msg, endpoint)
	case *common.MessageAttitude:
		r.handleAttitude(msg, endpoint)
	case *common.MessageVfrHud:
		r.handleVfrHud(msg, endpoint)
	case *common.MessageSysStatus:
		r.handleSysStatus(msg, endpoint)
	}
}

// handleHeartbeat processes heartbeat messages
func (r *Relay) handleHeartbeat(msg *common.MessageHeartbeat, endpoint string) {
	envelope := telemetry.BuildHeartbeatEnvelope(endpoint, msg)
	r.handleTelemetryMessage(envelope)
}

// handleGlobalPosition processes global position messages
func (r *Relay) handleGlobalPosition(msg *common.MessageGlobalPositionInt, source string) {
	envelope := telemetry.BuildGlobalPositionIntEnvelope(source, msg)
	r.handleTelemetryMessage(envelope)
}

// handleAttitude processes attitude messages
func (r *Relay) handleAttitude(msg *common.MessageAttitude, source string) {
	envelope := telemetry.BuildAttitudeEnvelope(source, msg)
	r.handleTelemetryMessage(envelope)
}

// handleVfrHud processes VFR HUD messages
func (r *Relay) handleVfrHud(msg *common.MessageVfrHud, source string) {
	envelope := telemetry.BuildVfrHudEnvelope(source, msg)

	r.handleTelemetryMessage(envelope)
}

// handleSysStatus processes system status messages
func (r *Relay) handleSysStatus(msg *common.MessageSysStatus, source string) {
	envelope := telemetry.BuildSysStatusEnvelope(source, msg)
	r.handleTelemetryMessage(envelope)
}

// getFlightMode converts custom mode to flight mode string
func (r *Relay) getFlightMode(customMode uint32) string {
	// This is a simplified mapping - in practice, you'd need to check
	// the specific autopilot type and mode definitions
	switch customMode {
	case 0:
		return "STABILIZE"
	case 1:
		return "ACRO"
	case 2:
		return "ALT_HOLD"
	case 3:
		return "AUTO"
	case 4:
		return "GUIDED"
	case 5:
		return "LOITER"
	case 6:
		return "RTL"
	case 7:
		return "CIRCLE"
	case 8:
		return "POSITION"
	case 9:
		return "LAND"
	case 10:
		return "OF_LOITER"
	case 11:
		return "DRIFT"
	case 13:
		return "SPORT"
	case 14:
		return "FLIP"
	case 15:
		return "AUTOTUNE"
	case 16:
		return "POSHOLD"
	case 17:
		return "BRAKE"
	case 18:
		return "THROW"
	case 19:
		return "AVOID_ADSB"
	case 20:
		return "GUIDED_NOGPS"
	case 21:
		return "SMART_RTL"
	case 22:
		return "FLOWHOLD"
	case 23:
		return "FOLLOW"
	case 24:
		return "ZIGZAG"
	case 25:
		return "SYSTEMID"
	case 26:
		return "AUTOROTATE"
	case 27:
		return "AUTO_RTL"
	default:
		return "UNKNOWN"
	}
}

// handleTelemetryMessage processes incoming telemetry messages
func (r *Relay) handleTelemetryMessage(msg telemetry.TelemetryEnvelope) {
	relayMessagesTotal.WithLabelValues(msg.DroneID, msg.MsgName).Inc()

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
