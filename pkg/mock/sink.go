package mock

import (
	"sync"

	"github.com/makinje/aero-arc-relay/internal/sinks"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// MockSink implements the Sink interface for testing
type MockSink struct {
	messages []telemetry.TelemetryMessage
	closed   bool
	mu       sync.RWMutex
}

// NewMockSink creates a new mock sink for testing
func NewMockSink() *MockSink {
	return &MockSink{
		messages: make([]telemetry.TelemetryMessage, 0),
		closed:   false,
	}
}

// WriteMessage implements the sinks.Sink interface
func (m *MockSink) WriteMessage(msg telemetry.TelemetryMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.messages = append(m.messages, msg)
	return nil
}

// Close implements the sinks.Sink interface
func (m *MockSink) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

// GetMessages returns a copy of all messages received by the sink
func (m *MockSink) GetMessages() []telemetry.TelemetryMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	messages := make([]telemetry.TelemetryMessage, len(m.messages))
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

	m.messages = make([]telemetry.TelemetryMessage, 0)
}

// IsClosed returns whether the sink has been closed
func (m *MockSink) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.closed
}

// GetMessagesByType returns messages filtered by message type
func (m *MockSink) GetMessagesByType(msgType string) []telemetry.TelemetryMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []telemetry.TelemetryMessage
	for _, msg := range m.messages {
		if msg.GetMessageType() == msgType {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// GetMessagesBySource returns messages filtered by source
func (m *MockSink) GetMessagesBySource(source string) []telemetry.TelemetryMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []telemetry.TelemetryMessage
	for _, msg := range m.messages {
		if msg.GetSource() == source {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// GetLastMessage returns the most recently received message
func (m *MockSink) GetLastMessage() telemetry.TelemetryMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.messages) == 0 {
		return nil
	}
	return m.messages[len(m.messages)-1]
}

// GetFirstMessage returns the first received message
func (m *MockSink) GetFirstMessage() telemetry.TelemetryMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.messages) == 0 {
		return nil
	}
	return m.messages[0]
}

// Assert that MockSink implements the sinks.Sink interface
var _ sinks.Sink = (*MockSink)(nil)
