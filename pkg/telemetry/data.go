package telemetry

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"time"
)

// TelemetryMessage interface for individual MAVLink messages
type TelemetryMessage interface {
	GetSource() string
	GetTimestamp() time.Time
	GetMessageType() string
	ToJSON() ([]byte, error)
	ToBinary() ([]byte, error)
}

// HeartbeatMessage represents MAVLink HEARTBEAT message
type HeartbeatMessage struct {
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
	Mode      string    `json:"flight_mode"`
	Type      string    `json:"message_type"`
}

// PositionMessage represents MAVLink GLOBAL_POSITION_INT message
type PositionMessage struct {
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Altitude  float64   `json:"altitude"`
	Type      string    `json:"message_type"`
}

// AttitudeMessage represents MAVLink ATTITUDE message
type AttitudeMessage struct {
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Roll      float64   `json:"roll"`
	Pitch     float64   `json:"pitch"`
	Yaw       float64   `json:"yaw"`
	Type      string    `json:"message_type"`
}

// VfrHudMessage represents MAVLink VFR_HUD message
type VfrHudMessage struct {
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Speed     float64   `json:"speed"`
	Altitude  float64   `json:"altitude"`
	Heading   float64   `json:"heading"`
	Type      string    `json:"message_type"`
}

// BatteryMessage represents MAVLink SYS_STATUS message
type BatteryMessage struct {
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Battery   float64   `json:"battery"`
	Voltage   float64   `json:"voltage,omitempty"`
	Type      string    `json:"message_type"`
}

// Implement TelemetryMessage interface for HeartbeatMessage
func (h *HeartbeatMessage) GetSource() string       { return h.Source }
func (h *HeartbeatMessage) GetTimestamp() time.Time { return h.Timestamp }
func (h *HeartbeatMessage) GetMessageType() string  { return h.Type }
func (h *HeartbeatMessage) ToJSON() ([]byte, error) { return json.Marshal(h) }
func (h *HeartbeatMessage) ToBinary() ([]byte, error) {
	return h.encodeBinary()
}

// Implement TelemetryMessage interface for PositionMessage
func (p *PositionMessage) GetSource() string       { return p.Source }
func (p *PositionMessage) GetTimestamp() time.Time { return p.Timestamp }
func (p *PositionMessage) GetMessageType() string  { return p.Type }
func (p *PositionMessage) ToJSON() ([]byte, error) { return json.Marshal(p) }
func (p *PositionMessage) ToBinary() ([]byte, error) {
	return p.encodeBinary()
}

// Implement TelemetryMessage interface for AttitudeMessage
func (a *AttitudeMessage) GetSource() string       { return a.Source }
func (a *AttitudeMessage) GetTimestamp() time.Time { return a.Timestamp }
func (a *AttitudeMessage) GetMessageType() string  { return a.Type }
func (a *AttitudeMessage) ToJSON() ([]byte, error) { return json.Marshal(a) }
func (a *AttitudeMessage) ToBinary() ([]byte, error) {
	return a.encodeBinary()
}

// Implement TelemetryMessage interface for VfrHudMessage
func (v *VfrHudMessage) GetSource() string       { return v.Source }
func (v *VfrHudMessage) GetTimestamp() time.Time { return v.Timestamp }
func (v *VfrHudMessage) GetMessageType() string  { return v.Type }
func (v *VfrHudMessage) ToJSON() ([]byte, error) { return json.Marshal(v) }
func (v *VfrHudMessage) ToBinary() ([]byte, error) {
	return v.encodeBinary()
}

// Implement TelemetryMessage interface for BatteryMessage
func (b *BatteryMessage) GetSource() string       { return b.Source }
func (b *BatteryMessage) GetTimestamp() time.Time { return b.Timestamp }
func (b *BatteryMessage) GetMessageType() string  { return b.Type }
func (b *BatteryMessage) ToJSON() ([]byte, error) { return json.Marshal(b) }
func (b *BatteryMessage) ToBinary() ([]byte, error) {
	return b.encodeBinary()
}

