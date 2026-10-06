package media

import "time"

type Media struct {
	ID     string `json:"id"`
	UserID string `json:"userID"`
	Type   string `json:"type"`

	Name string `json:"name,omitempty"`

	OriginalURL  string  `json:"originalURL"`
	ProcessedURL *string `json:"processedURL,omitempty"`
	ThumbnailURL *string `json:"thumbnailURL,omitempty"`

	Format    string `json:"format"`
	SizeBytes int64  `json:"sizeBytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Duration  int    `json:"duration"`

	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the last write to the row. Replace bumps it via SQL
	// (NOW()), so it doubles as a content version for cache keys: a replace
	// can never be served previously cached transformed bytes.
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}
