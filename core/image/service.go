package image

import (
	"context"
	"fmt"
	"mime/multipart"
	"time"
	"universal-media-service/adapters/r2"
)

type Image struct {
	ID          string `json:"id"`
	UserID      string `json:"userID"`
	OriginalURL string `json:"url"`
	Format      string `json:"format"`
	SizeBytes   int64  `json:"size"`
	CreatedAt   int64  `json:"createdAt"`
}

type Repository interface {
	SaveImage(img *Image) error
}

type ImageService struct {
	Storage *r2.Client
	Repo    Repository
}

func NewImageService(repo Repository, storage *r2.Client) *ImageService {
	return &ImageService{
		Repo:    repo,
		Storage: storage,
	}
}

// Upload saves file to R2 and stores metadata
func (s *ImageService) Upload(userID string, file multipart.File, filename, contentType string, size int64) (*Image, error) {
	key := fmt.Sprintf("raw/%s/%s", userID, filename)

	url, err := s.Storage.Upload(context.TODO(), key, file, contentType)
	if err != nil {
		return nil, err
	}

	img := &Image{
		ID:          fmt.Sprintf("%d", time.Now().UnixNano()), // simple unique ID
		UserID:      userID,
		OriginalURL: url,
		Format:      contentType,
		SizeBytes:   size,
		CreatedAt:   time.Now().Unix(),
	}

	if s.Repo != nil {
		if err := s.Repo.SaveImage(img); err != nil {
			return nil, err
		}
	}

	return img, nil
}
