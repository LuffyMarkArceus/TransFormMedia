package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// The old counter-based limiter refreshed its window anchor on every request,
// so any client polling more often than the window accumulated count forever
// and was permanently 429-locked after `limit` total requests. A token bucket
// refills continuously, so staying under the sustained rate always works.
func TestRateLimiter_SustainedPollingNeverLocksOut(t *testing.T) {
	rl := NewRateLimiter(4, 400*time.Millisecond) // sustained 10 req/s
	defer rl.Close()

	// 10 requests at 150ms spacing ≈ 6.7 req/s — under the sustained rate,
	// so none may be rejected even though there are far more than `limit`
	// requests in total and consecutive requests are closer than the window.
	for i := 0; i < 10; i++ {
		if !rl.allow("user-a") {
			t.Fatalf("request %d rejected although client is under the sustained rate", i+1)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func TestRateLimiter_BurstIsLimitedAndRecovers(t *testing.T) {
	rl := NewRateLimiter(3, 500*time.Millisecond)
	defer rl.Close()

	for i := 0; i < 3; i++ {
		if !rl.allow("user-b") {
			t.Fatalf("request %d should pass within burst", i+1)
		}
	}
	if rl.allow("user-b") {
		t.Fatal("request beyond burst should be rate limited")
	}

	// After one full window the bucket is full again — no permanent lockout.
	time.Sleep(600 * time.Millisecond)
	if !rl.allow("user-b") {
		t.Fatal("request after a full window must be allowed again")
	}
}

func TestRateLimiter_KeysAreIsolated(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Close()

	if !rl.allow("user-1") {
		t.Fatal("first request for user-1 should pass")
	}
	if rl.allow("user-1") {
		t.Fatal("second request for user-1 should be limited")
	}
	if !rl.allow("user-2") {
		t.Fatal("user-2 must have an independent bucket")
	}
}

func newTestContext(userID string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/media", nil)
	c.Request.RemoteAddr = "203.0.113.10:4321"
	if userID != "" {
		c.Set("userID", userID)
	}
	return c, w
}

func TestUserMiddleware_KeyedByIdentityNotIP(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Close()
	mw := rl.UserMiddleware()

	// Same IP, same user: second request limited.
	c1, _ := newTestContext("user_x")
	mw(c1)
	if c1.IsAborted() {
		t.Fatal("first request should pass")
	}
	c2, w2 := newTestContext("user_x")
	mw(c2)
	if !c2.IsAborted() || w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request should be 429, got aborted=%v code=%d", c2.IsAborted(), w2.Code)
	}

	// Same IP, different user: independent bucket.
	c3, _ := newTestContext("user_y")
	mw(c3)
	if c3.IsAborted() {
		t.Fatal("different user behind the same IP must not share the strict bucket")
	}
}

func TestIPMiddleware_LimitsByIP(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Close()
	mw := rl.IPMiddleware()

	c1, _ := newTestContext("")
	mw(c1)
	if c1.IsAborted() {
		t.Fatal("first request should pass")
	}
	c2, w2 := newTestContext("")
	mw(c2)
	if !c2.IsAborted() || w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request from same IP should be 429, got aborted=%v code=%d", c2.IsAborted(), w2.Code)
	}
}
