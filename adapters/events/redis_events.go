// Package events implements the runtime event bus on Redis: per-user
// pub/sub channels for status pushes (consumed by SSE in the HTTP layer) and
// short-lived tokens that a browser's EventSource can present in exchange for
// a subscription.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	coreevents "universal-media-service/core/events"

	"github.com/redis/go-redis/v9"
)

// ErrTokenNotFound is returned by Resolve when a token is unknown, expired,
// or already consumed.
var ErrTokenNotFound = errors.New("events: token not found")

// RedisEvents satisfies three roles off one shared connection pool:
//   - core/events.Publisher for the worker (publish ready/failed),
//   - http.TokenStore for the SSE authorize endpoint,
//   - http.StatusStreamer for the SSE stream endpoint.
type RedisEvents struct {
	client *redis.Client
}

func NewRedisEvents(client *redis.Client) *RedisEvents {
	return &RedisEvents{client: client}
}

func channelFor(userID string) string {
	return "events:" + userID
}

// --- core/events.Publisher ---

func (r *RedisEvents) Publish(ctx context.Context, ev coreevents.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return r.PublishStatus(ctx, ev.UserID, data)
}

// --- http.StatusStreamer ---

// PublishStatus delivers raw JSON bytes onto userID's status channel. Redis
// PUBLISH only reaches live subscribers; a dropped event is fine because SSE
// consumers refetch on reconnect.
func (r *RedisEvents) PublishStatus(ctx context.Context, userID string, data []byte) error {
	return r.client.Publish(ctx, channelFor(userID), data).Err()
}

// Subscribe invokes onMessage for every status event published for userID
// until the returned stop func is called (or the Redis connection drops).
func (r *RedisEvents) Subscribe(ctx context.Context, userID string, onMessage func([]byte)) (func() error, error) {
	ps := r.client.Subscribe(ctx, channelFor(userID))
	if _, err := ps.Receive(ctx); err != nil {
		ps.Close()
		return nil, err
	}

	ch := ps.Channel()
	var once sync.Once
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for msg := range ch {
			onMessage([]byte(msg.Payload))
		}
	}()

	stop := func() error {
		var err error
		once.Do(func() { err = ps.Close() })
		wg.Wait()
		return err
	}
	return stop, nil
}

// --- http.TokenStore ---

func (r *RedisEvents) Create(ctx context.Context, token, userID string, ttl time.Duration) error {
	return r.client.Set(ctx, "sse:"+token, userID, ttl).Err()
}

func (r *RedisEvents) Resolve(ctx context.Context, token string) (string, error) {
	userID, err := r.client.Get(ctx, "sse:"+token).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}
