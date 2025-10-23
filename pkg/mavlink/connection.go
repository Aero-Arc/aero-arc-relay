package mavlink

import (
	"context"
	"fmt"

	"github.com/bluenviron/gomavlib/v2"
	"github.com/bluenviron/gomavlib/v2/pkg/dialect"
	"github.com/bluenviron/gomavlib/v2/pkg/frame"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// Connection represents a MAVLink connection
type Connection struct {
	name     string
	endpoint *gomavlib.EndpointConf
	node     *gomavlib.Node
	handler  func(*telemetry.Data)
}

// NewConnection creates a new MAVLink connection
func NewConnection(endpoint config.MAVLinkEndpoint, handler func(*telemetry.Data)) (*Connection, error) {
	var endpointConf *gomavlib.EndpointConf

	switch endpoint.Protocol {
	case "udp":
		endpointConf = &gomavlib.EndpointConf{
			Type:    gomavlib.EndpointUDP,
			Address: fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port),
		}
	case "tcp":
		endpointConf = &gomavlib.EndpointConf{
			Type:    gomavlib.EndpointTCP,
			Address: fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port),
		}
	case "serial":
		endpointConf = &gomavlib.EndpointConf{
			Type:    gomavlib.EndpointSerial,
			Address: endpoint.Address,
			Baud:    endpoint.BaudRate,
		}
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", endpoint.Protocol)
	}

	return &Connection{
		name:     endpoint.Name,
		endpoint: endpointConf,
		handler:  handler,
	}, nil
}

// Start begins the MAVLink connection
func (c *Connection) Start(ctx context.Context) error {
	// Create MAVLink node
	node, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints:   []gomavlib.EndpointConf{*c.endpoint},
		Dialect:     dialect.Standard,
		OutVersion:  gomavlib.V2,
		OutSystemID: 255,
	})
	if err != nil {
		return fmt.Errorf("failed to create MAVLink node: %w", err)
	}

	c.node = node

	// Start processing messages
	go c.processMessages(ctx)

	return nil
}

// Stop stops the MAVLink connection
func (c *Connection) Stop() {
	if c.node != nil {
		c.node.Close()
	}
}

// Name returns the connection name
func (c *Connection) Name() string {
	return c.name
}

// processMessages processes incoming MAVLink messages
func (c *Connection) processMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-c.node.Events():
			if evt, ok := evt.(*gomavlib.EventFrame); ok {
				c.handleFrame(evt)
			}
		}
	}
}

// handleFrame processes a MAVLink frame
func (c *Connection) handleFrame(evt *gomavlib.EventFrame) {
	switch msg := evt.Frame.GetMessage().(type) {
	case *frame.MessageHeartbeat:
		c.handleHeartbeat(msg)
	case *frame.MessageGlobalPositionInt:
		c.handleGlobalPosition(msg)
	case *frame.MessageAttitude:
		c.handleAttitude(msg)
	case *frame.MessageVfrHud:
		c.handleVfrHud(msg)
	case *frame.MessageSysStatus:
		c.handleSysStatus(msg)
	}
}

// handleHeartbeat processes heartbeat messages
func (c *Connection) handleHeartbeat(msg *frame.MessageHeartbeat) {
	telemetryData := telemetry.New(c.name)
	telemetryData.Status = "connected"
	telemetryData.Mode = getFlightMode(msg.CustomMode)

	c.handler(telemetryData)
}

// handleGlobalPosition processes global position messages
func (c *Connection) handleGlobalPosition(msg *frame.MessageGlobalPositionInt) {
	telemetryData := telemetry.New(c.name)
	telemetryData.Latitude = float64(msg.Lat) / 1e7
	telemetryData.Longitude = float64(msg.Lon) / 1e7
	telemetryData.Altitude = float64(msg.Alt) / 1000.0 // Convert mm to meters

	c.handler(telemetryData)
}

// handleAttitude processes attitude messages
func (c *Connection) handleAttitude(msg *frame.MessageAttitude) {
	telemetryData := telemetry.New(c.name)
	telemetryData.Heading = msg.Yaw * 180.0 / 3.14159 // Convert to degrees

	c.handler(telemetryData)
}

// handleVfrHud processes VFR HUD messages
func (c *Connection) handleVfrHud(msg *frame.MessageVfrHud) {
	telemetryData := telemetry.New(c.name)
	telemetryData.Speed = msg.Groundspeed
	telemetryData.Altitude = msg.Alt
	telemetryData.Heading = msg.Heading

	c.handler(telemetryData)
}

// handleSysStatus processes system status messages
func (c *Connection) handleSysStatus(msg *frame.MessageSysStatus) {
	telemetryData := telemetry.New(c.name)
	telemetryData.Battery = float64(msg.BatteryRemaining) / 100.0 // Convert to percentage
	telemetryData.Signal = int(msg.OnboardControlSensorsPresent)

	c.handler(telemetryData)
}

// getFlightMode converts custom mode to flight mode string
func getFlightMode(customMode uint32) string {
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
