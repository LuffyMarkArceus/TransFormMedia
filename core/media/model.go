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

	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}
