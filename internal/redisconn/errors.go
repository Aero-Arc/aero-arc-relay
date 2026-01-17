package redisconn

import "errors"

var (
	// ErrRedisAddrNotSet indicates the Redis address was not provided.
	// The caller can decide whether this means "Redis disabled" or is fatal.
	ErrRedisAddrNotSet = errors.New("redis address not set (REDIS_ADDR)")

	// ErrRedisDBInvalid indicates REDIS_DB is present but not a valid integer DB index.
	ErrRedisDBInvalid = errors.New("redis db invalid (REDIS_DB)")

	// ErrRedisPingFailed indicates the initial connectivity check to Redis failed.
	ErrRedisPingFailed = errors.New("redis ping failed")

	// ErrRedisClientUninitialized indicates the Client has no underlying redis.Client.
	ErrRedisClientUninitialized = errors.New("redis client is uninitialized")
)
