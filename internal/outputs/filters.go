package outputs

import "strings"

const wildcardMessage = "*"

// MessageFilter controls which normalized MAVLink message names reach a
// consumer. Empty include lists mean "include everything".
type MessageFilter struct {
	Include []string
	Exclude []string
}

func (f MessageFilter) Allows(messageName string) bool {
	normalized := NormalizeMessageName(messageName)
	if normalized == "" {
		return false
	}
	if containsMessage(f.Exclude, normalized) {
		return false
	}
	if len(f.Include) == 0 {
		return true
	}
	return containsMessage(f.Include, normalized)
}

func containsMessage(messages []string, messageName string) bool {
	for _, item := range messages {
		normalized := NormalizeMessageName(item)
		if normalized == wildcardMessage || normalized == messageName {
			return true
		}
	}
	return false
}

// NormalizeMessageName collapses Go type strings, pointer prefixes, and package
// paths to the stable MAVLink message name used by filters.
func NormalizeMessageName(messageName string) string {
	name := strings.TrimSpace(messageName)
	if name == "" {
		return ""
	}
	if name == wildcardMessage {
		return wildcardMessage
	}
	for strings.HasPrefix(name, "*") {
		name = strings.TrimPrefix(name, "*")
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.TrimPrefix(name, "Message")
	return name
}
