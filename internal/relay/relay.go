package relay

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/bluenviron/gomavlib/v2"
	"github.com/bluenviron/gomavlib/v2/pkg/dialect"
	"github.com/bluenviron/gomavlib/v2/pkg/dialects/common"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// Relay manages MAVLink connections and data forwarding to sinks
type Relay struct {
	config *config.Config
	sinks  []sinks.Sink
	node   *gomavlib.Node
	mu     sync.RWMutex
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
	if err := r.initializeMAVLinkNode(r.config.MAVLink.Dialect); err != nil {
		return fmt.Errorf("failed to initialize MAVLink node: %w", err)
	}

	// Start message processing
	go r.processMessages(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	log.Println("Shutting down relay...")

	// Stop MAVLink node
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.node != nil {
		r.node.Close()
	}

	// Close all sinks
	for _, sink := range r.sinks {
		sink.Close()
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
func (r *Relay) initializeMAVLinkNode(dialect *dialect.Dialect) error {
	if len(r.config.MAVLink.Endpoints) == 0 {
		return fmt.Errorf("no MAVLink endpoints configured")
	}

	// Convert all endpoints to gomavlib endpoint configurations
	var endpoints []gomavlib.EndpointConf
	for _, endpoint := range r.config.MAVLink.Endpoints {
		endpointConf, err := r.createEndpointConf(endpoint)
		if err != nil {
			return fmt.Errorf("failed to create endpoint config for %s: %w", endpoint.Name, err)
		}
		endpoints = append(endpoints, endpointConf)
	}

	// Create single MAVLink node with all endpoints
	node, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints:   endpoints,
		Dialect:     dialect,
		OutVersion:  gomavlib.V2,
		OutSystemID: 255,
	})
	if err != nil {
		return fmt.Errorf("failed to create MAVLink node: %w", err)
	}

	r.node = node
	return nil
}

// createEndpointConf converts a config endpoint to gomavlib endpoint configuration
func (r *Relay) createEndpointConf(endpoint config.MAVLinkEndpoint) (gomavlib.EndpointConf, error) {
	switch endpoint.Protocol {
	case "udp":
		return &gomavlib.EndpointUDPClient{
			Address: fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port),
		}, nil
	case "tcp":
		return &gomavlib.EndpointTCPClient{
			Address: fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port),
		}, nil
	case "serial":
		return &gomavlib.EndpointSerial{
			Device: endpoint.Address,
			Baud:   endpoint.BaudRate,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", endpoint.Protocol)
	}
}

// processMessages processes incoming MAVLink messages
func (r *Relay) processMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-r.node.Events():
			if evt, ok := evt.(*gomavlib.EventFrame); ok {
				r.handleFrame(evt)
			}
		}
	}
}

// handleFrame processes a MAVLink frame
func (r *Relay) handleFrame(evt *gomavlib.EventFrame) {
	// Determine source endpoint name from the frame
	sourceName := r.getSourceName(evt)

	switch msg := evt.Frame.GetMessage().(type) {
	case *common.MessageHeartbeat:
		r.handleHeartbeat(msg, sourceName)
	case *common.MessageGlobalPositionInt:
		r.handleGlobalPosition(msg, sourceName)
	case *common.MessageAttitude:
		r.handleAttitude(msg, sourceName)
	case *common.MessageVfrHud:
		r.handleVfrHud(msg, sourceName)
	case *common.MessageSysStatus:
		r.handleSysStatus(msg, sourceName)
	}
}

// getSourceName determines the source endpoint name from the frame
func (r *Relay) getSourceName(evt *gomavlib.EventFrame) string {
	// For now, we'll use a simple mapping based on system ID
	// In a more sophisticated implementation, you might want to track
	// which endpoint each system ID is associated with
	systemID := evt.Frame.GetSystemID()
	return fmt.Sprintf("system-%d", systemID)
}

// handleHeartbeat processes heartbeat messages
func (r *Relay) handleHeartbeat(msg *common.MessageHeartbeat, sourceName string) {
	telemetryData := telemetry.New(sourceName)
	telemetryData.Status = "connected"
	telemetryData.Mode = r.getFlightMode(msg.CustomMode)
	r.handleTelemetry(telemetryData)
}

// handleGlobalPosition processes global position messages
func (r *Relay) handleGlobalPosition(msg *common.MessageGlobalPositionInt, sourceName string) {
	telemetryData := telemetry.New(sourceName)
	telemetryData.Latitude = float64(msg.Lat) / 1e7
	telemetryData.Longitude = float64(msg.Lon) / 1e7
	telemetryData.Altitude = float64(msg.Alt) / 1000.0 // Convert mm to meters
	r.handleTelemetry(telemetryData)
}

// handleAttitude processes attitude messages
func (r *Relay) handleAttitude(msg *common.MessageAttitude, sourceName string) {
	telemetryData := telemetry.New(sourceName)
	telemetryData.Heading = float64(msg.Yaw * 180.0 / 3.14159) // Convert to degrees
	r.handleTelemetry(telemetryData)
}

// handleVfrHud processes VFR HUD messages
func (r *Relay) handleVfrHud(msg *common.MessageVfrHud, sourceName string) {
	telemetryData := telemetry.New(sourceName)
	telemetryData.Speed = float64(msg.Groundspeed)
	telemetryData.Altitude = float64(msg.Alt)
	telemetryData.Heading = float64(msg.Heading)
	r.handleTelemetry(telemetryData)
}

// handleSysStatus processes system status messages
func (r *Relay) handleSysStatus(msg *common.MessageSysStatus, sourceName string) {
	telemetryData := telemetry.New(sourceName)
	telemetryData.Battery = float64(msg.BatteryRemaining) / 100.0 // Convert to percentage
	telemetryData.Signal = int(msg.OnboardControlSensorsPresent)
	r.handleTelemetry(telemetryData)
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

// handleTelemetry processes incoming telemetry data
func (r *Relay) handleTelemetry(data *telemetry.Data) {
	// Forward to all sinks
	for _, sink := range r.sinks {
		if err := sink.Write(data); err != nil {
			log.Printf("Failed to write to sink: %v", err)
		}
	}
}
