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

func (b *Backend) Close(context.Context) error {
	if err := b.client.Close(); err != nil {
		return fmt.Errorf("close InfluxDB 3 client: %w", err)
	}
	return nil
}
