package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"universal-media-service/adapters/events"

	"github.com/gin-gonic/gin"
)

type fakeTokenStore struct {
	mu     sync.Mutex
	tokens map[string]string
}

func (f *fakeTokenStore) Create(_ context.Context, token, userID string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[token] = userID
	return nil
}

func (f *fakeTokenStore) Resolve(_ context.Context, token string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	userID, ok := f.tokens[token]
	if !ok {
		return "", events.ErrTokenNotFound
	}
	return userID, nil
}

type fakeStreamer struct {
	mu           sync.Mutex
	subscribed   chan struct{}
	stopped      int
	onMessage    func([]byte)
	subscribeErr error
}

func (f *fakeStreamer) Subscribe(_ context.Context, _ string, onMessage func([]byte)) (func() error, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subscribeErr != nil {
		return nil, f.subscribeErr
	}
	f.onMessage = onMessage
	close(f.subscribed)
	once := sync.Once{}
	return func() error {
		once.Do(func() { f.stopped++ })
		return nil
	}, nil
}

func (f *fakeStreamer) PublishStatus(context.Context, string, []byte) error { return nil }

func (f *fakeStreamer) emit(data []byte) {
	f.mu.Lock()
	cb := f.onMessage
	f.mu.Unlock()
	if cb != nil {
		cb(data)
	}
}

func TestEventsAuthorize_MintsToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := &fakeTokenStore{tokens: map[string]string{}}
	h := NewEventsHandler(&fakeStreamer{subscribed: make(chan struct{})}, tokens)

	engine := gin.New()
	engine.POST("/authorize", func(c *gin.Context) {
		c.Set("userID", "user-1")
		h.Authorize(c)
	})

	req := httptest.NewRequest("POST", "/authorize", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	tokens.mu.Lock()
	defer tokens.mu.Unlock()
	if len(tokens.tokens) != 1 {
		t.Fatalf("expected exactly one token stored, got %d", len(tokens.tokens))
	}
	for token, userID := range tokens.tokens {
		if userID != "user-1" {
			t.Fatalf("token must bind to user-1, got %q", userID)
		}
		if len(token) != 32 {
			t.Fatalf("expected a 128-bit hex token, got %q (%d chars)", token, len(token))
		}
	}
	if !strings.Contains(rec.Body.String(), `"token"`) || !strings.Contains(rec.Body.String(), `"ttlSeconds"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestEventsAuthorize_RequiresUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewEventsHandler(&fakeStreamer{subscribed: make(chan struct{})}, &fakeTokenStore{tokens: map[string]string{}})

	engine := gin.New()
	engine.POST("/authorize", h.Authorize)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("POST", "/authorize", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a user, got %d", rec.Code)
	}
}

func TestEventsStream_MissingTokenRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewEventsHandler(&fakeStreamer{subscribed: make(chan struct{})}, &fakeTokenStore{tokens: map[string]string{}})

	engine := gin.New()
	engine.GET("/stream", h.Stream)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("GET", "/stream", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without a token, got %d", rec.Code)
	}
}

func TestEventsStream_UnknownTokenRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewEventsHandler(&fakeStreamer{subscribed: make(chan struct{})}, &fakeTokenStore{tokens: map[string]string{}})

	engine := gin.New()
	engine.GET("/stream", h.Stream)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("GET", "/stream?token=nope", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unknown token, got %d", rec.Code)
	}
}

func TestEventsStream_RelaysStatusEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	streamer := &fakeStreamer{subscribed: make(chan struct{})}
	tokens := &fakeTokenStore{tokens: map[string]string{"abc": "user-1"}}
	h := NewEventsHandler(streamer, tokens)

	engine := gin.New()
	engine.GET("/stream", h.Stream)

	// The request context itself bounds the stream's lifetime, so ServeHTTP
	// runs synchronously in this goroutine and the only other goroutine is
	// the emitter — the recorder is never touched concurrently.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("GET", "/stream?token=abc", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	go func() {
		<-streamer.subscribed
		time.Sleep(50 * time.Millisecond)
		streamer.emit([]byte(`{"type":"status","mediaID":"media-1","status":"ready"}`))
	}()

	engine.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: status") || !strings.Contains(body, `"mediaID":"media-1"`) {
		t.Fatalf("expected the status event to be relayed, got body: %q", body)
	}
	// The 500ms task must not cut the stream short before an event arrives.
	if !strings.HasPrefix(body, ": connected") {
		t.Fatalf("expected the connected comment first, got: %q", body)
	}
}

func TestEventsStream_StopsWhenNoMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	streamer := &fakeStreamer{subscribed: make(chan struct{})}
	tokens := &fakeTokenStore{tokens: map[string]string{"abc": "user-1"}}
	h := NewEventsHandler(streamer, tokens)

	engine := gin.New()
	engine.GET("/stream", h.Stream)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("GET", "/stream?token=abc", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Fatalf("expected the connected comment even with no events, got: %q", body)
	}
	if streamer.stopped != 1 {
		t.Fatalf("expected the subscription to be stopped on disconnect, got %d stops", streamer.stopped)
	}
}
