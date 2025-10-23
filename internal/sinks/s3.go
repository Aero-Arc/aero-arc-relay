package sinks

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
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
	client *s3.S3
	bucket string
	prefix string
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

	return &S3Sink{
		client: s3.New(sess),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

// Write uploads telemetry data to S3
func (s *S3Sink) Write(data *telemetry.Data) error {
	// Serialize data to JSON
	jsonData, err := data.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize data: %w", err)
	}

	// Generate object key with timestamp
	timestamp := time.Now().UTC()
	key := filepath.Join(s.prefix, timestamp.Format("2006/01/02"),
		fmt.Sprintf("%s_%d.json", data.Source, timestamp.Unix()))

	// Upload to S3
	_, err = s.client.PutObjectWithContext(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(jsonData),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// Close closes the S3 sink
func (s *S3Sink) Close() error {
	// S3 client doesn't need explicit closing
	return nil
}
