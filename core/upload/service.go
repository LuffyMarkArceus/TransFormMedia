package upload

import (
	"bytes"
	"context"
	"fmt"

	"log"
	"mime/multipart"
	"net/url"
	"strings"
	"time"

	"universal-media-service/adapters/r2"
	"universal-media-service/core/image"
	"universal-media-service/core/media"

	"github.com/google/uuid"
)

type Service struct {
	Storage *r2.Client
	repo    media.Repository
}

func NewService(repo media.Repository, Storage *r2.Client) *Service {
	return &Service{repo: repo, Storage: Storage}
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

	// ---------- Read file into memory ----------
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("Failed to read file %w", err)
	}
	originalBytes := buf.Bytes()

	// ---------- Process Image ----------
	result, err := image.Process(
		originalBytes,
		image.DefaultOptions(),
		image.DefaultThumbnailOptions(),
	)
	if err != nil {
		return nil, fmt.Errorf("Invalid Image, Failed to process image %w", err)
	}

	imageID := uuid.NewString()
	log.Printf("Processed Image & thumbnail created for %s", imageID)

	// ---------- Keys ----------
	rawKey := fmt.Sprintf("raw/%s/%s", userID, imageID)
	processedKey := fmt.Sprintf("processed/%s/%s", userID, imageID)
	thumbnailKey := fmt.Sprintf("thumbnail/%s/%s", userID, imageID)

	// ---------- Upload original ----------
	_, err = s.Storage.Upload(ctx, rawKey, bytes.NewReader(originalBytes), contentType)
	if err != nil {
		return nil, err
	}

	// ---------- Upload Processed ----------
	if _, err := s.Storage.Upload(
		ctx,
		processedKey,
		bytes.NewReader(result.ProcessedBytes),
		result.ProcessedContentType,
	); err != nil {
		return nil, err
	}

	// ---------- Thumbnail (320 x 320 center crop) ----------

	if _, err := s.Storage.Upload(
		ctx,
		thumbnailKey,
		bytes.NewReader(result.ThumbnailBytes),
		result.ThumbnailContentType,
	); err != nil {
		return nil, err
	}

	// ---------- Construct public URL for frontend ----------
	originalURL := fmt.Sprintf("%s/%s", s.Storage.PublicBase, rawKey)
	processedURL := fmt.Sprintf("%s/%s", s.Storage.PublicBase, processedKey)
	thumbnailURL := fmt.Sprintf("%s/%s", s.Storage.PublicBase, thumbnailKey)

	m := &media.Media{
		ID:           uuid.NewString(),
		UserID:       userID,
		Name:         filename,
		Type:         "image",
		OriginalURL:  originalURL,
		ProcessedURL: &processedURL,
		ThumbnailURL: &thumbnailURL,
		Format:       contentType,
		SizeBytes:    size,
		Width:        result.Width,
		Height:       result.Height,
		Status:       "uploaded",
		CreatedAt:    time.Now(),
	}

	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}

	log.Printf("Uploaded images for %s (%dx%d)", imageID, result.Width, result.Height)

	return m, nil
}

func extractKey(publicURL string) string {
	u, _ := url.Parse(publicURL)
	return strings.TrimPrefix(u.Path, "/")
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
	// prefix := s.storage.PublicBase + "/"
	// key := strings.TrimPrefix(img.OriginalURL, prefix)

	// 3. Delete from R2
	if img.OriginalURL != "" {
		_ = s.Storage.Delete(ctx, extractKey(img.OriginalURL))
		log.Printf("Deleted original image from R2 %s", img.OriginalURL)
	}
	if img.ProcessedURL != nil {
		_ = s.Storage.Delete(ctx, extractKey(*img.ProcessedURL))
		log.Printf("Deleted processed image from R2 %s", *img.ProcessedURL)
	}
	if img.ThumbnailURL != nil {
		_ = s.Storage.Delete(ctx, extractKey(*img.ThumbnailURL))
		log.Printf("Deleted thumbnail image from R2 %s", *img.ThumbnailURL)
	}

	// 4. Delete DB row
	return s.repo.DeleteByID(ctx, imageID, userID)
}
