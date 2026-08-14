/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

package outputs

import (
	"strings"
	"unicode"
)

const wildcardMessage = "*"

// MessageFilter controls which normalized MAVLink message names reach a
// consumer. Empty include lists match nothing; use "*" to include everything.
type MessageFilter struct {
	Include []string
	Exclude []string
}

func (f MessageFilter) hasIncludes() bool {
	for _, message := range f.Include {
		if NormalizeMessageName(message) != "" {
			return true
		}
	}
	return false
}

// Allows reports whether the filter permits the supplied telemetry envelope.
//
// Parameters:
//   - messageName: is the string value supplied to Allows.
//
// Returns:
//   - bool: reports whether the requested condition was satisfied.
func (f MessageFilter) Allows(messageName string) bool {
	if !f.hasIncludes() {
		return false
	}
	normalized := NormalizeMessageName(messageName)
	if containsMessage(f.Exclude, normalized) {
		return false
	}
	return containsMessage(f.Include, normalized)
}

func containsMessage(messages []string, messageName string) bool {
	for _, item := range messages {
		normalized := NormalizeMessageName(item)
		if normalized == "" {
			continue
		}
		if normalized == wildcardMessage || normalized == messageName {
			return true
		}
	}
	return false
}

// NormalizeMessageName collapses Go type strings, pointer prefixes, package
// paths, and legacy aliases to the canonical lower-snake-case MAVLink message
// name used by filters and the internal normalizer registry.
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
	const goTypePrefix = "message"
	if len(name) >= len(goTypePrefix) && strings.EqualFold(name[:len(goTypePrefix)], goTypePrefix) {
		name = name[len(goTypePrefix):]
	}
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")

	var normalized strings.Builder
	runes := []rune(name)
	for i, current := range runes {
		if current == '_' {
			if normalized.Len() > 0 && !strings.HasSuffix(normalized.String(), "_") {
				normalized.WriteRune('_')
			}
			continue
		}
		if unicode.IsUpper(current) {
			if i > 0 && runes[i-1] != '_' &&
				(unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) ||
					(i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				normalized.WriteRune('_')
			}
			normalized.WriteRune(unicode.ToLower(current))
			continue
		}
		normalized.WriteRune(unicode.ToLower(current))
	}

	result := strings.Trim(normalized.String(), "_")
	switch result {
	case "system_status":
		return "sys_status"
	case "gpsraw_int":
		return "gps_raw_int"
	default:
		return result
	}
}
