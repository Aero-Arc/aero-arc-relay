package telemetrynormalize

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func fieldString(fields map[string]any, name string) (string, bool) {
	value, ok := fields[name]
	if !ok || value == nil {
		return "", false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text), true
	}
	return strings.TrimSpace(fmt.Sprint(value)), true
}

func requiredInt64(fields map[string]any, name string) (int64, error) {
	text, ok := fieldString(fields, name)
	if !ok || text == "" {
		return 0, fmt.Errorf("required field %s is missing", name)
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse required field %s: %w", name, err)
	}
	return value, nil
}

func optionalInt64(fields map[string]any, name string) (int64, bool) {
	text, ok := fieldString(fields, name)
	if !ok || text == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(text, 10, 64)
	return value, err == nil
}

func optionalUint64(fields map[string]any, name string) (uint64, bool) {
	text, ok := fieldString(fields, name)
	if !ok || text == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(text, 10, 64)
	return value, err == nil
}

func optionalFloat64(fields map[string]any, name string) (float64, bool) {
	text, ok := fieldString(fields, name)
	if !ok || text == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func optionalEnum(fields map[string]any, name string) (string, bool) {
	text, ok := fieldString(fields, name)
	if !ok || text == "" {
		return "", false
	}
	return strings.ToLower(text), true
}

func optionalUint16Array(fields map[string]any, name string) ([]uint16, bool) {
	text, ok := fieldString(fields, name)
	if !ok {
		return nil, false
	}
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return nil, false
	}
	text = strings.TrimSpace(text[1 : len(text)-1])
	if text == "" {
		return []uint16{}, true
	}
	parts := strings.Split(text, ",")
	values := make([]uint16, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseUint(strings.TrimSpace(part), 10, 16)
		if err != nil {
			return nil, false
		}
		values = append(values, uint16(value))
	}
	return values, true
}
