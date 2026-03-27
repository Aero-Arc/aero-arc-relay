package sinks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// NATSSink implements the Sink interface for NATS JetStream
type NATSSink struct {
	nc             *nats.Conn
	js             nats.JetStreamContext
	kv             nats.KeyValue
	subjectPattern string
	streamName     string
	kvKeyPattern   string
	kvMessageTypes map[string]bool // Message types that should update KV state
	base           *BaseAsyncSink
}

// NewNATSSink creates a new NATS JetStream sink
func NewNATSSink(cfg config.NATSConfig) (*NATSSink, error) {
	// Build connection options
	opts := []nats.Option{
		nats.Timeout(10 * time.Second),
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1), // Unlimited reconnects
	}

	// Add authentication if provided
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}
	if cfg.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	}

	// Connect to NATS
	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to nats: %w", err)
	}

	// Create JetStream Context
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create jetstream context: %w", err)
	}

	sink := &NATSSink{
		nc:             nc,
		js:             js,
		subjectPattern: cfg.Subject,
		kvMessageTypes: make(map[string]bool),
	}

	// Create or update JetStream if configured
	if cfg.Stream != nil {
		if err := sink.ensureStream(cfg.Stream); err != nil {
			nc.Close()
			return nil, fmt.Errorf("failed to create jetstream: %w", err)
		}
		sink.streamName = cfg.Stream.Name
	}

	// Create or get KV bucket if configured
	if cfg.KV != nil {
		if err := sink.ensureKVBucket(cfg.KV); err != nil {
			nc.Close()
			return nil, fmt.Errorf("failed to create kv bucket: %w", err)
		}
		sink.kvKeyPattern = cfg.KV.KeyPattern
		// Build message type filter
		if len(cfg.KV.MessageTypes) > 0 {
			for _, mt := range cfg.KV.MessageTypes {
				sink.kvMessageTypes[mt] = true
			}
		} else {
			// Default state-relevant message types
			sink.kvMessageTypes["Heartbeat"] = true
			sink.kvMessageTypes["GlobalPositionInt"] = true
			sink.kvMessageTypes["Attitude"] = true
			sink.kvMessageTypes["SystemStatus"] = true
			sink.kvMessageTypes["VFR_HUD"] = true
		}
	}

	// Create worker function for async processing
	worker := func(msg telemetry.TelemetryEnvelope) error {
		return sink.publishMessage(msg)
	}

	// Initialize base async sink
	sink.base = NewBaseAsyncSink(cfg.QueueSize, cfg.BackpressurePolicy, "nats", worker)

	logger.Info("NATS JetStream sink initialized successfully",
		zap.String("url", cfg.URL),
		zap.String("subject_pattern", cfg.Subject),
		zap.String("stream", cfg.Stream.Name))
	return sink, nil
}

// WriteMessage implements the Sink interface
func (s *NATSSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	return s.base.Enqueue(msg)
}

// Close implements the Sink interface
func (s *NATSSink) Close(ctx context.Context) error {
	s.base.Close()
	s.nc.Drain()
	return nil
}

// ensureStream creates or updates a JetStream stream
func (s *NATSSink) ensureStream(cfg *config.StreamConfig) error {
	// Set defaults
	storage := nats.FileStorage
	if strings.ToLower(cfg.Storage) == "memory" {
		storage = nats.MemoryStorage
	}

	replicas := cfg.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	// Parse max age
	var maxAge time.Duration
	if cfg.MaxAge != "" {
		var err error
		maxAge, err = time.ParseDuration(cfg.MaxAge)
		if err != nil {
			return fmt.Errorf("invalid max_age format: %w", err)
		}
	}

	streamConfig := &nats.StreamConfig{
		Name:        cfg.Name,
		Subjects:    cfg.Subjects,
		Storage:     storage,
		Replicas:    replicas,
		MaxAge:      maxAge,
		MaxBytes:    cfg.MaxBytes,
		MaxMsgs:     cfg.MaxMsgs,
		Compression: nats.NoCompression,
	}

	if cfg.Compression {
		streamConfig.Compression = nats.S2Compression
	}

	// Try to get existing stream info
	_, err := s.js.StreamInfo(cfg.Name)
	if err != nil {
		// Stream doesn't exist, create it
		_, err = s.js.AddStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
		logger.Info("Created NATS stream",
			zap.String("name", cfg.Name),
			zap.Strings("subjects", cfg.Subjects),
			zap.String("storage", cfg.Storage))
	} else {
		// Stream exists, update it
		_, err = s.js.UpdateStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to update stream: %w", err)
		}
		logger.Info("Updated NATS stream",
			zap.String("name", cfg.Name),
			zap.Strings("subjects", cfg.Subjects))
	}

	return nil
}

