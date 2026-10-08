package upload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"universal-media-service/core/audio"
	"universal-media-service/core/image"
	"universal-media-service/core/media"
	"universal-media-service/core/video"

	"github.com/google/uuid"
)

type Service struct {
	Storage        Storage
	repo           media.Repository
	imageProcessor *image.Processor
	videoProcessor *video.Processor
	audioProcessor *audio.Processor
	Limits         Limits
}

// Limits are the per-request and per-user size boundaries of the service.
type Limits struct {
	// MaxUploadBytes caps a single file of any non-image type.
	MaxUploadBytes int64
	// MaxImageBytes caps a single image (images are dimension-limited anyway;
	// this keeps a huge "image" from ever being buffered for processing).
	MaxImageBytes int64
	// StorageQuotaBytes caps the sum of a user's stored media (including
	// trashed and pending rows, which still occupy storage or reserve quota).
	StorageQuotaBytes int64
}

// DefaultLimits matches the historical app-level constants.
func DefaultLimits() Limits {
	return Limits{
		MaxUploadBytes:    500 * 1024 * 1024,
		MaxImageBytes:     32 * 1024 * 1024,
		StorageQuotaBytes: 10 * 1024 * 1024 * 1024,
	}
}

type Option func(*Service)

// WithLimits overrides the default size limits.
func WithLimits(l Limits) Option {
	return func(s *Service) { s.Limits = l }
}

