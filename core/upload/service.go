package upload

import (
	"context"
	"fmt"
	"mime/multipart"
	"time"

	"universal-media-service/adapters/r2"
	"universal-media-service/core/media"

	"github.com/google/uuid"
)

type Service struct {
	storage *r2.Client
	repo    media.Repository
}

func NewService(repo media.Repository, storage *r2.Client) *Service {
	return &Service{repo: repo, storage: storage}
}

// Upload Image saves file to R2 and stores metadata
func (s *Service) UploadImage(
	ctx context.Context,
	userID string,
	file multipart.File,
	filename string,
	contentType string,
	size int64,
) (*media.Media, error) {

	key := fmt.Sprintf("raw/%s/%s", userID, filename)

	// Upload to R2 key for the object
	_, err := s.storage.Upload(ctx, key, file, contentType)
	if err != nil {
		return nil, err
	}

	// Construct public URL for frontend
	publicURL := fmt.Sprintf("%s/%s/%s", s.storage.PublicBase, "raw/"+userID, filename)

	m := &media.Media{
		ID:          uuid.NewString(),
		UserID:      userID,
		Name:        filename,
		Type:        "image",
		OriginalURL: publicURL,
		Format:      contentType,
		SizeBytes:   size,
		Status:      "uploaded",
		CreatedAt:   time.Now(),
	}

	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}

	return m, nil
}
