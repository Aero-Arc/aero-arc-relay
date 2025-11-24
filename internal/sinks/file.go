package sinks

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	*BaseAsyncSink
}

// NewFileSink creates a new file sink
func NewFileSink(cfg *config.FileConfig) (*FileSink, error) {
	// Ensure directory exists
	if err := os.MkdirAll(cfg.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate filename with timestamp
	filename := generateFilename(cfg.Path, cfg.Prefix, cfg.Format)

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
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

	if cfg.RotationInterval == 0 {
		cfg.RotationInterval = 60 * time.Second
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 1000
	}
	if cfg.BackpressurePolicy == "" {
		cfg.BackpressurePolicy = "drop"
	}

	sink.BaseAsyncSink = NewBaseAsyncSink(cfg.QueueSize, cfg.BackpressurePolicy, "file", sink.handleMessage)

	return sink, nil
}

// WriteMessage writes telemetry message to file
func (f *FileSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	return f.BaseAsyncSink.Enqueue(msg)
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
	f.BaseAsyncSink.Close()

	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.flushLocked(); err != nil {
		return err
	}
	return f.file.Close()
}

func (f *FileSink) handleMessage(envelope telemetry.TelemetryEnvelope) error {

	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if rotation is needed
	if f.needsRotation() {
		if err := f.rotateFileLocked(); err != nil {
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	if envelope.Fields == nil {
		envelope.Fields = map[string]any{}
	}

	// Write message based on format
	switch f.config.Format {
	case "json":
		return f.writeJSON(envelope)
	case "csv":
		return f.writeCSV(envelope)
	case "binary":
		return f.writeBinary(envelope)
	default:
		return fmt.Errorf("unsupported format: %s", f.config.Format)
	}
}

// writeJSON writes message in JSON format
func (f *FileSink) writeJSON(msg telemetry.TelemetryEnvelope) error {
	jsonData, err := msg.ToJSON()
	if err != nil {
		return err
	}

	_, err = f.file.Write(append(jsonData, '\n'))
	return err
}

// writeCSV writes message in CSV format
func (f *FileSink) writeCSV(msg telemetry.TelemetryEnvelope) error {
	if f.writer == nil {
		return fmt.Errorf("csv writer not configured")
	}

	fieldsJSON, err := json.Marshal(msg.Fields)
	if err != nil {
		return fmt.Errorf("failed to marshal fields to JSON: %w", err)
	}

	row := []string{
		msg.TimestampRelay.Format(time.RFC3339Nano),
		strconv.FormatFloat(msg.TimestampDevice, 'f', -1, 64),
		msg.DroneID,
		msg.Source,
		msg.MsgName,
		strconv.FormatUint(uint64(msg.MsgID), 10),
		strconv.Itoa(int(msg.SystemID)),
		strconv.Itoa(int(msg.ComponentID)),
		strconv.FormatUint(uint64(msg.Sequence), 10),
		string(fieldsJSON),
		string(msg.Raw),
	}

	if err := f.writer.Write(row); err != nil {
		return err
	}

	f.writer.Flush()
	return f.writer.Error()
}

// writeBinary writes message in binary format
func (f *FileSink) writeBinary(msg telemetry.TelemetryEnvelope) error {
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
	switch format {
	case "json":
		ext = "json"
	case "csv":
		ext = "csv"
	case "binary":
		ext = "bin"
	default:
		return fmt.Sprintf("%s/%s_%d.%s", basePath, prefix, timestamp, format)
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

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
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