func NewService(repo media.Repository, storage Storage, opts ...Option) *Service {
	s := &Service{
		Storage:        storage,
		repo:           repo,
		imageProcessor: image.NewProcessor(),
		videoProcessor: video.NewProcessor(),
		audioProcessor: audio.NewProcessor(),
		Limits:         DefaultLimits(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

const (
	// Max size we allow for synchronous in-memory processing. Anything larger
	// is stored raw and processed by the worker, which streams through temp
	// files — 512 Mi Cloud Run instances must never hold whole jobs in RAM.
	MaxSyncProcessingSize = 16 * 1024 * 1024

	// presignTTL bounds how long a direct-upload URL stays valid. It covers
	// slow clients comfortably; abandoned uploads are swept hours later.
	presignTTL = 30 * time.Minute

	// maxNameRunes keeps stored filenames sane without splitting UTF-8.
	maxNameRunes = 200
)

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

func (s *Service) GetImageProcessor() *image.Processor {
	return s.imageProcessor
}

func (s *Service) GetVideoProcessor() *video.Processor {
	return s.videoProcessor
}

func (s *Service) GetAudioProcessor() *audio.Processor {
	return s.audioProcessor
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

// MediaTypeOf maps a content type to its family: image | video | audio |
// unknown. Both the multipart and direct-upload flows rely on it.
func MediaTypeOf(contentType string) string {
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

// MaxBytesFor returns the per-file byte cap for the given content type.
func (s *Service) MaxBytesFor(contentType string) int64 {
	if MediaTypeOf(contentType) == "image" {
		return s.Limits.MaxImageBytes
	}
	return s.Limits.MaxUploadBytes
}

func (s *Service) ReplaceMedia(
	ctx context.Context,
	mediaID string,
	userID string,
	file multipart.File,
	filename string,
	contentType string,
	size int64,
) (*media.Media, error) {
	existing, err := s.repo.GetByIDForUser(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	if existing.Status == "trashed" {
		return nil, media.ErrTrashed
	}

	// Ingest the new content under the SAME media ID so storage keys are
	// reused. Ingestion performs no DB write: if it fails, the existing
	// record still points at intact objects.
	m, err := s.ingestMedia(ctx, userID, mediaID, file, filename, contentType, size)
	if err != nil {
		return nil, err
	}

	// Update the existing row in place — no delete + re-create, so no
	// duplicate rows and no window where the record is missing.
	if err := s.repo.UpdateContent(ctx, m); err != nil {
		return nil, err
	}

	// Only after the DB points at the new content, drop old objects the new
	// version no longer references (type changed, or an async replace that
	// has not produced processed/thumbnail variants yet).
	s.deleteStaleAssets(ctx, existing, m)

	return m, nil
}

// deleteStaleAssets removes storage objects from the previous version of a
// media record that the new version no longer references. Failures are logged
// only: an orphaned object costs storage, while a wrongly deleted one is data
// loss.
func (s *Service) deleteStaleAssets(ctx context.Context, old, updated *media.Media) {
	stale := func(oldURL, newURL *string) {
		if oldURL == nil || *oldURL == "" {
			return
		}
		if newURL != nil && *newURL == *oldURL {
			return
		}
		if err := s.Storage.Delete(ctx, extractKey(*oldURL)); err != nil {
			log.Printf("Warning: failed to delete stale asset %s: %v", *oldURL, err)
		}
	}
	stale(&old.OriginalURL, &updated.OriginalURL)
	stale(old.ProcessedURL, updated.ProcessedURL)
	stale(old.ThumbnailURL, updated.ThumbnailURL)
}

func (s *Service) UploadMedia(
	ctx context.Context,
	userID string,
	file multipart.File,
	filename string,
	contentType string,
	size int64,
) (*media.Media, error) {
	m, err := s.ingestMedia(ctx, userID, uuid.NewString(), file, filename, contentType, size)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// ingestMedia validates, processes and stores the media content under the
// given mediaID. It returns the fully populated record but does NOT persist
// it — callers decide whether to Create (new upload) or UpdateContent
// (replace).
func (s *Service) ingestMedia(
	ctx context.Context,
	userID string,
	mediaID string,
	file multipart.File,
	filename string,
	contentType string,
	size int64,
) (*media.Media, error) {
	processor := s.getProcessor(contentType)
	if processor == nil {
		return nil, fmt.Errorf("unsupported media type: %s", contentType)
	}
	mediaType := MediaTypeOf(contentType)

	// If the upload is large, avoid in-memory synchronous processing.
	if size > MaxSyncProcessingSize {
		// Persist raw to a temp file and upload the original to storage, leaving processing to async workers.
		tmp, err := os.CreateTemp("", "ums_upload_*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		defer func() {
			tmp.Close()
			os.Remove(tmp.Name())
		}()

		if _, err := io.Copy(tmp, file); err != nil {
			return nil, fmt.Errorf("failed to save upload to temp file: %w", err)
		}

		// Upload original
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		rawKey := fmt.Sprintf("raw/%s/%s/%s", mediaType, userID, mediaID)
		f, err := os.Open(tmp.Name())
		if err != nil {
			return nil, err
		}
		defer f.Close()

		_, err = s.Storage.Upload(ctx, rawKey, f, contentType)
		if err != nil {
			return nil, fmt.Errorf("failed to upload original: %w", err)
		}

		originalURL := fmt.Sprintf("%s/%s", s.Storage.PublicBaseURL(), rawKey)

		m := &media.Media{
			ID:           mediaID,
			UserID:       userID,
			Name:         filename,
			Type:         mediaType,
			OriginalURL:  originalURL,
			ProcessedURL: nil,
			ThumbnailURL: nil,
			Format:       contentType,
			SizeBytes:    size,
			Width:        0,
			Height:       0,
			Duration:     0,
			Status:       "uploaded",
			CreatedAt:    time.Now(),
		}

		log.Printf("Uploaded raw (deferred processing) %s for %s", mediaType, mediaID)
		return m, nil
	}

	// Small uploads: read into memory and process synchronously (with pre-decode checks in processor)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		log.Printf("Warning: seek failed (non-critical): %v", err)
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	originalBytes := buf.Bytes()

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
		if _, err := s.Storage.Upload(
			ctx,
			thumbnailKey,
			bytes.NewReader(result.ThumbnailBytes),
			result.ThumbnailContentType,
		); err != nil {
			log.Printf("Warning: failed to upload thumbnail: %v", err)
		} else {
			thPublic := fmt.Sprintf("%s/%s", s.Storage.PublicBaseURL(), thumbnailKey)
			thumbnailURL = &thPublic
		}
	}

	originalURL := fmt.Sprintf("%s/%s", s.Storage.PublicBaseURL(), rawKey)
	processedURL := fmt.Sprintf("%s/%s", s.Storage.PublicBaseURL(), processedKey)

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
		Status:       "ready",
		CreatedAt:    time.Now(),
	}

	log.Printf("Uploaded and processed %s for %s (%dx%d, %ds)", mediaType, mediaID, result.Width, result.Height, result.Duration)

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
	return s.repo.UpdateStatus(ctx, mediaID, userID, "trashed")
}

func (s *Service) HardDeleteMedia(
	ctx context.Context,
	mediaID string,
	userID string,
) error {
	m, err := s.repo.GetByIDForUser(ctx, mediaID, userID)
	if err != nil {
		return err
	}

	// Delete the row first: if storage cleanup fails afterwards, the worst
	// outcome is orphaned objects, never a database row pointing at deleted
	// files. If the row cannot be deleted, nothing below runs and every
	// object stays intact with its record.
	if err := s.repo.DeleteByID(ctx, mediaID, userID); err != nil {
		return err
	}

	cleanup := func(key string) {
		if key == "" {
			return
		}
		if err := s.Storage.Delete(ctx, key); err != nil {
			log.Printf("Warning: failed to delete storage object %s: %v", key, err)
		}
	}
	cleanup(extractKey(m.OriginalURL))
	if m.ProcessedURL != nil {
		cleanup(extractKey(*m.ProcessedURL))
	}
	if m.ThumbnailURL != nil {
		cleanup(extractKey(*m.ThumbnailURL))
	}

	return nil
}

func (s *Service) RestoreMedia(
	ctx context.Context,
	mediaID string,
	userID string,
) error {
	return s.repo.UpdateStatus(ctx, mediaID, userID, "ready")
}

func (s *Service) DeleteImage(
	ctx context.Context,
	imageID string,
	userID string,
) error {
	return s.DeleteMedia(ctx, imageID, userID)
}

func (s *Service) ReprocessMedia(
	ctx context.Context,
	mediaID string,
	userID string,
) (*media.Media, error) {
	m, err := s.repo.GetByIDForUser(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	if m.Status == "trashed" {
		return nil, media.ErrTrashed
	}

	if err := s.repo.UpdateStatus(ctx, mediaID, userID, "uploaded"); err != nil {
		return nil, err
	}

	m.Status = "uploaded"
	return m, nil
}

// BeginUploadRequest is the JSON payload of POST /media/uploads.
type BeginUploadRequest struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

// BeginDirectUpload validates a client-declared upload, reserves quota, and
// returns the new media ID plus a presigned PUT URL that sends the object
// straight to R2 without transiting this service. The row starts in
// "pending": it is invisible to list queries and is swept (row + object) if
// the client never completes.
func (s *Service) BeginDirectUpload(ctx context.Context, userID string, req BeginUploadRequest) (string, string, error) {
	mediaType := MediaTypeOf(req.ContentType)
	if mediaType == "unknown" {
		return "", "", ErrUnsupportedType
	}
	if req.Size <= 0 {
		return "", "", ErrInvalidRequest
	}
	if maxBytes := s.MaxBytesFor(req.ContentType); req.Size > maxBytes {
		return "", "", fmt.Errorf("%w (max %d MB)", ErrFileTooLarge, maxBytes/(1024*1024))
	}

	name := sanitizeName(req.Name)

	used, err := s.repo.SumSizeByUser(ctx, userID)
	if err != nil {
		return "", "", fmt.Errorf("failed to check storage quota: %w", err)
	}
	if used+req.Size > s.Limits.StorageQuotaBytes {
		return "", "", ErrQuotaExceeded
	}

	mediaID := uuid.NewString()
	rawKey := fmt.Sprintf("raw/%s/%s/%s", mediaType, userID, mediaID)

	m := &media.Media{
		ID:          mediaID,
		UserID:      userID,
		Name:        name,
		Type:        mediaType,
		OriginalURL: fmt.Sprintf("%s/%s", s.Storage.PublicBaseURL(), rawKey),
		Format:      req.ContentType,
		SizeBytes:   req.Size,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return "", "", fmt.Errorf("failed to reserve upload: %w", err)
	}

	uploadURL, err := s.Storage.PresignPut(ctx, rawKey, req.ContentType, presignTTL)
	if err != nil {
		// No client can ever satisfy this row; drop it now instead of making
		// the sweeper clean it up hours later.
		if derr := s.repo.DeleteByID(ctx, mediaID, userID); derr != nil {
			log.Printf("Warning: failed to roll back pending upload %s: %v", mediaID, derr)
		}
		return "", "", fmt.Errorf("failed to sign upload URL: %w", err)
	}

	log.Printf("BeginDirectUpload %s for %s (%d bytes, %s)", mediaType, userID, req.Size, mediaID)
	return mediaID, uploadURL, nil
}

// CompleteDirectUpload verifies the object the client PUT to R2 and flips the
// row from "pending" to "uploaded", which is what enqueues worker processing
// (the worker polls for status=uploaded; SSE then reports ready/failed).
// It is idempotent: completing an already-completed upload returns the row.
func (s *Service) CompleteDirectUpload(ctx context.Context, userID, mediaID string) (*media.Media, error) {
	m, err := s.repo.GetByIDForUser(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	if m.Status == "trashed" {
		return nil, media.ErrTrashed
	}
	if m.Status != "pending" {
		return m, nil
	}

	key := extractKey(m.OriginalURL)

	info, err := s.Storage.Head(ctx, key)
	if errors.Is(err, ErrObjectNotFound) {
		return nil, ErrUploadNotStarted
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat upload: %w", err)
	}

	// The presigned URL cannot pre-enforce size, so verify after the fact.
	// A smaller-than-declared object is fine (quota is then over-reserved
	// only in the safe direction); a larger one means the client lied about
	// the size it was quota-checked against, so the object is removed.
	if info.Size > m.SizeBytes {
		if derr := s.Storage.Delete(ctx, key); derr != nil {
			log.Printf("Warning: failed to delete oversized upload %s: %v", key, derr)
		}
		return nil, ErrSizeMismatch
	}

	header, err := s.Storage.GetRange(ctx, key, 0, 512)
	if err != nil {
		return nil, fmt.Errorf("failed to read upload header: %w", err)
	}
	// Family-level check: the sniffed bytes must belong to the same family
	// the caller declared. Subtype mismatches (declared mp4, stored webm)
	// are tolerated because ffmpeg autodetects the real container.
	if family := MediaTypeOf(media.DetectContentType(header)); family == "unknown" || family != m.Type {
		if derr := s.Storage.Delete(ctx, key); derr != nil {
			log.Printf("Warning: failed to delete mismatched upload %s: %v", key, derr)
		}
		return nil, ErrContentMismatch
	}

	m.SizeBytes = info.Size
	m.Status = "uploaded"
	if err := s.repo.UpdateContent(ctx, m); err != nil {
		return nil, err
	}

	log.Printf("CompleteDirectUpload %s (%s, %d bytes)", mediaID, m.Type, info.Size)
	return m, nil
}

// sanitizeName strips any path components and bounds the stored filename.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = filepath.Base(name)
	if name == "" || name == "." || string(filepath.Separator) == name {
		return "upload"
	}
	if runes := []rune(name); len(runes) > maxNameRunes {
		name = string(runes[:maxNameRunes])
	}
	return name
}
