package upload

import (
	"bytes"
	"context"
	"fmt"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"mime/multipart"
	"net/url"
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

	// ---------- Read file into memory ----------
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("Failed to read file %w", err)
	}
	originalBytes := buf.Bytes()

	// ---------- Decode image to get dimensions ----------
	img, err := imaging.Decode(bytes.NewReader(originalBytes))
	if err != nil {
		return nil, fmt.Errorf("Invalid Image, Failed to decode image %w", err)
	}
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	// ---------- Keys ----------
	rawKey := fmt.Sprintf("raw/%s/%s", userID, filename)
	processedKey := fmt.Sprintf("processed/%s/%s", userID, filename)
	thumbnailKey := fmt.Sprintf("thumbnail/%s/%s", userID, filename)

	// ---------- Upload original ----------
	_, err = s.storage.Upload(ctx, rawKey, bytes.NewReader(originalBytes), contentType)
	if err != nil {
		return nil, err
	}
	log.Printf("Uploaded original image to R2 %s", rawKey)

	// ---------- Processed image (max 1920 x 1080) ----------
	processedImg := imaging.Fit(img, 1920, 1080, imaging.Lanczos)

	var processedBuf bytes.Buffer
	if err := imaging.Encode(&processedBuf, processedImg, imaging.JPEG); err != nil {
		return nil, err
	}

	if _, err := s.storage.Upload(
		ctx,
		processedKey,
		bytes.NewReader(processedBuf.Bytes()),
		"image/jpeg",
	); err != nil {
		return nil, err
	}
	// log.Println("Uploaded processed image to R2")

	// ---------- Thumbnail (320 x 320 center crop) ----------
	thumbImg := imaging.Fit(img, 320, 180, imaging.Lanczos)

	var thumbBuf bytes.Buffer
	if err := imaging.Encode(&thumbBuf, thumbImg, imaging.JPEG); err != nil {
		return nil, err
	}

	if _, err := s.storage.Upload(
		ctx,
		thumbnailKey,
		bytes.NewReader(thumbBuf.Bytes()),
		"image/jpeg",
	); err != nil {
		return nil, err
	}
	// log.Println("Uploaded thumbnail image to R2")

	// ---------- Construct public URL for frontend ----------
	originalURL := fmt.Sprintf("%s/%s", s.storage.PublicBase, rawKey)
	processedURL := fmt.Sprintf("%s/%s", s.storage.PublicBase, processedKey)
	thumbnailURL := fmt.Sprintf("%s/%s", s.storage.PublicBase, thumbnailKey)

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
		Width:        width,
		Height:       height,
		Status:       "uploaded",
		CreatedAt:    time.Now(),
	}

	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}

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
		_ = s.storage.Delete(ctx, extractKey(img.OriginalURL))
		log.Printf("Deleted original image from R2 %s", img.OriginalURL)
	}
	if img.ProcessedURL != nil {
		_ = s.storage.Delete(ctx, extractKey(*img.ProcessedURL))
		log.Printf("Deleted processed image from R2 %s", *img.ProcessedURL)
	}
	if img.ThumbnailURL != nil {
		_ = s.storage.Delete(ctx, extractKey(*img.ThumbnailURL))
		log.Printf("Deleted thumbnail image from R2 %s", *img.ThumbnailURL)
	}

	// 4. Delete DB row
	return s.repo.DeleteByID(ctx, imageID, userID)
}
