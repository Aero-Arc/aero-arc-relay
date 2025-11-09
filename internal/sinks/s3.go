package sinks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// S3Sink implements Sink interface for AWS S3
type S3Sink struct {
	client   *s3.S3
	bucket   string
	prefix   string
	fileSink *FileSink
	mu       sync.Mutex
}

// NewS3Sink creates a new S3 sink
func NewS3Sink(cfg *config.S3Config) (*S3Sink, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(cfg.Region),
		Credentials: credentials.NewStaticCredentials(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	fileSink, err := NewFileSink(&config.FileConfig{
		Path:   fmt.Sprintf("/tmp/s3-sink-%v.log", time.Now().Unix()),
		Format: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create file sink: %w", err)
	}

	s := &S3Sink{
		client:   s3.New(sess),
		bucket:   cfg.Bucket,
		prefix:   cfg.Prefix,
		fileSink: fileSink,
	}

	go func() {
		timer := time.NewTimer(cfg.FlushInterval)
		for {
			select {
			case <-timer.C:
				s.RotateAndUpload()
			}
		}
	}()

	return s, nil
}

// WriteMessage uploads telemetry message to S3
func (s *S3Sink) WriteMessage(msg telemetry.TelemetryMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.fileSink.WriteMessage(msg)
}

// RotateFile rotates the file
func (s *S3Sink) RotateAndUpload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate object key with timestamp and message type
	key := s.fileSink.config.Path
	fileData, err := io.ReadAll(s.fileSink.file)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Upload to S3
	_, err = s.client.PutObjectWithContext(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(fileData),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	s.fileSink.rotateFile()

	return nil

	return s.fileSink.rotateFile()
}

// Close closes the S3 sink
func (s *S3Sink) Close() error {
	// S3 client doesn't need explicit closing
	return nil
}
