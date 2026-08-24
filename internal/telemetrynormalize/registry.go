package telemetrynormalize

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/makinje/aero-arc-relay/internal/outputs"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

type Normalizer interface {
	Normalize(telemetry.TelemetryEnvelope) (Record, error)
}

type NormalizerFunc func(telemetry.TelemetryEnvelope) (Record, error)

// Normalize normalizes the supplied telemetry through NormalizerFunc.
//
// Parameters:
//   - envelope: is the telemetry.TelemetryEnvelope value supplied to Normalize.
//
// Returns:
//   - result: is the Record value produced by Normalize.
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (f NormalizerFunc) Normalize(envelope telemetry.TelemetryEnvelope) (Record, error) {
	return f(envelope)
}

type Registry struct {
	normalizers map[string]Normalizer
}

// NewRegistry constructs telemetrynormalize from the supplied configuration and dependencies.
//
// Returns:
//   - result: is the *Registry value produced by NewRegistry.
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

// Register registers the supplied Registry identity or handler.
//
// Parameters:
//   - messageName: is the string value supplied to Register.
//   - normalizer: is the Normalizer value supplied to Register.
func (r *Registry) Register(messageName string, normalizer Normalizer) {
	if r == nil || normalizer == nil {
		return
	}
	name := outputs.NormalizeMessageName(messageName)
	if name != "" && name != "*" {
		r.normalizers[name] = normalizer
	}
}

// Lookup looks up Registry data using the supplied key.
//
// Parameters:
//   - messageName: is the string value supplied to Lookup.
//
// Returns:
//   - result: is the Normalizer value produced by Lookup.
//   - bool: reports whether the requested condition was satisfied.
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
	if envelope.TimestampAgent.IsZero() {
		return Record{}, fmt.Errorf("agent capture time is required")
	}
	agentTimeValue := envelope.TimestampAgent.UTC()
	agentTime := &agentTimeValue
	dialect := strings.ToLower(strings.TrimSpace(envelope.Dialect))
	if dialect == "" {
		dialect = "common"
	}
	if strings.TrimSpace(envelope.AgentID) == "" {
		return Record{}, fmt.Errorf("agent ID is required")
	}
	walID := strings.TrimSpace(envelope.WALID)
	if walID == "" {
		return Record{}, fmt.Errorf("WAL generation ID is required")
	}
	if _, err := uuid.Parse(walID); err != nil {
		return Record{}, fmt.Errorf("WAL generation ID is invalid: %w", err)
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
			FrameID:   frameIDV1(envelope.AgentID, agentTimeValue, envelope.WALSequence),
			WALID:     walID,
			Sequence:  envelope.WALSequence,
			MessageID: envelope.MsgID,
			Dialect:   dialect,
		},
		Timing: TimingContext{
			EventTime:        agentTimeValue,
			RelayTime:        relayTime,
			AgentCaptureTime: agentTime,
			TimestampSource:  TimestampSourceAgent,
		},
		MessageName: canonicalName,
		Fields:      make(Fields),
	}, nil
}

// frameIDV1 retains the deployed schema-version-1 identity across mixed Relay
// versions. Changing this formula requires a new schema version and a
// coordinated drain so one WAL entry cannot be persisted under two identities.
func frameIDV1(agentID string, agentCaptureTime time.Time, walSequence uint64) string {
	return fmt.Sprintf("%d:%s:%d:%d", len(agentID), agentID, agentCaptureTime.UnixNano(), walSequence)
}