// ensureKVBucket creates or gets a NATS KeyValue bucket for device state
func (s *NATSSink) ensureKVBucket(cfg *config.KVConfig) error {
	// Set defaults
	storage := nats.FileStorage
	if strings.ToLower(cfg.Storage) == "memory" {
		storage = nats.MemoryStorage
	}

	replicas := cfg.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	// Parse TTL
	var ttl time.Duration
	if cfg.TTL != "" {
		var err error
		ttl, err = time.ParseDuration(cfg.TTL)
		if err != nil {
			return fmt.Errorf("invalid ttl format: %w", err)
		}
	}

	kvConfig := &nats.KeyValueConfig{
		Bucket:      cfg.Bucket,
		Description: cfg.Description,
		MaxBytes:    cfg.MaxBytes,
		Storage:     storage,
		Replicas:    replicas,
		TTL:         ttl,
	}

	// Try to get existing bucket
	kv, err := s.js.KeyValue(cfg.Bucket)
	if err != nil {
		// Bucket doesn't exist, create it
		kv, err = s.js.CreateKeyValue(kvConfig)
		if err != nil {
			return fmt.Errorf("failed to create kv bucket: %w", err)
		}
		logger.Info("Created NATS KV bucket",
			zap.String("bucket", cfg.Bucket),
			zap.String("key_pattern", cfg.KeyPattern),
			zap.String("storage", cfg.Storage))
	} else {
		logger.Info("Using existing NATS KV bucket",
			zap.String("bucket", cfg.Bucket),
			zap.String("key_pattern", cfg.KeyPattern))
	}

	s.kv = kv
	return nil
}

// publishMessage publishes a telemetry message to NATS JetStream with entity-specific subjects
func (s *NATSSink) publishMessage(msg telemetry.TelemetryEnvelope) error {
	// Serialize message to JSON
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Resolve subject pattern with entity/drone ID
	subject := s.resolveSubject(msg)

	// Create NATS message with headers
	natsMsg := &nats.Msg{
		Subject: subject,
		Data:    jsonData,
		Header: nats.Header{
			"entity_id":    []string{msg.DroneID},
			"source":       []string{msg.Source},
			"message_type": []string{msg.MsgName},
			"timestamp":    []string{msg.TimestampRelay.Format(time.RFC3339Nano)},
		},
	}

	// Publish message to JetStream with ack
	ack, err := s.js.PublishMsg(natsMsg)
	if err != nil {
		return fmt.Errorf("failed to publish to jetstream: %w", err)
	}

	// Log successful publish with debug level
	logger.Debug("Published message to NATS",
		zap.String("subject", subject),
		zap.String("stream", ack.Stream),
		zap.Uint64("sequence", ack.Sequence),
		zap.String("entity_id", msg.DroneID),
		zap.String("message_type", msg.MsgName))

	// Update KV state for state-relevant message types
	if s.kv != nil && s.shouldUpdateKV(msg.MsgName) {
		if err := s.updateKVState(msg); err != nil {
			logger.Warn("Failed to update KV state",
				zap.String("entity_id", msg.DroneID),
				zap.String("message_type", msg.MsgName),
				zap.Error(err))
			// Don't return error - KV update failure shouldn't stop streaming
		}
	}

	return nil
}

// shouldUpdateKV checks if this message type should update KV state
func (s *NATSSink) shouldUpdateKV(msgName string) bool {
	return s.kvMessageTypes[msgName]
}

