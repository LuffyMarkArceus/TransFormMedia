package media

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, m *Media) error
	ListByUser(ctx context.Context, userID string) ([]Media, error)
	ListByUserAndType(ctx context.Context, userID string, mediaType string) ([]Media, error)

	GetByID(ctx context.Context, id string) (*Media, error)
	// GetByIDForUser returns media only when owned by userID; otherwise ErrNotFound.
	GetByIDForUser(ctx context.Context, id, userID string) (*Media, error)
	DeleteByID(ctx context.Context, id, userID string) error

	UpdateName(ctx context.Context, id, userID, name string) error
}
