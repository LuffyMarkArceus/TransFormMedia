package image

import "time"

type Image struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	OriginalURL string    `json:"original_url"`
	Format      string    `json:"format"`
	SizeBytes   int64     `json:"size_bytes"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Metadata    string    `json:"metadata"`
	CreatedAt   time.Time `json:"created_at"`
}