// Binary encoding helpers
func (h *HeartbeatMessage) encodeBinary() ([]byte, error) {
	sourceBytes := []byte(h.Source)
	statusBytes := []byte(h.Status)
	modeBytes := []byte(h.Mode)

	data := make([]byte, 8+4+len(sourceBytes)+4+len(statusBytes)+4+len(modeBytes))
	offset := 0

	// Timestamp (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(h.Timestamp.Unix()))
	offset += 8

	// Source length + source
	binary.BigEndian.PutUint32(data[offset:], uint32(len(sourceBytes)))
	offset += 4
	copy(data[offset:], sourceBytes)
	offset += len(sourceBytes)

	// Status length + status
	binary.BigEndian.PutUint32(data[offset:], uint32(len(statusBytes)))
	offset += 4
	copy(data[offset:], statusBytes)
	offset += len(statusBytes)

	// Mode length + mode
	binary.BigEndian.PutUint32(data[offset:], uint32(len(modeBytes)))
	offset += 4
	copy(data[offset:], modeBytes)

	return data, nil
}

func (p *PositionMessage) encodeBinary() ([]byte, error) {
	sourceBytes := []byte(p.Source)
	data := make([]byte, 8+4+len(sourceBytes)+8+8+8)
	offset := 0

	// Timestamp (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(p.Timestamp.Unix()))
	offset += 8

	// Source length + source
	binary.BigEndian.PutUint32(data[offset:], uint32(len(sourceBytes)))
	offset += 4
	copy(data[offset:], sourceBytes)
	offset += len(sourceBytes)

	// Latitude (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(p.Latitude))
	offset += 8

	// Longitude (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(p.Longitude))
	offset += 8

	// Altitude (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(p.Altitude))

	return data, nil
}

func (a *AttitudeMessage) encodeBinary() ([]byte, error) {
	sourceBytes := []byte(a.Source)
	data := make([]byte, 8+4+len(sourceBytes)+8+8+8)
	offset := 0

	// Timestamp (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(a.Timestamp.Unix()))
	offset += 8

	// Source length + source
	binary.BigEndian.PutUint32(data[offset:], uint32(len(sourceBytes)))
	offset += 4
	copy(data[offset:], sourceBytes)
	offset += len(sourceBytes)

	// Roll (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(a.Roll))
	offset += 8

	// Pitch (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(a.Pitch))
	offset += 8

	// Yaw (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(a.Yaw))

	return data, nil
}

func (v *VfrHudMessage) encodeBinary() ([]byte, error) {
	sourceBytes := []byte(v.Source)
	data := make([]byte, 8+4+len(sourceBytes)+8+8+8)
	offset := 0

	// Timestamp (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(v.Timestamp.Unix()))
	offset += 8

	// Source length + source
	binary.BigEndian.PutUint32(data[offset:], uint32(len(sourceBytes)))
	offset += 4
	copy(data[offset:], sourceBytes)
	offset += len(sourceBytes)

	// Speed (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(v.Speed))
	offset += 8

	// Altitude (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(v.Altitude))
	offset += 8

	// Heading (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(v.Heading))

	return data, nil
}

func (b *BatteryMessage) encodeBinary() ([]byte, error) {
	sourceBytes := []byte(b.Source)
	data := make([]byte, 8+4+len(sourceBytes)+8+8)
	offset := 0

	// Timestamp (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], uint64(b.Timestamp.Unix()))
	offset += 8

	// Source length + source
	binary.BigEndian.PutUint32(data[offset:], uint32(len(sourceBytes)))
	offset += 4
	copy(data[offset:], sourceBytes)
	offset += len(sourceBytes)

	// Battery (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(b.Battery))
	offset += 8

	// Voltage (8 bytes)
	binary.BigEndian.PutUint64(data[offset:], math.Float64bits(b.Voltage))

	return data, nil
}

// NewHeartbeatMessage creates a new heartbeat message
func NewHeartbeatMessage(source string) *HeartbeatMessage {
	return &HeartbeatMessage{
		Source:    source,
		Timestamp: time.Now(),
		Type:      "heartbeat",
	}
}

// NewPositionMessage creates a new position message
func NewPositionMessage(source string) *PositionMessage {
	return &PositionMessage{
		Source:    source,
		Timestamp: time.Now(),
		Type:      "position",
	}
}

// NewAttitudeMessage creates a new attitude message
func NewAttitudeMessage(source string) *AttitudeMessage {
	return &AttitudeMessage{
		Source:    source,
		Timestamp: time.Now(),
		Type:      "attitude",
	}
}

// NewVfrHudMessage creates a new VFR HUD message
func NewVfrHudMessage(source string) *VfrHudMessage {
	return &VfrHudMessage{
		Source:    source,
		Timestamp: time.Now(),
		Type:      "vfr_hud",
	}
}

// NewBatteryMessage creates a new battery message
func NewBatteryMessage(source string) *BatteryMessage {
	return &BatteryMessage{
		Source:    source,
		Timestamp: time.Now(),
		Type:      "battery",
	}
}
