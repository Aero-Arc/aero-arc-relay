package redisconn

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// DroneRoutingStore publishes routing metadata for a drone to Redis.
// This is intentionally narrow to avoid leaking the underlying Redis client.
type DroneRoutingStore interface {
	UpsertDroneRouting(ctx context.Context, droneID, relayID, sessionID string, ttl time.Duration) error
	DeleteDroneRouting(ctx context.Context, droneID string) error
}

// Client wraps a go-redis client and can be shared across components.
type Client struct {
	rdb *redis.Client
}

// Close closes the underlying Redis client.
func (c *Client) Close() error {
	if c.rdb == nil {
		return ErrRedisClientUninitialized
	}
	return c.rdb.Close()
}

// Ping checks connectivity to Redis.
func (c *Client) Ping(ctx context.Context) error {
	if c.rdb == nil {
		return ErrRedisClientUninitialized
	}
	return c.rdb.Ping(ctx).Err()
}

// NewClientFromEnv creates a Redis client from environment variables.
//
// Environment variables:
//   - REDIS_ADDR      (required to enable Redis, e.g. "localhost:6379")
//   - REDIS_PASSWORD  (optional)
//   - REDIS_DB        (optional, integer DB index; defaults to 0)
//
// Behaviour:
//   - If REDIS_ADDR is not set, ErrRedisAddrNotSet is returned.
//   - If ping fails, the client is returned alongside an error.
func NewClientFromEnv(ctx context.Context) (*Client, error) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		return nil, ErrRedisAddrNotSet
	}

	password := os.Getenv("REDIS_PASSWORD")
	db := 0
	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		parsed, err := strconv.Atoi(dbStr)
		if err != nil || parsed < 0 {
			slog.LogAttrs(ctx, slog.LevelWarn, "Invalid REDIS_DB", slog.String("value", dbStr))
			return nil, fmt.Errorf("%w: %q", ErrRedisDBInvalid, dbStr)
		}
		db = parsed
	}

	opts := &redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	}

	rdb := redis.NewClient(opts)

	// Perform a short ping on startup to surface connectivity issues without
	// crashing the relay.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client := &Client{rdb: rdb}
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return client, fmt.Errorf("%w: %v", ErrRedisPingFailed, err)
	}
	return client, nil
}

type droneRoutingValue struct {
	RelayID   string `json:"relay_id"`
	SessionID string `json:"session_id"`
}

func droneRoutingKey(droneID string) string {
	return "drone:" + droneID
}

// UpsertDroneRouting writes drone routing metadata with a TTL.
// It is safe to call repeatedly; each call refreshes the TTL.
func (c *Client) UpsertDroneRouting(ctx context.Context, droneID, relayID, sessionID string, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	if droneID == "" {
		return fmt.Errorf("drone_id is required")
	}
	if relayID == "" {
		return fmt.Errorf("relay_id is required")
	}
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if ttl <= 0 {
		return fmt.Errorf("ttl must be > 0")
	}

	payload, err := json.Marshal(droneRoutingValue{RelayID: relayID, SessionID: sessionID})
	if err != nil {
		return err
	}

	opCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	return c.rdb.Set(opCtx, droneRoutingKey(droneID), payload, ttl).Err()
}

// DeleteDroneRouting best-effort deletes drone routing metadata.
// TTL-based expiry still acts as a safety net on crashes.
func (c *Client) DeleteDroneRouting(ctx context.Context, droneID string) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	if droneID == "" {
		return fmt.Errorf("drone_id is required")
	}
	opCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	return c.rdb.Del(opCtx, droneRoutingKey(droneID)).Err()
}

// NOTE: DB parsing intentionally uses strconv; this runs only at startup and should stay readable.
