package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"universal-media-service/adapters/events"

	"github.com/gin-gonic/gin"
)

// TokenStore mints short-lived bearer-like tokens that a browser's
// EventSource can present: EventSource cannot set an Authorization header,
// so the authorize endpoint trades an authenticated client for an opaque
// query token.
type TokenStore interface {
	Create(ctx context.Context, token, userID string, ttl time.Duration) error
	Resolve(ctx context.Context, token string) (string, error)
}

// StatusStreamer delivers status updates for a user: PublishStatus broadcasts
// on the user's channel and Subscribe runs onMessage for every event until the
// returned stop func is called.
type StatusStreamer interface {
	Subscribe(ctx context.Context, userID string, onMessage func([]byte)) (stop func() error, err error)
	PublishStatus(ctx context.Context, userID string, data []byte) error
}

const (
	// streamTokenTTL bounds how long an authorize token is valid before the
	// client must re-authorize.
	streamTokenTTL = 15 * time.Minute
	// streamKeepAlive keeps idle connections alive through proxies that drop
	// silent streams, and reveals dead clients via write errors.
	streamKeepAlive = 25 * time.Second
	// streamBuffer caps queued events per connection. A slow consumer drops
	// excess events, which the client compensates for with a reconnect
	// refetch rather than blocking the worker's publish path.
	streamBuffer = 16
)

type EventsHandler struct {
	streamer StatusStreamer
	tokens   TokenStore
}

func NewEventsHandler(streamer StatusStreamer, tokens TokenStore) *EventsHandler {
	return &EventsHandler{streamer: streamer, tokens: tokens}
}

// Authorize trades a verified session for a short-lived random stream token
// bound to the authenticated user. Tokens are 128 bits of random secret, so
// forging one requires guessing the value.
func (h *EventsHandler) Authorize(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	token, err := newStreamToken()
	if err != nil {
		log.Printf("Events: failed to generate token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if err := h.tokens.Create(c.Request.Context(), token, userID, streamTokenTTL); err != nil {
		log.Printf("Events: failed to store token for %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "ttlSeconds": int(streamTokenTTL.Seconds())})
}

// Stream opens a Server-Sent Events connection for the user an authorize
// token was minted for. Each status transition is relayed as an "event:
// status" frame; keepalive comments keep idle streams alive through proxies.
func (h *EventsHandler) Stream(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})
		return
	}

	userID, err := h.tokens.Resolve(c.Request.Context(), token)
	if err != nil {
		if errors.Is(err, events.ErrTokenNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		log.Printf("Events: failed to resolve token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	msgCh := make(chan []byte, streamBuffer)
	stop, err := h.streamer.Subscribe(c.Request.Context(), userID, func(data []byte) {
		select {
		case msgCh <- data:
		default: // drop events for a slow consumer
		}
	})
	if err != nil {
		log.Printf("Events: failed to subscribe for %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	defer stop()

	h.writeStream(c, msgCh)
}

func (h *EventsHandler) writeStream(c *gin.Context, msgCh <-chan []byte) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	// Disable buffering by reverse proxies (nginx, Cloudflare, ...) so events
	// reach the browser as they are published.
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	if _, err := fmt.Fprint(c.Writer, ": connected\n\n"); err != nil {
		return
	}
	c.Writer.Flush()

	ticker := time.NewTicker(streamKeepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case msg := <-msgCh:
			if _, err := fmt.Fprintf(c.Writer, "event: status\ndata: %s\n\n", msg); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}

// newStreamToken returns a 128-bit random token encoded as hex.
func newStreamToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
