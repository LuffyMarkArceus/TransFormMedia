// Package lease provides a Redis-backed implementation of the worker's
// LeaseStore: per-item claim leases plus a retry budget with escalating
// backoff, all kept in the same Redis the cache already uses.
package lease

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Held for this long an item stays claimed after the worker crashed mid-job,
// after which another replica can pick it up again.
const holdTTL = 10 * time.Minute

// After this long without a successful run the retry counter forgets an
// item's history, effectively resetting the budget for a genuinely stuck job.
const memoryTTL = time.Hour

// RedisLease implements worker.LeaseStore on a shared *redis.Client.
type RedisLease struct {
	client    *redis.Client
	retryBase time.Duration
}

func NewRedisLease(client *redis.Client, retryBase time.Duration) *RedisLease {
	return &RedisLease{client: client, retryBase: retryBase}
}

func (l *RedisLease) Acquire(ctx context.Context, key string) (bool, error) {
	return l.client.SetNX(ctx, key, "1", holdTTL).Result()
}

func (l *RedisLease) Release(ctx context.Context, key string) error {
	return l.client.Del(ctx, key).Err()
}

func (l *RedisLease) PendingRetry(ctx context.Context, mediaID string) (bool, error) {
	n, err := l.client.Exists(ctx, retryGateKey(mediaID)).Result()
	return n > 0, err
}

// IncRetry bumps the attempt counter and arms the backoff gate for the next
// attempt. Backoff doubles per attempt (30s, 60s, 120s, ...) capped at 15m.
func (l *RedisLease) IncRetry(ctx context.Context, mediaID string) (int, error) {
	n, err := l.client.Incr(ctx, attemptKey(mediaID)).Result()
	if err != nil {
		return 0, err
	}
	if err := l.client.Expire(ctx, attemptKey(mediaID), memoryTTL).Err(); err != nil {
		return int(n), err
	}

	backoff := backoffFor(l.retryBase, int(n))
	err = l.client.Set(ctx, retryGateKey(mediaID), int64(backoff.Seconds()), backoff).Err()
	return int(n), err
}

func (l *RedisLease) ResetRetry(ctx context.Context, mediaID string) error {
	if err := l.client.Del(ctx, attemptKey(mediaID)).Err(); err != nil {
		return err
	}
	return l.client.Del(ctx, retryGateKey(mediaID)).Err()
}

func attemptKey(mediaID string) string {
	return "wattempts:" + mediaID
}

func retryGateKey(mediaID string) string {
	return "wretry:" + mediaID
}

// backoffFor doubles the base per attempt, capping at 15 minutes so a
// flapping item never stalls a small fleet for very long.
func backoffFor(base time.Duration, attempt int) time.Duration {
	shift := attempt - 1
	if shift > 4 {
		shift = 4
	}
	d := base << shift
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d
}
