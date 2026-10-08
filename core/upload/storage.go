package upload

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrObjectNotFound is returned by Head/GetRange/DownloadTo when the key does
// not exist. Implementations must return it unwrapped-friendly (errors.Is).
var ErrObjectNotFound = errors.New("object not found")

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Size int64
}

// Storage abstracts object storage for uploads and deletes (enables unit tests).
type Storage interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) (string, error)
	Delete(ctx context.Context, key string) error
	Get(ctx context.Context, key string) ([]byte, error)

	// DownloadTo streams the object into dst without buffering it in memory.
	// Returns ErrObjectNotFound when the key does not exist.
	DownloadTo(ctx context.Context, key string, dst io.Writer) (int64, error)
	// Head returns the object's size. Returns ErrObjectNotFound when missing.
	Head(ctx context.Context, key string) (ObjectInfo, error)
	// GetRange reads at most length bytes starting at offset.
	// Returns ErrObjectNotFound when the key does not exist.
	GetRange(ctx context.Context, key string, offset, length int64) ([]byte, error)
	// PresignPut returns a time-limited URL that lets anyone possessing it PUT
	// this key with exactly contentType. ttl bounds the URL's validity.
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)

	PublicBaseURL() string
}
