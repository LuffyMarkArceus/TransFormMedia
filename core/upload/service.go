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
	"universal-media-service/core/audio"
	"universal-media-service/core/image"
	"universal-media-service/core/media"
	"universal-media-service/core/video"

	"github.com/google/uuid"
)

type Service struct {
	Storage        *r2.Client
	repo           media.Repository
	imageProcessor *image.Processor
	videoProcessor *video.Processor
	audioProcessor *audio.Processor
}

func NewService(repo media.Repository, storage *r2.Client) *Service {
	return &Service{
		Storage:        storage,
		repo:           repo,
		imageProcessor: image.NewProcessor(),
		videoProcessor: video.NewProcessor(),
		audioProcessor: audio.NewProcessor(),
	}
}

var supportedImageTypes = []string{"image/jpeg", "image/jpg", "image/png"}
var supportedVideoTypes = []string{"video/mp4", "video/quicktime", "video/x-msvideo", "video/webm", "video/x-matroska"}
var supportedAudioTypes = []string{"audio/mpeg", "audio/mp3", "audio/wav", "audio/x-wav", "audio/ogg", "audio/flac", "audio/aac", "audio/mp4"}

func isImageType(ct string) bool {
	for _, t := range supportedImageTypes {
		if ct == t || (ct == "image/jpg" && t == "image/jpeg") {
			return true
		}
	}
	return false
}

func isVideoType(ct string) bool {
	for _, t := range supportedVideoTypes {
		if ct == t {
			return true
		}
	}
	return false
}

func isAudioType(ct string) bool {
	for _, t := range supportedAudioTypes {
		if ct == t {
			return true
		}
	}
	return false
}

func (s *Service) getProcessor(contentType string) media.MediaProcessor {
	switch {
	case isImageType(contentType):
		return s.imageProcessor
	case isVideoType(contentType):
		return s.videoProcessor
	case isAudioType(contentType):
		return s.audioProcessor
	default:
		return nil
	}
}

func getMediaType(contentType string) string {
	switch {
	case isImageType(contentType):
		return "image"
	case isVideoType(contentType):
		return "video"
	case isAudioType(contentType):
		return "audio"
	default:
		return "unknown"
	}
}

func (s *Service) UploadMedia(
	ctx context.Context,
	userID string,
	file multipart.File,
	filename string,
	contentType string,
	size int64,
) (*media.Media, error) {
	processor := s.getProcessor(contentType)
	if processor == nil {
		return nil, fmt.Errorf("unsupported media type: %s", contentType)
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	originalBytes := buf.Bytes()

	mediaType := getMediaType(contentType)
	mediaID := uuid.NewString()

	result, err := processor.Process(ctx, originalBytes, contentType)
	if err != nil {
		return nil, fmt.Errorf("failed to process %s: %w", mediaType, err)
	}

	rawKey := fmt.Sprintf("raw/%s/%s/%s", mediaType, userID, mediaID)
	processedKey := fmt.Sprintf("processed/%s/%s/%s", mediaType, userID, mediaID)
	_, err = s.Storage.Upload(ctx, rawKey, bytes.NewReader(originalBytes), contentType)
	if err != nil {
		return nil, fmt.Errorf("failed to upload original: %w", err)
	}

	_, err = s.Storage.Upload(
		ctx,
		processedKey,
		bytes.NewReader(result.ProcessedBytes),
		result.ProcessedContentType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upload processed: %w", err)
	}

	var thumbnailURL *string
	if len(result.ThumbnailBytes) > 0 {
		thumbnailKey := fmt.Sprintf("thumbnail/%s/%s/%s", mediaType, userID, mediaID)
		thURL, err := s.Storage.Upload(
			ctx,
			thumbnailKey,
			bytes.NewReader(result.ThumbnailBytes),
			result.ThumbnailContentType,
		)
		if err != nil {
			log.Printf("Warning: failed to upload thumbnail: %v", err)
		} else {
			thumbnailURL = &thURL
		}
	}

	originalURL := fmt.Sprintf("%s/%s", s.Storage.PublicBase, rawKey)
	processedURL := fmt.Sprintf("%s/%s", s.Storage.PublicBase, processedKey)

	m := &media.Media{
		ID:           mediaID,
		UserID:       userID,
		Name:         filename,
		Type:         mediaType,
		OriginalURL:  originalURL,
		ProcessedURL: &processedURL,
		ThumbnailURL: thumbnailURL,
		Format:       contentType,
		SizeBytes:    size,
		Width:        result.Width,
		Height:       result.Height,
		Duration:     result.Duration,
		Status:       "uploaded",
		CreatedAt:    time.Now(),
	}

	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}

	log.Printf("Uploaded %s for %s (%dx%d, %ds)", mediaType, mediaID, result.Width, result.Height, result.Duration)

	return m, nil
}

func (s *Service) UploadImage(
	ctx context.Context,
	userID string,
	file multipart.File,
	filename string,
	contentType string,
	size int64,
) (*media.Media, error) {
	return s.UploadMedia(ctx, userID, file, filename, contentType, size)
}

func extractKey(publicURL string) string {
	u, _ := url.Parse(publicURL)
	return strings.TrimPrefix(u.Path, "/")
}

func (s *Service) DeleteMedia(
	ctx context.Context,
	mediaID string,
	userID string,
) error {
	m, err := s.repo.GetByID(ctx, mediaID)
	if err != nil {
		return err
	}

	if m.OriginalURL != "" {
		_ = s.Storage.Delete(ctx, extractKey(m.OriginalURL))
		log.Printf("Deleted original from R2: %s", m.OriginalURL)
	}
	if m.ProcessedURL != nil {
		_ = s.Storage.Delete(ctx, extractKey(*m.ProcessedURL))
		log.Printf("Deleted processed from R2: %s", *m.ProcessedURL)
	}
	if m.ThumbnailURL != nil {
		_ = s.Storage.Delete(ctx, extractKey(*m.ThumbnailURL))
		log.Printf("Deleted thumbnail from R2: %s", *m.ThumbnailURL)
	}

	return s.repo.DeleteByID(ctx, mediaID, userID)
}

func (s *Service) DeleteImage(
	ctx context.Context,
	imageID string,
	userID string,
) error {
	return s.DeleteMedia(ctx, imageID, userID)
}
