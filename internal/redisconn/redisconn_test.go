package redisconn

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestNewClientFromEnv_ErrRedisAddrNotSet(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "")

	client, err := NewClientFromEnv(context.Background())
	if client != nil {
		t.Fatalf("expected nil client, got %#v", client)
	}
	if !errors.Is(err, ErrRedisAddrNotSet) {
		t.Fatalf("expected ErrRedisAddrNotSet, got %v", err)
	}
}

func TestNewClientFromEnv_ErrRedisDBInvalid(t *testing.T) {
	t.Setenv("REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "not-an-int")

	client, err := NewClientFromEnv(context.Background())
	if client != nil {
		t.Fatalf("expected nil client, got %#v", client)
	}
	if !errors.Is(err, ErrRedisDBInvalid) {
		t.Fatalf("expected ErrRedisDBInvalid, got %v", err)
	}
}

func TestNewClientFromEnv_PingFailureReturnsClientAndErrRedisPingFailed(t *testing.T) {
	// Pick a local port and close it so connection should reliably fail.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	t.Setenv("REDIS_ADDR", addr)
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "0")

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	client, err := NewClientFromEnv(ctx)
	if client == nil {
		t.Fatalf("expected non-nil client on ping failure")
	}
	if !errors.Is(err, ErrRedisPingFailed) {
		t.Fatalf("expected ErrRedisPingFailed, got %v", err)
	}
}

func TestClient_PingAndClose_Uninitialized(t *testing.T) {
	var c Client // rdb is nil

	if err := c.Ping(context.Background()); !errors.Is(err, ErrRedisClientUninitialized) {
		t.Fatalf("expected ErrRedisClientUninitialized, got %v", err)
	}
	if err := c.Close(); !errors.Is(err, ErrRedisClientUninitialized) {
		t.Fatalf("expected ErrRedisClientUninitialized, got %v", err)
	}
}
