package redisconn

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps a go-redis client and can be shared across components.
type Client struct {
	rdb *redis.Client
}

// global holds the process-wide Redis client instance, if configured.
var global *Client

// InitFromEnv initialises a Redis client from environment variables.
//
// Environment variables:
//   - REDIS_ADDR      (required to enable Redis, e.g. "localhost:6379")
//   - REDIS_PASSWORD  (optional)
//   - REDIS_DB        (optional, integer DB index; defaults to 0)
//
// Behaviour:
//   - If REDIS_ADDR is not set, Redis is treated as disabled and nil is returned.
//   - If connection or ping fails, a warning is logged but the relay continues
//     running; the client is still returned so components can implement their
//     own retry/backoff logic.
func InitFromEnv(ctx context.Context) *Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		slog.LogAttrs(ctx, slog.LevelInfo, "Redis disabled (REDIS_ADDR not set)")
		return nil
	}

	password := os.Getenv("REDIS_PASSWORD")
	db := 0
	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		// Ignore parse errors and keep db=0; this avoids crashing on bad input.
		if parsed, err := parseDB(dbStr); err == nil {
			db = parsed
		} else {
			slog.LogAttrs(ctx, slog.LevelWarn, "Invalid REDIS_DB value, defaulting to 0", slog.String("error", err.Error()))
		}
	}

	opts := &redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	}

	rdb := redis.NewClient(opts)

	// Perform a short ping on startup to surface connectivity issues without
	// crashing the relay.
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.LogAttrs(ctx, slog.LevelWarn, "Redis ping failed; continuing without aborting relay", slog.String("error", err.Error()))
	}

	client := &Client{rdb: rdb}
	global = client
	return client
}

// Get returns the process-wide Redis client, if initialised.
// It may be nil when Redis is disabled or misconfigured.
func Get() *Client {
	return global
}

// parseDB converts a REDIS_DB string into an integer index.
func parseDB(value string) (int, error) {
	// Small, local parse to avoid pulling in strconv here unnecessarily.
	var n int
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("non-digit character %q in DB index", ch)
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}
