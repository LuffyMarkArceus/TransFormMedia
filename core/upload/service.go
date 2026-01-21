package upload

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"strings"
	"time"

	"universal-media-service/adapters/r2"
	"universal-media-service/core/media"

	"github.com/disintegration/imaging"
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

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("Failed to read file %w", err)
	}
	data := buf.Bytes()

	// Decode image to get dimensions
	img, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("Invalid Image, Failed to decode image %w", err)
	}
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	key := fmt.Sprintf("raw/%s/%s", userID, filename)

	// Upload to R2 key for the object
	_, err = s.storage.Upload(ctx, key, bytes.NewReader(data), contentType)
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
		Width:       width,
		Height:      height,
		Status:      "uploaded",
		CreatedAt:   time.Now(),
	}

	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}

	return m, nil
}

// DeleteImage removes image metadata and file from R2
func (s *Service) DeleteImage(
	ctx context.Context,
	imageID string,
	userID string,
) error {

	// 1. Fetch image
	img, err := s.repo.GetByID(ctx, imageID, userID)
	if err != nil {
		return err
	}

	// 2. Extract R2 key from URL
	// publicURL = https://...r2.dev/raw/userID/filename
	prefix := s.storage.PublicBase + "/"
	key := strings.TrimPrefix(img.OriginalURL, prefix)

	// 3. Delete from R2
	if err := s.storage.Delete(ctx, key); err != nil {
		return err
	}

	// 4. Delete DB row
	return s.repo.DeleteByID(ctx, imageID, userID)
}
