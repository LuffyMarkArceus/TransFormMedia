package auth

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimiter throttles requests per key using a token bucket
// (golang.org/x/time/rate).
//
// The bucket refills continuously at limit/window and holds a burst of
// `limit` tokens. A client that stays at or under the sustained rate is
// therefore never locked out, no matter how continuously it polls — unlike a
// naive counter that only resets after a full idle period, which permanently
// 429s any client whose requests are spaced closer than the window.
type RateLimiter struct {
	mu        sync.Mutex
	visitors  map[string]*visitor
	rate      rate.Limit
	burst     int
	window    time.Duration
	stop      chan struct{}
	closeOnce sync.Once
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter allows up to `limit` requests per `window` per key, with a
// burst capacity of `limit`.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate.Limit(float64(limit) / window.Seconds()),
		burst:    limit,
		window:   window,
		stop:     make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// UserMiddleware throttles by authenticated identity (Clerk `sub`), falling
// back to the client IP only when no user is attached. Keying on the verified
// JWT subject makes the strict limit unspoofable and keeps users behind a
// shared NAT in independent buckets.
func (rl *RateLimiter) UserMiddleware() gin.HandlerFunc {
	return rl.middleware(func(c *gin.Context) string {
		if uid := c.GetString("userID"); uid != "" {
			return "u:" + uid
		}
		return "ip:" + c.ClientIP()
	})
}

// IPMiddleware throttles by client IP. It is the coarse pre-auth flood guard;
// the authoritative limit is UserMiddleware. Note: behind a reverse proxy the
// IP comes from X-Forwarded-For and is only as trustworthy as the proxy is —
// which is why the strict limit must be identity-keyed, not IP-keyed.
func (rl *RateLimiter) IPMiddleware() gin.HandlerFunc {
	return rl.middleware(func(c *gin.Context) string {
		return "ip:" + c.ClientIP()
	})
}

func (rl *RateLimiter) middleware(keyFn func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !rl.allow(keyFn(c)) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

func (rl *RateLimiter) allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.rate, rl.burst)}
		rl.visitors[key] = v
	}
	v.lastSeen = now
	return v.limiter.Allow()
}

// cleanup evicts idle keys so memory stays bounded by active clients.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for k, v := range rl.visitors {
				if now.Sub(v.lastSeen) > rl.window*2 {
					delete(rl.visitors, k)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Close stops the background cleanup goroutine. Safe to call more than once.
func (rl *RateLimiter) Close() {
	rl.closeOnce.Do(func() { close(rl.stop) })
}
