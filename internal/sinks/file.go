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
	config   *config.FileConfig
	file     *os.File
	writer   *csv.Writer
	mu       sync.Mutex
	rotation *FileRotation
}

// FileRotation handles file rotation logic
type FileRotation struct {
	lastRotation time.Time
	rotationType string
}

// NewFileSink creates a new file sink
func NewFileSink(cfg *config.FileConfig) (*FileSink, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate filename with timestamp
	filename := generateFilename(cfg.Path, cfg.Format)

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	sink := &FileSink{
		config: cfg,
		file:   file,
		rotation: &FileRotation{
			lastRotation: time.Now(),
			rotationType: cfg.Rotation,
		},
	}

	// Initialize writer based on format
	if cfg.Format == "csv" {
		sink.writer = csv.NewWriter(file)
	}

	return sink, nil
}

// Write writes telemetry data to file
func (f *FileSink) Write(data *telemetry.Data) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if rotation is needed
	if f.needsRotation() {
		if err := f.rotateFile(); err != nil {
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	// Write data based on format
	switch f.config.Format {
	case "json":
		return f.writeJSON(data)
	case "csv":
		return f.writeCSV(data)
	case "binary":
		return f.writeBinary(data)
	default:
		return fmt.Errorf("unsupported format: %s", f.config.Format)
	}
}

// Close closes the file sink
func (f *FileSink) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.writer != nil {
		f.writer.Flush()
	}
	return f.file.Close()
}

// writeJSON writes data in JSON format
func (f *FileSink) writeJSON(data *telemetry.Data) error {
	jsonData, err := data.ToJSON()
	if err != nil {
		return err
	}

	_, err = f.file.Write(append(jsonData, '\n'))
	return err
}

// writeCSV writes data in CSV format
func (f *FileSink) writeCSV(data *telemetry.Data) error {
	// Convert telemetry data to CSV row
	row := []string{
		data.Timestamp.Format(time.RFC3339),
		data.Source,
		fmt.Sprintf("%.6f", data.Latitude),
		fmt.Sprintf("%.6f", data.Longitude),
		fmt.Sprintf("%.2f", data.Altitude),
		fmt.Sprintf("%.2f", data.Speed),
		fmt.Sprintf("%.2f", data.Heading),
	}

	return f.writer.Write(row)
}

// writeBinary writes data in binary format
func (f *FileSink) writeBinary(data *telemetry.Data) error {
	binaryData, err := data.ToBinary()
	if err != nil {
		return err
	}

	_, err = f.file.Write(binaryData)
	return err
}

// needsRotation checks if file rotation is needed
func (f *FileSink) needsRotation() bool {
	switch f.rotation.rotationType {
	case "daily":
		return time.Since(f.rotation.lastRotation) >= 24*time.Hour
	case "hourly":
		return time.Since(f.rotation.lastRotation) >= time.Hour
	default:
		return false
	}
}

// rotateFile performs file rotation
func (f *FileSink) rotateFile() error {
	// Close current file
	if f.writer != nil {
		f.writer.Flush()
	}
	f.file.Close()

	// Generate new filename
	filename := generateFilename(f.config.Path, f.config.Format)

	// Open new file
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	f.file = file
	if f.config.Format == "csv" {
		f.writer = csv.NewWriter(file)
	}
	f.rotation.lastRotation = time.Now()

	return nil
}

// generateFilename creates a filename with timestamp
func generateFilename(basePath, format string) string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	ext := format
	if format == "json" {
		ext = "json"
	} else if format == "csv" {
		ext = "csv"
	} else if format == "binary" {
		ext = "bin"
	}

	return fmt.Sprintf("%s_%s.%s", basePath, timestamp, ext)
}