// updateKVState updates the device state in KV store
func (s *NATSSink) updateKVState(msg telemetry.TelemetryEnvelope) error {
	// Resolve KV key pattern
	key := s.resolveKVKey(msg)

	// Build device state from message
	state := DeviceState{
		EntityID:    msg.DroneID,
		Source:      msg.Source,
		LastSeen:    msg.TimestampRelay,
		LastMsgType: msg.MsgName,
		SystemID:    msg.SystemID,
		ComponentID: msg.ComponentID,
	}

	// Update state fields based on message type
	state.UpdateFromMessage(msg)

	// Get existing state to merge (preserve fields from other message types)
	existingData, err := s.kv.Get(key)
	if err == nil {
		var existingState DeviceState
		if err := json.Unmarshal(existingData.Value(), &existingState); err == nil {
			state = mergeDeviceState(existingState, state, msg.MsgName)
		}
	}

	// Serialize and put to KV
	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to serialize device state: %w", err)
	}

	_, err = s.kv.Put(key, stateData)
	if err != nil {
		return fmt.Errorf("failed to put kv state: %w", err)
	}

	logger.Debug("Updated KV state",
		zap.String("key", key),
		zap.String("entity_id", msg.DroneID),
		zap.String("message_type", msg.MsgName))

	return nil
}

// resolveKVKey resolves the KV key pattern with entity information
func (s *NATSSink) resolveKVKey(msg telemetry.TelemetryEnvelope) string {
	key := s.kvKeyPattern
	key = strings.ReplaceAll(key, "{entity_id}", msg.DroneID)
	key = strings.ReplaceAll(key, "{drone_id}", msg.DroneID)
	key = strings.ReplaceAll(key, "{source}", msg.Source)
	return key
}

// DeviceState represents the aggregated state of a device in KV store
type DeviceState struct {
	EntityID    string    `json:"entity_id"`
	Source      string    `json:"source"`
	LastSeen    time.Time `json:"last_seen"`
	LastMsgType string    `json:"last_msg_type"`
	SystemID    uint8     `json:"system_id"`
	ComponentID uint8     `json:"component_id"`

	// Position data (from GlobalPositionInt)
	Latitude    *int32  `json:"latitude,omitempty"`
	Longitude   *int32  `json:"longitude,omitempty"`
	Altitude    *int32  `json:"altitude,omitempty"`
	RelativeAlt *int32  `json:"relative_alt,omitempty"`
	Heading     *uint16 `json:"heading,omitempty"`
	Vx          *int16  `json:"vx,omitempty"`
	Vy          *int16  `json:"vy,omitempty"`
	Vz          *int16  `json:"vz,omitempty"`

	// Attitude data (from Attitude)
	Pitch      *float32 `json:"pitch,omitempty"`
	Roll       *float32 `json:"roll,omitempty"`
	Yaw        *float32 `json:"yaw,omitempty"`
	PitchSpeed *float32 `json:"pitch_speed,omitempty"`
	RollSpeed  *float32 `json:"roll_speed,omitempty"`
	YawSpeed   *float32 `json:"yaw_speed,omitempty"`

	// System status (from SystemStatus)
	BatteryRemaining *int8   `json:"battery_remaining,omitempty"`
	VoltageBattery   *uint16 `json:"voltage_battery,omitempty"`
	Load             *uint16 `json:"load,omitempty"`

	// VFR HUD data
	GroundSpeed *float32 `json:"ground_speed,omitempty"`
	Throttle    *uint16  `json:"throttle,omitempty"`
	ClimbRate   *float32 `json:"climb_rate,omitempty"`

	// Heartbeat data
	VehicleType *string `json:"vehicle_type,omitempty"`
}

