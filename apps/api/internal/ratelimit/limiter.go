package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	client *redis.Client
	script *redis.Script
}

// NewFromURL connects and verifies Redis before returning. A missing or broken
// limiter must make social writes fail closed, not open.
func NewFromURL(ctx context.Context, rawURL string) (*Limiter, error) {
	if rawURL == "" {
		return nil, errors.New("redis url is empty")
	}
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return New(client), nil
}

func New(client *redis.Client) *Limiter {
	return &Limiter{
		client: client,
		script: redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
if capacity <= 0 or refill <= 0 or cost <= 0 then
  return 0
end
local now_parts = redis.call('TIME')
local now = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local values = redis.call('HMGET', key, 'tokens', 'last')
local tokens = tonumber(values[1])
local last = tonumber(values[2])
if tokens == nil then tokens = capacity end
if last == nil then last = now end
if now > last then
  tokens = math.min(capacity, tokens + (now - last) / 1000.0 * refill)
end
local allowed = 0
if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
end
redis.call('HSET', key, 'tokens', tostring(tokens), 'last', tostring(now))
local ttl = math.ceil(capacity / refill * 1000 * 2)
if ttl < 1000 then ttl = 1000 end
redis.call('PEXPIRE', key, ttl)
return allowed
`),
	}
}

// Allow atomically consumes cost tokens from key. When Redis is unavailable it
// returns an error; callers must reject writes instead of allowing them.
func (l *Limiter) Allow(ctx context.Context, key string, capacity int64, refillPerSecond float64, cost int64) (bool, error) {
	if l == nil || l.client == nil {
		return false, errors.New("rate limiter unavailable")
	}
	if key == "" || capacity <= 0 || refillPerSecond <= 0 || cost <= 0 {
		return false, errors.New("invalid rate limit arguments")
	}
	n, err := l.script.Run(ctx, l.client, []string{"rl:" + key}, capacity, refillPerSecond, cost).Int()
	if err != nil {
		return false, fmt.Errorf("redis rate limit: %w", err)
	}
	return n == 1, nil
}

func (l *Limiter) Ping(ctx context.Context) error {
	if l == nil || l.client == nil {
		return errors.New("rate limiter unavailable")
	}
	return l.client.Ping(ctx).Err()
}

func (l *Limiter) Close() error {
	if l == nil || l.client == nil {
		return nil
	}
	return l.client.Close()
}
