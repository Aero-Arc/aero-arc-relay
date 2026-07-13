package telemetrynormalize

import (
	"fmt"
	"strings"
	"time"

	"github.com/makinje/aero-arc-relay/internal/outputs"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type Normalizer interface {
	Normalize(telemetry.TelemetryEnvelope) (Record, error)
}

type NormalizerFunc func(telemetry.TelemetryEnvelope) (Record, error)

func (f NormalizerFunc) Normalize(envelope telemetry.TelemetryEnvelope) (Record, error) {
	return f(envelope)
}

type Registry struct {
	normalizers map[string]Normalizer
}

func NewRegistry() *Registry {
	registry := &Registry{normalizers: make(map[string]Normalizer)}
	registry.Register("global_position_int", NormalizerFunc(normalizeGlobalPositionInt))
	registry.Register("battery_status", NormalizerFunc(normalizeBatteryStatus))
	registry.Register("heartbeat", NormalizerFunc(normalizeHeartbeat))
	registry.Register("sys_status", NormalizerFunc(normalizeSysStatus))
	registry.Register("vfr_hud", NormalizerFunc(normalizeVFRHUD))
	registry.Register("extended_sys_state", NormalizerFunc(normalizeExtendedSysState))
	registry.Register("gps_raw_int", NormalizerFunc(normalizeGPSRawInt))
	registry.Register("system_time", NormalizerFunc(normalizeSystemTime))
	return registry
}

func (r *Registry) Register(messageName string, normalizer Normalizer) {
	if r == nil || normalizer == nil {
		return
	}
	name := outputs.NormalizeMessageName(messageName)
	if name != "" && name != "*" {
		r.normalizers[name] = normalizer
	}
}

func (r *Registry) Lookup(messageName string) (Normalizer, bool) {
	if r == nil {
		return nil, false
	}
	normalizer, ok := r.normalizers[outputs.NormalizeMessageName(messageName)]
	return normalizer, ok
}

func baseRecord(envelope telemetry.TelemetryEnvelope, canonicalName string) (Record, error) {
	relayTime := envelope.TimestampRelay.UTC()
	if relayTime.IsZero() {
		return Record{}, fmt.Errorf("relay time is required")
	}
	eventTime := relayTime
	timestampSource := TimestampSourceRelay
	var agentTime *time.Time
	if !envelope.TimestampAgent.IsZero() {
		value := envelope.TimestampAgent.UTC()
		agentTime = &value
		eventTime = value
		timestampSource = TimestampSourceAgent
	}
	dialect := strings.ToLower(strings.TrimSpace(envelope.Dialect))
	if dialect == "" {
		dialect = "common"
	}
	if strings.TrimSpace(envelope.AgentID) == "" {
		return Record{}, fmt.Errorf("agent ID is required")
	}
	frameCaptureUnixNano := int64(0)
	if agentTime != nil {
		frameCaptureUnixNano = agentTime.UnixNano()
	}
	return Record{
		SchemaVersion: SchemaVersion,
		Identity: IdentityContext{
			OperatorID:    envelope.OperatorID,
			AircraftID:    envelope.AircraftID,
			AgentID:       envelope.AgentID,
			RelayID:       envelope.RelayID,
			SessionID:     envelope.SessionID,
			FlightID:      envelope.FlightID,
			IntentID:      envelope.IntentID,
			IntentVersion: envelope.IntentVersion,
		},
		Source: SourceContext{
			FrameID:   fmt.Sprintf("%d:%s:%d:%d", len(envelope.AgentID), envelope.AgentID, frameCaptureUnixNano, envelope.WALSequence),
			Sequence:  envelope.WALSequence,
			MessageID: envelope.MsgID,
			Dialect:   dialect,
		},
		Timing: TimingContext{
			EventTime:        eventTime,
			RelayTime:        relayTime,
			AgentCaptureTime: agentTime,
			TimestampSource:  timestampSource,
		},
		MessageName: canonicalName,
		Fields:      make(Fields),
	}, nil
}
