package sinks

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// FileSink implements Sink interface for file-based storage
type FileSink struct {
	config       *config.FileConfig
	file         *os.File
	writer       *csv.Writer
	mu           sync.Mutex
	lastRotation time.Time
}

// NewFileSink creates a new file sink
func NewFileSink(cfg *config.FileConfig) (*FileSink, error) {
	// Ensure directory exists
	if err := os.MkdirAll(cfg.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate filename with timestamp
	filename := generateFilename(cfg.Path, cfg.Prefix, cfg.Format)

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	sink := &FileSink{
		config:       cfg,
		file:         file,
		lastRotation: time.Now(),
	}

	// Initialize writer based on format
	if cfg.Format == "csv" {
		sink.writer = csv.NewWriter(file)
	}

	return sink, nil
}

// WriteMessage writes telemetry message to file
func (f *FileSink) WriteMessage(msg telemetry.TelemetryMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if rotation is needed
	if f.needsRotation() {
		if err := f.rotateFileLocked(); err != nil {
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	// Write message based on format
	switch f.config.Format {
	case "json":
		return f.writeJSON(msg)
	case "csv":
		return f.writeCSV(msg)
	case "binary":
		return f.writeBinary(msg)
	default:
		return fmt.Errorf("unsupported format: %s", f.config.Format)
	}
}

// GetFilename returns the filename of the file sink
func (f *FileSink) GetFilename() string {
	return filepath.Base(f.file.Name())
}

// GetPath returns the path of the file sink
func (f *FileSink) GetPath() string {
	return f.config.Path
}

// GetPrefix returns the prefix of the file sink
func (f *FileSink) GetPrefix() string {
	return f.config.Prefix
}

// GetFormat returns the format of the file sink
func (f *FileSink) GetFormat() string {
	return f.config.Format
}

// GetRotationInterval returns the rotation interval of the file sink
func (f *FileSink) GetRotationInterval() time.Duration {
	return f.config.RotationInterval
}

// GetLastRotation returns the last rotation time of the file sink
func (f *FileSink) GetLastRotation() time.Time {
	return f.lastRotation
}

// Close closes the file sink
func (f *FileSink) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.flushLocked(); err != nil {
		return err
	}
	return f.file.Close()
}

// writeJSON writes message in JSON format
func (f *FileSink) writeJSON(msg telemetry.TelemetryMessage) error {
	jsonData, err := msg.ToJSON()
	if err != nil {
		return err
	}

	_, err = f.file.Write(append(jsonData, '\n'))
	return err
}

// writeCSV writes message in CSV format
func (f *FileSink) writeCSV(msg telemetry.TelemetryMessage) error {
	// Convert telemetry message to CSV row based on message type
	var row []string

	switch m := msg.(type) {
	case *telemetry.HeartbeatMessage:
		row = []string{
			m.Timestamp.Format(time.RFC3339),
			m.Source,
			m.GetMessageType(),
			m.Status,
			m.Mode,
		}
	case *telemetry.PositionMessage:
		row = []string{
			m.Timestamp.Format(time.RFC3339),
			m.Source,
			m.GetMessageType(),
			fmt.Sprintf("%.6f", m.Latitude),
			fmt.Sprintf("%.6f", m.Longitude),
			fmt.Sprintf("%.2f", m.Altitude),
		}
	case *telemetry.AttitudeMessage:
		row = []string{
			m.Timestamp.Format(time.RFC3339),
			m.Source,
			m.GetMessageType(),
			fmt.Sprintf("%.2f", m.Roll),
			fmt.Sprintf("%.2f", m.Pitch),
			fmt.Sprintf("%.2f", m.Yaw),
		}
	case *telemetry.VfrHudMessage:
		row = []string{
			m.Timestamp.Format(time.RFC3339),
			m.Source,
			m.GetMessageType(),
			fmt.Sprintf("%.2f", m.Speed),
			fmt.Sprintf("%.2f", m.Altitude),
			fmt.Sprintf("%.2f", m.Heading),
		}
	case *telemetry.BatteryMessage:
		row = []string{
			m.Timestamp.Format(time.RFC3339),
			m.Source,
			m.GetMessageType(),
			fmt.Sprintf("%.2f", m.Battery),
			fmt.Sprintf("%.2f", m.Voltage),
		}
	default:
		// Generic row for unknown message types
		row = []string{
			msg.GetTimestamp().Format(time.RFC3339),
			msg.GetSource(),
			msg.GetMessageType(),
		}
	}

	return f.writer.Write(row)
}

// writeBinary writes message in binary format
func (f *FileSink) writeBinary(msg telemetry.TelemetryMessage) error {
	binaryData, err := msg.ToBinary()
	if err != nil {
		return err
	}

	_, err = f.file.Write(binaryData)
	return err
}

// needsRotation checks if file rotation is needed
func (f *FileSink) needsRotation() bool {
	return time.Since(f.lastRotation) >= f.config.RotationInterval
}

// rotateFile performs file rotation
func (f *FileSink) rotateFile() error {
	return f.rotateFileLocked()
}

// generateFilename creates a filename with timestamp
func generateFilename(basePath, prefix, format string) string {
	timestamp := time.Now().UTC().Unix()
	ext := format
	if format == "json" {
		ext = "json"
	} else if format == "csv" {
		ext = "csv"
	} else if format == "binary" {
		ext = "bin"
	}

	return fmt.Sprintf("%s/%s_%d.%s", basePath, prefix, timestamp, ext)
}

func (f *FileSink) flushLocked() error {
	if f.writer != nil {
		f.writer.Flush()
		if err := f.writer.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileSink) rotateFileLocked() error {
	if err := f.flushLocked(); err != nil {
		return err
	}
	if err := f.file.Close(); err != nil {
		return err
	}

	filename := generateFilename(f.config.Path, f.config.Prefix, f.config.Format)

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	f.file = file
	if f.config.Format == "csv" {
		f.writer = csv.NewWriter(file)
	}
	f.lastRotation = time.Now()

	return nil
}
