package worker

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"universal-media-service/core/media"
	"universal-media-service/core/upload"
)

type Processor interface {
	Process(ctx context.Context, data []byte, contentType string) (*media.ProcessedResult, error)
	SupportedTypes() []string
}

type Worker struct {
	repo           media.Repository
	storage        upload.Storage
	imageProcessor Processor
	videoProcessor Processor
	audioProcessor Processor
	pollInterval   time.Duration
	batchSize      int
}

func New(
	repo media.Repository,
	storage upload.Storage,
	imageProcessor Processor,
	videoProcessor Processor,
	audioProcessor Processor,
) *Worker {
	return &Worker{
		repo:           repo,
		storage:        storage,
		imageProcessor: imageProcessor,
		videoProcessor: videoProcessor,
		audioProcessor: audioProcessor,
		pollInterval:   10 * time.Second,
		batchSize:      5,
	}
}

func (w *Worker) Start(ctx context.Context) {
	log.Println("Async worker started (poll interval: 10s)")
	for {
		select {
		case <-ctx.Done():
			log.Println("Async worker stopped")
			return
		case <-time.After(w.pollInterval):
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	items, err := w.repo.ListByStatus(ctx, "uploaded", w.batchSize)
	if err != nil {
		log.Printf("Worker: failed to list unprocessed items: %v", err)
		return
	}

	for _, item := range items {
		if err := w.processItem(ctx, item); err != nil {
			log.Printf("Worker: failed to process item %s: %v", item.ID, err)
		}
	}
}

func (w *Worker) processItem(ctx context.Context, item media.Media) error {
	processor := w.getProcessor(item.Type)
	if processor == nil {
		log.Printf("Worker: no processor for type %s, marking as failed", item.Type)
		_ = w.repo.UpdateStatus(ctx, item.ID, item.UserID, "failed")
		return fmt.Errorf("unsupported type: %s", item.Type)
	}

	log.Printf("Worker: processing item %s (%s)", item.ID, item.Type)

	sourceKey := extractKey(item.OriginalURL)
	data, err := w.storage.Get(ctx, sourceKey)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", sourceKey, err)
	}

	result, err := processor.Process(ctx, data, item.Format)
	if err != nil {
		_ = w.repo.UpdateStatus(ctx, item.ID, item.UserID, "failed")
		return fmt.Errorf("failed to process %s: %w", item.ID, err)
	}

	mediaID := item.ID
	userID := item.UserID
	mediaType := item.Type

	processedKey := fmt.Sprintf("processed/%s/%s/%s", mediaType, userID, mediaID)
	if _, err := w.storage.Upload(ctx, processedKey, bytes.NewReader(result.ProcessedBytes), result.ProcessedContentType); err != nil {
		return fmt.Errorf("failed to upload processed %s: %w", mediaID, err)
	}

	var thumbnailKey string
	if len(result.ThumbnailBytes) > 0 {
		thumbnailKey = fmt.Sprintf("thumbnail/%s/%s/%s", mediaType, userID, mediaID)
		if _, err := w.storage.Upload(ctx, thumbnailKey, bytes.NewReader(result.ThumbnailBytes), result.ThumbnailContentType); err != nil {
			log.Printf("Worker: warning: failed to upload thumbnail for %s: %v", mediaID, err)
			thumbnailKey = ""
		}
	}

	processedPublic := fmt.Sprintf("%s/%s", w.storage.PublicBaseURL(), processedKey)

	if err := w.updateProcessedResult(ctx, item.ID, item.UserID, processedPublic, thumbnailKey, result); err != nil {
		return fmt.Errorf("failed to update media record %s: %w", mediaID, err)
	}

	log.Printf("Worker: completed processing item %s (%s)", item.ID, item.Type)
	return nil
}

func (w *Worker) updateProcessedResult(ctx context.Context, id, userID, processedURL, thumbnailKey string, result *media.ProcessedResult) error {
	var thumbPublic string
	if thumbnailKey != "" {
		thumbPublic = fmt.Sprintf("%s/%s", w.storage.PublicBaseURL(), thumbnailKey)
	}
	return w.repo.UpdateProcessedResult(ctx, id, userID, processedURL, thumbPublic, result.Width, result.Height, result.Duration)
}

func (w *Worker) getProcessor(mediaType string) Processor {
	switch mediaType {
	case "image":
		return w.imageProcessor
	case "video":
		return w.videoProcessor
	case "audio":
		return w.audioProcessor
	default:
		return nil
	}
}

func extractKey(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return strings.TrimPrefix(publicURL, "/")
	}
	return strings.TrimPrefix(u.Path, "/")
}
