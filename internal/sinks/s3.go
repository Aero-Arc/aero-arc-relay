package sinks

import (
	"context"
	"fmt"
	"io"
	"log"
	"path"
	"path/filepath"
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
	client    *s3.S3
	bucket    string
	prefix    string
	fileSink  *FileSink
	mu        sync.Mutex
	closeChan chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup
	*BaseAsyncSink
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
		Path:               "/tmp",
		Prefix:             "s3-sink",
		Format:             "json",
		RotationInterval:   cfg.FlushInterval,
		QueueSize:          cfg.QueueSize,
		BackpressurePolicy: cfg.BackpressurePolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create file sink: %w", err)
	}

	s := &S3Sink{
		client:    s3.New(sess),
		bucket:    cfg.Bucket,
		prefix:    cfg.Prefix,
		fileSink:  fileSink,
		closeChan: make(chan struct{}),
	}
	s.BaseAsyncSink = NewBaseAsyncSink(cfg.QueueSize, cfg.BackpressurePolicy, "s3", s.handleMessage)

	s.wg.Add(1)
	go func(closeCh <-chan struct{}) {
		defer s.wg.Done()

		flushTicker := time.NewTicker(cfg.FlushInterval)
		defer flushTicker.Stop()

		for {
			select {
			case <-flushTicker.C:
				if err := s.RotateAndUpload(); err != nil {
					log.Printf("failed to rotate and upload file: %v", err)
				}
			case <-closeCh:
				return
			}
		}
	}(s.closeChan)

	return s, nil
}

// WriteMessage uploads telemetry message to S3
func (s *S3Sink) WriteMessage(msg telemetry.TelemetryMessage) error {
	return s.BaseAsyncSink.Enqueue(msg)
}

func (s *S3Sink) handleMessage(msg telemetry.TelemetryMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.fileSink.WriteMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to write message to file sink: %w", err)
	}

	return nil
}

// RotateFile rotates the file
func (s *S3Sink) RotateAndUpload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.uploadAndMaybeRotateLocked(true)
}

// Close closes the S3 sink
func (s *S3Sink) Close() error {
	s.stopOnce.Do(func() {
		close(s.closeChan)
	})

	s.wg.Wait()

	s.BaseAsyncSink.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.uploadAndMaybeRotateLocked(false); err != nil {
		return err
	}

	if err := s.fileSink.Close(); err != nil {
		return fmt.Errorf("failed to close file sink: %w", err)
	}

	return nil
}

func (s *S3Sink) uploadAndMaybeRotateLocked(rotate bool) error {
	f := s.fileSink

	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.flushLocked(); err != nil {
		return fmt.Errorf("failed to flush file sink: %w", err)
	}

	info, err := f.file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if info.Size() == 0 {
		return nil
	}

	if _, err := f.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek file: %w", err)
	}

	key := path.Join(s.prefix, filepath.Base(f.file.Name()))

	_, err = s.client.PutObjectWithContext(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        f.file,
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	if rotate {
		if err := f.rotateFileLocked(); err != nil {
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	return nil
}
