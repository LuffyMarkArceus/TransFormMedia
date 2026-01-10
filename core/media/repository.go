package media

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, m *Media) error
	ListByUser(ctx context.Context, userID string) ([]Media, error)
}
