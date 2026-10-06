package media

import (
	"context"
)

const (
	SortByCreatedAt = "created_at"
	SortByName      = "name"
	SortBySize      = "size_bytes"

	SortDirDesc = "desc"
	SortDirAsc  = "asc"

	DefaultLimit  = 20
	MaxLimit      = 100
	DefaultOffset = 0
)

type ListParams struct {
	UserID  string
	Type    string // optional type filter
	Search  string // optional name search (ILIKE)
	Status  string // optional status filter (default excludes "trashed")
	SortBy  string // SortByCreatedAt, SortByName, SortBySize
	SortDir string // SortDirDesc or SortDirAsc
	Limit   int
	Offset  int
}

type PaginatedResult struct {
	Data  []Media `json:"data"`
	Total int     `json:"total"`
}

type Repository interface {
	Create(ctx context.Context, m *Media) error
	ListByUser(ctx context.Context, userID string) ([]Media, error)
	ListByUserAndType(ctx context.Context, userID string, mediaType string) ([]Media, error)
	ListPaginated(ctx context.Context, params ListParams) (*PaginatedResult, error)
	ListByStatus(ctx context.Context, status string, limit int) ([]Media, error)

	GetByID(ctx context.Context, id string) (*Media, error)
	GetByIDForUser(ctx context.Context, id, userID string) (*Media, error)
	DeleteByID(ctx context.Context, id, userID string) error

	UpdateName(ctx context.Context, id, userID, name string) error
	UpdateStatus(ctx context.Context, id, userID, status string) error
	UpdateProcessedResult(ctx context.Context, id, userID, processedURL, thumbnailURL string, width, height, duration int) error

	// UpdateContent replaces every content-derived field of an existing row
	// in place. It never changes id, user_id or created_at, and returns
	// ErrNotFound when the row does not exist or belongs to another user.
	UpdateContent(ctx context.Context, m *Media) error
}