// UpdateFromMessage updates device state fields from a telemetry message
func (s *DeviceState) UpdateFromMessage(msg telemetry.TelemetryEnvelope) {
	switch msg.MsgName {
	case "GlobalPositionInt":
		if v, ok := msg.Fields["latitude"].(int32); ok {
			s.Latitude = &v
		}
		if v, ok := msg.Fields["longitude"].(int32); ok {
			s.Longitude = &v
		}
		if v, ok := msg.Fields["altitude"].(int32); ok {
			s.Altitude = &v
		}
		if v, ok := msg.Fields["relative_alt"].(int32); ok {
			s.RelativeAlt = &v
		}
		if v, ok := msg.Fields["heading"].(uint16); ok {
			s.Heading = &v
		}
		if v, ok := msg.Fields["vx"].(int16); ok {
			s.Vx = &v
		}
		if v, ok := msg.Fields["vy"].(int16); ok {
			s.Vy = &v
		}
		if v, ok := msg.Fields["vz"].(int16); ok {
			s.Vz = &v
		}

	case "Attitude":
		if v, ok := msg.Fields["pitch"].(float32); ok {
			s.Pitch = &v
		}
		if v, ok := msg.Fields["roll"].(float32); ok {
			s.Roll = &v
		}
		if v, ok := msg.Fields["yaw"].(float32); ok {
			s.Yaw = &v
		}
		if v, ok := msg.Fields["pitch_speed"].(float32); ok {
			s.PitchSpeed = &v
		}
		if v, ok := msg.Fields["roll_speed"].(float32); ok {
			s.RollSpeed = &v
		}
		if v, ok := msg.Fields["yaw_speed"].(float32); ok {
			s.YawSpeed = &v
		}

	case "SystemStatus":
		if v, ok := msg.Fields["battery_remaining"].(int8); ok {
			s.BatteryRemaining = &v
		}
		if v, ok := msg.Fields["voltage_battery"].(uint16); ok {
			s.VoltageBattery = &v
		}
		if v, ok := msg.Fields["load"].(uint16); ok {
			s.Load = &v
		}

	case "VFR_HUD":
		if v, ok := msg.Fields["ground_speed"].(float32); ok {
			s.GroundSpeed = &v
		}
		if v, ok := msg.Fields["throttle"].(uint16); ok {
			s.Throttle = &v
		}
		if v, ok := msg.Fields["climb_rate"].(float32); ok {
			s.ClimbRate = &v
		}

	case "Heartbeat":
		if v, ok := msg.Fields["type"].(string); ok {
			s.VehicleType = &v
		}
	}
}

// mergeDeviceState merges new state into existing state, preserving fields from other message types
func mergeDeviceState(existing, new DeviceState, msgType string) DeviceState {
	// Start with existing state
	merged := existing

	// Update common fields
	merged.LastSeen = new.LastSeen
	merged.LastMsgType = new.LastMsgType
	merged.SystemID = new.SystemID
	merged.ComponentID = new.ComponentID

	// Merge based on message type - only update fields from that message type
	switch msgType {
	case "GlobalPositionInt":
		merged.Latitude = new.Latitude
		merged.Longitude = new.Longitude
		merged.Altitude = new.Altitude
		merged.RelativeAlt = new.RelativeAlt
		merged.Heading = new.Heading
		merged.Vx = new.Vx
		merged.Vy = new.Vy
		merged.Vz = new.Vz

	case "Attitude":
		merged.Pitch = new.Pitch
		merged.Roll = new.Roll
		merged.Yaw = new.Yaw
		merged.PitchSpeed = new.PitchSpeed
		merged.RollSpeed = new.RollSpeed
		merged.YawSpeed = new.YawSpeed

	case "SystemStatus":
		merged.BatteryRemaining = new.BatteryRemaining
		merged.VoltageBattery = new.VoltageBattery
		merged.Load = new.Load

	case "VFR_HUD":
		merged.GroundSpeed = new.GroundSpeed
		merged.Throttle = new.Throttle
		merged.ClimbRate = new.ClimbRate

	case "Heartbeat":
		merged.VehicleType = new.VehicleType
	}

	return merged
}

// resolveSubject resolves subject pattern with entity information
func (s *NATSSink) resolveSubject(msg telemetry.TelemetryEnvelope) string {
	subject := s.subjectPattern

	// Replace placeholders with actual values
	// For 1:1 mode: constellation.telemetry.{entity_id}
	// For multi mode: constellation.telemetry.{org_id} (would need org_id from config)
	subject = strings.ReplaceAll(subject, "{entity_id}", msg.DroneID)
	subject = strings.ReplaceAll(subject, "{drone_id}", msg.DroneID)
	subject = strings.ReplaceAll(subject, "{source}", msg.Source)
	subject = strings.ReplaceAll(subject, "{message_type}", strings.ToLower(msg.MsgName))

	// For multi mode, you could replace {org_id} with organizational identifier
	// This would need to be passed in via configuration or derived from source
	subject = strings.ReplaceAll(subject, "{org_id}", "default_org")

	return subject
}
