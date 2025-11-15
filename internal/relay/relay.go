package relay

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/bluenviron/gomavlib/v2"
	"github.com/bluenviron/gomavlib/v2/pkg/dialect"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// Relay manages MAVLink connections and data forwarding to sinks
type Relay struct {
	config      *config.Config
	sinks       []sinks.Sink
	connections sync.Map // map[string]*gomavlib.Node
	mu          sync.RWMutex
}

// New creates a new relay instance
func New(cfg *config.Config) (*Relay, error) {
	relay := &Relay{
		config: cfg,
		sinks:  make([]sinks.Sink, 0),
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

	shutdown := func() {
		r.connections.Range(func(key, value any) bool {
			node, ok := value.(*gomavlib.Node)
			if !ok {
				return true
			}

			node.Close()
			return true
		})

		for _, sink := range r.sinks {
			sink.Close()
		}

	}

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
	mode := strings.ToLower(endpoint.Mode)
	if mode == "" {
		mode = "server"
	}

	switch endpoint.Protocol {
	case "udp":
		address := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
		switch mode {
		case "server":
			return &gomavlib.EndpointUDPServer{
				Address: address,
			}, nil
		case "client":
			return &gomavlib.EndpointUDPClient{
				Address: address,
			}, nil
		default:
			return nil, fmt.Errorf("unsupported mode %q for UDP endpoint %s", endpoint.Mode, endpoint.Name)
		}
	case "tcp":
		address := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
		switch mode {
		case "server":
			return &gomavlib.EndpointTCPServer{
				Address: address,
			}, nil
		case "client":
			return &gomavlib.EndpointTCPClient{
				Address: address,
			}, nil
		default:
			return nil, fmt.Errorf("unsupported mode %q for TCP endpoint %s", endpoint.Mode, endpoint.Name)
		}
	case "serial":
		if mode != "server" && mode != "client" && mode != "" {
			return nil, fmt.Errorf("unsupported mode %q for serial endpoint %s", endpoint.Mode, endpoint.Name)
		}
		return &gomavlib.EndpointSerial{
			Device: endpoint.Address,
			Baud:   endpoint.BaudRate,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", endpoint.Protocol)
	}
}

// processMessages processes incoming MAVLink messages
func (r *Relay) processMessages(ctx context.Context, name string) {
	log.Printf("processing messages for %s", name)
	conn, ok := r.connections.Load(name)
	if !ok {
		log.Fatalf("connection %s not found", name)
	}
	node, ok := conn.(*gomavlib.Node)
	if !ok {
		log.Fatalf("connection %s is not a valid MAVLink node", name)
	}

	for evt := range node.Events() {
		select {
		case <-ctx.Done():
			return
		default:
			if frameEvt, ok := evt.(*gomavlib.EventFrame); ok {
				r.handleFrame(frameEvt, name)
				continue
			}

			if _, ok := evt.(*gomavlib.EventChannelOpen); ok {
				log.Printf("channel open for %s", name)
				continue
			}

			if _, ok := evt.(*gomavlib.EventChannelClose); ok {
				log.Printf("channel closed for %s", name)
				continue
			}

			log.Printf("unsupported event type: %T", evt)
		}
	}
}

// handleFrame processes a MAVLink frame
func (r *Relay) handleFrame(evt *gomavlib.EventFrame, name string) {
	// Determine source endpoint name from the frame
	switch msg := evt.Frame.GetMessage().(type) {
	case *common.MessageHeartbeat:
		r.handleHeartbeat(msg, name)
	case *common.MessageGlobalPositionInt:
		r.handleGlobalPosition(msg, name)
	case *common.MessageAttitude:
		r.handleAttitude(msg, name)
	case *common.MessageVfrHud:
		r.handleVfrHud(msg, name)
	case *common.MessageSysStatus:
		r.handleSysStatus(msg, name)
	}
}

// handleHeartbeat processes heartbeat messages
func (r *Relay) handleHeartbeat(msg *common.MessageHeartbeat, sourceName string) {
	heartbeatMsg := telemetry.NewHeartbeatMessage(sourceName)
	heartbeatMsg.Status = "connected"
	heartbeatMsg.Mode = r.getFlightMode(msg.CustomMode)
	r.handleTelemetryMessage(heartbeatMsg)
}

// handleGlobalPosition processes global position messages
func (r *Relay) handleGlobalPosition(msg *common.MessageGlobalPositionInt, sourceName string) {
	positionMsg := telemetry.NewPositionMessage(sourceName)
	positionMsg.Latitude = float64(msg.Lat) / 1e7
	positionMsg.Longitude = float64(msg.Lon) / 1e7
	positionMsg.Altitude = float64(msg.Alt) / 1000.0 // Convert mm to meters
	r.handleTelemetryMessage(positionMsg)
}

// handleAttitude processes attitude messages
func (r *Relay) handleAttitude(msg *common.MessageAttitude, sourceName string) {
	attitudeMsg := telemetry.NewAttitudeMessage(sourceName)
	attitudeMsg.Roll = float64(msg.Roll * 180.0 / 3.14159)   // Convert to degrees
	attitudeMsg.Pitch = float64(msg.Pitch * 180.0 / 3.14159) // Convert to degrees
	attitudeMsg.Yaw = float64(msg.Yaw * 180.0 / 3.14159)     // Convert to degrees
	r.handleTelemetryMessage(attitudeMsg)
}

// handleVfrHud processes VFR HUD messages
func (r *Relay) handleVfrHud(msg *common.MessageVfrHud, sourceName string) {
	vfrHudMsg := telemetry.NewVfrHudMessage(sourceName)
	vfrHudMsg.Speed = float64(msg.Groundspeed)
	vfrHudMsg.Altitude = float64(msg.Alt)
	vfrHudMsg.Heading = float64(msg.Heading)
	r.handleTelemetryMessage(vfrHudMsg)
}

// handleSysStatus processes system status messages
func (r *Relay) handleSysStatus(msg *common.MessageSysStatus, sourceName string) {
	batteryMsg := telemetry.NewBatteryMessage(sourceName)
	batteryMsg.Battery = float64(msg.BatteryRemaining) / 100.0 // Convert to percentage
	batteryMsg.Voltage = float64(msg.VoltageBattery) / 1000.0  // Convert mV to V
	r.handleTelemetryMessage(batteryMsg)
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
func (r *Relay) handleTelemetryMessage(msg telemetry.TelemetryMessage) {
	// Forward to all sinks
	for _, sink := range r.sinks {
		if err := sink.WriteMessage(msg); err != nil {
			log.Printf("Failed to write message to sink: %v", err)
		}
	}
}
