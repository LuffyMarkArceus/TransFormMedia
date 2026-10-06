// Package events defines the small contract the worker uses to announce
// media status transitions for push consumption (SSE in the API layer).
package events

import "context"

// Kind identifies the type of an event.
type Kind string

const (
	// KindStatus is published when a media record changes status
	// (currently "ready" or the permanent "failed" after retries run out).
	KindStatus Kind = "status"
)

// Event is a status transition. UserID is used for per-user routing and is
// never serialized out to clients.
type Event struct {
	Type    Kind   `json:"type"`
	MediaID string `json:"mediaID"`
	UserID  string `json:"-"`
	Status  string `json:"status"`
}

// Publisher delivers events. Implementations are optional in the runtime:
// the worker tolerates a nil publisher, and subscribers (SSE) tolerate an
// absent stream by degrading to polling.
type Publisher interface {
	Publish(ctx context.Context, ev Event) error
}
