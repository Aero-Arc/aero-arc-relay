package influx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/makinje/aero-arc-relay/internal/telemetrynormalize"
)

type Config struct {
	Host     string
	Token    string
	Database string
	Timeout  time.Duration
}

type pointWriter interface {
	WritePoints(context.Context, []*influxdb3.Point, ...influxdb3.WriteOption) error
	Close() error
}

type Backend struct {
	client pointWriter
}

// New constructs influx from the supplied configuration and dependencies.
//
// Parameters:
//   - config: provides the configuration values used to initialize or execute the operation.
//
// Returns:
//   - result: is the *Backend value produced by New.
//   - error: reports validation, dependency, cancellation, or persistence failures.
func New(config Config) (*Backend, error) {
	if config.Host == "" || config.Token == "" || config.Database == "" {
		return nil, errors.New("InfluxDB 3 host, token, and database are required")
	}
	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     config.Host,
		Token:    config.Token,
		Database: config.Database,
		Timeout:  config.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create InfluxDB 3 client: %w", err)
	}
	return &Backend{client: client}, nil
}

func newWithClient(client pointWriter) *Backend { return &Backend{client: client} }

// WriteBatch writes the supplied data through Backend.
//
// Parameters:
//   - ctx: controls cancellation and deadlines for the operation.
//   - records: is the []telemetrynormalize.Record value supplied to WriteBatch.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (b *Backend) WriteBatch(ctx context.Context, records []telemetrynormalize.Record) error {
	if len(records) == 0 {
		return nil
	}
	points := make([]*influxdb3.Point, 0, len(records))
	for _, record := range records {
		point, err := recordToPoint(record)
		if err != nil {
			return err
		}
		points = append(points, point)
	}
	if err := b.client.WritePoints(ctx, points); err != nil {
		return fmt.Errorf("write InfluxDB 3 points: %w", err)
	}
	return nil
}

// Close releases resources owned by Backend and completes any required shutdown work.
//
// Parameters:
//   - ctx: is accepted for interface compatibility; the in-memory operation completes synchronously.
//
// Returns:
//   - error: reports validation, dependency, cancellation, or persistence failures.
func (b *Backend) Close(context.Context) error {
	if err := b.client.Close(); err != nil {
		return fmt.Errorf("close InfluxDB 3 client: %w", err)
	}
	return nil
}
