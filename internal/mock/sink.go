package mock

import (
	"context"
	"sync"

	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// MockSink implements the Sink interface for testing
type MockSink struct {
	messages []telemetry.TelemetryEnvelope
	closed   bool
	mu       sync.RWMutex
}

// NewMockSink creates a new mock sink for testing
func NewMockSink() *MockSink {
	return &MockSink{
		messages: make([]telemetry.TelemetryEnvelope, 0),
		closed:   false,
	}
}

// WriteMessage implements the sinks.Sink interface
func (m *MockSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.messages = append(m.messages, msg)
	return nil
}

// Close implements the sinks.Sink interface
func (m *MockSink) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

// GetMessages returns a copy of all messages received by the sink
func (m *MockSink) GetMessages() []telemetry.TelemetryEnvelope {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	messages := make([]telemetry.TelemetryEnvelope, len(m.messages))
	copy(messages, m.messages)
	return messages
}

// GetMessageCount returns the number of messages received
func (m *MockSink) GetMessageCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.messages)
}

// Clear removes all messages from the sink
func (m *MockSink) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = make([]telemetry.TelemetryEnvelope, 0)
}

// IsClosed returns whether the sink has been closed
func (m *MockSink) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.closed
}

// GetMessagesBySource returns messages filtered by source
func (m *MockSink) GetMessagesBySource(source string) []telemetry.TelemetryEnvelope {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []telemetry.TelemetryEnvelope
	for _, msg := range m.messages {
		if msg.Source == source {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// GetLastMessage returns the most recently received message
func (m *MockSink) GetLastMessage() telemetry.TelemetryEnvelope {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.messages) == 0 {
		return telemetry.TelemetryEnvelope{}
	}
	return m.messages[len(m.messages)-1]
}

// GetFirstMessage returns the first received message
func (m *MockSink) GetFirstMessage() telemetry.TelemetryEnvelope {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.messages) == 0 {
		return telemetry.TelemetryEnvelope{}
	}
	return m.messages[0]
}

// MockSink implements the Sink interface for testing
// The interface is defined in internal/sinks/sink.go
