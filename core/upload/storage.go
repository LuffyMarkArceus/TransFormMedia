package upload

import (
	"context"
	"io"
)

// Storage abstracts object storage for uploads and deletes (enables unit tests).
type Storage interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) (string, error)
	Delete(ctx context.Context, key string) error
	Get(ctx context.Context, key string) ([]byte, error)
	PublicBaseURL() string
}
