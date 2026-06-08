package cache

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisCache(redisURL string, ttl time.Duration) (*RedisCache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis url: %w", err)
	}

	client := redis.NewClient(opts)

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	log.Printf("Connected to Redis (TTL: %v)", ttl)
	return &RedisCache{client: client, ttl: ttl}, nil
}

func processCacheKey(mediaID string, width, height, quality int, format string) string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s:%d:%d:%d:%s", mediaID, width, height, quality, format)))
	return "proc:" + hex.EncodeToString(h.Sum(nil))
}

func (r *RedisCache) GetProcessed(ctx context.Context, mediaID string, width, height, quality int, format string) ([]byte, bool, error) {
	key := processCacheKey(mediaID, width, height, quality, format)
	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (r *RedisCache) SetProcessed(ctx context.Context, mediaID string, width, height, quality int, format string, data []byte) error {
	key := processCacheKey(mediaID, width, height, quality, format)
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
