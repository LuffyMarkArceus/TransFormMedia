package cache

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"

	"universal-media-service/core/image"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewClient dials Redis once. The single client is shared across the cache,
// the worker lease store, and the SSE event bus, so the process keeps one
// connection pool instead of one per subsystem.
func NewClient(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis url: %w", err)
	}

	client := redis.NewClient(opts)

	if err := client.Ping(context.Background()).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return client, nil
}

func NewRedisCacheFromClient(client *redis.Client, ttl time.Duration) *RedisCache {
	return &RedisCache{client: client, ttl: ttl}
}

// processCacheKey fingerprints both the transform options and a content
// version (the row's updated_at) so two requests that differ in width,
// gravity, blur, etc. never share an entry — and, crucially, a media record
// that was replaced is never served previously cached bytes derived from the
// old content. The key embeds mediaID and version so per-media invalidation
// (InvalidateMedia) and inspection can scan for it.
func processCacheKey(mediaID, version string, opts image.ProcessOptions) string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s:%s:%+v", mediaID, version, opts)))
	return "proc:" + mediaID + ":" + version + ":" + hex.EncodeToString(h.Sum(nil))
}

func (r *RedisCache) GetProcessed(ctx context.Context, mediaID, version string, opts image.ProcessOptions) ([]byte, bool, error) {
	key := processCacheKey(mediaID, version, opts)
	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (r *RedisCache) SetProcessed(ctx context.Context, mediaID, version string, opts image.ProcessOptions, data []byte) error {
	key := processCacheKey(mediaID, version, opts)
	return r.client.Set(ctx, key, data, r.ttl).Err()
}

func (r *RedisCache) InvalidateMedia(ctx context.Context, mediaID string) error {
	var cursor uint64
	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, fmt.Sprintf("proc:%s:*", mediaID), 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

func (r *RedisCache) Close() error {
	return r.client.Close()
}
