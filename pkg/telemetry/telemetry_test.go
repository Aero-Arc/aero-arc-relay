package telemetry

import (
	"testing"
	"time"
)

func makeTestEnvelope(source, msgName string, fields map[string]any) TelemetryEnvelope {
	return TelemetryEnvelope{
		AgentID:        source,
		Source:         source,
		TimestampRelay: time.Now().UTC(),
		MsgName:        msgName,
		Fields:         fields,
	}
}

func TestTelemetryEnvelopeBasics(t *testing.T) {
	envelope := makeTestEnvelope("drone-1", "heartbeat", map[string]any{
		"status": "connected",
		"mode":   "AUTO",
	})

	if got := envelope.GetSource(); got != "drone-1" {
		t.Errorf("GetSource() = %q, want %q", got, "drone-1")
	}

	if got := envelope.GetMessageType(); got != "heartbeat" {
		t.Errorf("GetMessageType() = %q, want %q", got, "heartbeat")
	}

	jsonData, err := envelope.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}
	if len(jsonData) == 0 {
		t.Fatal("ToJSON() returned empty payload")
	}

	binaryData, err := envelope.ToBinary()
	if err != nil {
		t.Fatalf("ToBinary() error = %v", err)
	}
	if len(binaryData) == 0 {
		t.Fatal("ToBinary() returned empty payload")
	}

	timestamp := envelope.GetTimestamp()
	if timestamp.IsZero() {
		t.Fatal("GetTimestamp() returned zero value")
	}
}

func TestTelemetryEnvelopeImplementsTelemetryMessage(t *testing.T) {
	now := time.Now().UTC()
	messages := []TelemetryMessage{
		TelemetryEnvelope{
			AgentID:        "test",
			Source:         "test",
			TimestampRelay: now,
			MsgName:        "heartbeat",
			Fields: map[string]any{
				"status": "connected",
			},
		},
		TelemetryEnvelope{
			AgentID:        "test",
			Source:         "test",
			TimestampRelay: now,
			MsgName:        "position",
			Fields: map[string]any{
				"latitude":  37.7749,
				"longitude": -122.4194,
				"altitude":  100.5,
			},
		},
		TelemetryEnvelope{
			AgentID:        "test",
			Source:         "test",
			TimestampRelay: now,
			MsgName:        "attitude",
			Fields: map[string]any{
				"roll":  10.5,
				"pitch": -5.2,
				"yaw":   180.0,
			},
		},
		TelemetryEnvelope{
			AgentID:        "test",
			Source:         "test",
			TimestampRelay: now,
			MsgName:        "vfr_hud",
			Fields: map[string]any{
				"speed":   15.2,
				"heading": 180.0,
			},
		},
		TelemetryEnvelope{
			AgentID:        "test",
			Source:         "test",
			TimestampRelay: now,
			MsgName:        "battery",
			Fields: map[string]any{
				"battery": 85.5,
				"voltage": 12.6,
			},
		},
	}

	for i, msg := range messages {
		if msg.GetSource() != "test" {
			t.Errorf("message[%d].GetSource() = %q, want %q", i, msg.GetSource(), "test")
		}

		if msg.GetTimestamp().IsZero() {
			t.Errorf("message[%d].GetTimestamp() returned zero value", i)
		}

		if msg.GetMessageType() == "" {
			t.Errorf("message[%d].GetMessageType() returned empty string", i)
		}

		payload, err := msg.ToJSON()
		if err != nil {
			t.Errorf("message[%d].ToJSON() error = %v", i, err)
		}
		if len(payload) == 0 {
			t.Errorf("message[%d].ToJSON() returned empty payload", i)
		}

		blob, err := msg.ToBinary()
		if err != nil {
			t.Errorf("message[%d].ToBinary() error = %v", i, err)
		}
		if len(blob) == 0 {
			t.Errorf("message[%d].ToBinary() returned empty payload", i)
		}
	}
}
