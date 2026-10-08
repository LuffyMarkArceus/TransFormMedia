package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"universal-media-service/core/events"
	"universal-media-service/core/media"
	"universal-media-service/core/upload"
)

// Processor runs a media job against a local file. The worker streams the
// source object into a temp file first, so no processor ever holds a whole
// file in memory — multi-hundred-MB jobs fit in a 512 Mi instance.
type Processor interface {
	// ProcessFile reads inputPath and writes the outputs into the
	// caller-owned workDir, returning their paths (see media.FileResult).
	ProcessFile(ctx context.Context, inputPath, contentType, workDir string) (*media.FileResult, error)
	SupportedTypes() []string
}

// LeaseStore claims items so a multi-replica fleet does not process the same
// row twice, and tracks a per-item retry budget with escalating backoff.
type LeaseStore interface {
	// Acquire claims key (returning false if another replica holds it) for a
	// bounded hold period so a crashed worker's item gets reprocessed.
	Acquire(ctx context.Context, key string) (bool, error)
	// Release returns an acquired claim.
	Release(ctx context.Context, key string) error
	// PendingRetry reports whether mediaID is inside its backoff window after
	// a prior transient failure.
	PendingRetry(ctx context.Context, mediaID string) (bool, error)
	// IncRetry records one more failed attempt for mediaID, returns the
	// running attempt count, and arms the backoff gate.
	IncRetry(ctx context.Context, mediaID string) (int, error)
	// ResetRetry clears the retry history after a successful run.
	ResetRetry(ctx context.Context, mediaID string) error
}

// MemLeaseStore is the single-replica fallback used when Redis is absent. It
// provides the same retry semantics as RedisLease but cannot arbitrate
// between processes; each replica keeps its own in-memory budget.
type MemLeaseStore struct {
	mu         sync.Mutex
	retryBase  time.Duration
	attempts   map[string]int
	leaseUntil map[string]time.Time
	retryUntil map[string]time.Time
}

func NewMemLeaseStore(retryBase time.Duration) *MemLeaseStore {
	return &MemLeaseStore{
		retryBase:  retryBase,
		attempts:   make(map[string]int),
		leaseUntil: make(map[string]time.Time),
		retryUntil: make(map[string]time.Time),
	}
}

func (m *MemLeaseStore) Acquire(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if until, ok := m.leaseUntil[key]; ok && time.Now().Before(until) {
		return false, nil
	}
	m.leaseUntil[key] = time.Now().Add(holdTTL)
	return true, nil
}

func (m *MemLeaseStore) Release(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.leaseUntil, key)
	return nil
}

func (m *MemLeaseStore) PendingRetry(_ context.Context, mediaID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Now().Before(m.retryUntil[mediaID]), nil
}

func (m *MemLeaseStore) IncRetry(_ context.Context, mediaID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts[mediaID]++
	attempt := m.attempts[mediaID]
	m.retryUntil[mediaID] = time.Now().Add(backoffFor(m.retryBase, attempt))
	return attempt, nil
}

func (m *MemLeaseStore) ResetRetry(_ context.Context, mediaID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.attempts, mediaID)
	delete(m.retryUntil, mediaID)
	return nil
}

const (
	// holdTTL bounds a claim so a crashed worker's item is never stuck; the
	// Redis lease uses an identical TTL.
	holdTTL = 10 * time.Minute
	// itemTimeout bounds one job (download + process + re-upload + DB write)
	// so a hung operation cannot wedge the poll loop forever.
	itemTimeout = 10 * time.Minute

	// pendingSweepInterval is how often abandoned direct uploads are reaped.
	pendingSweepInterval = 5 * time.Minute
	// pendingSweepAge is how long a "pending" row may sit uncompleted before
	// its row and object are deleted. Generous: slow clients get presigned
	// URLs valid 30 minutes and up to ~2h from begin to complete.
	pendingSweepAge = 2 * time.Hour
	// pendingSweepBatch bounds one sweep run.
	pendingSweepBatch = 50
)

// backoffFor doubles the base per attempt, capping at 15 minutes. Kept in
// sync with adapters/lease so Redis and in-memory stores agree.
func backoffFor(base time.Duration, attempt int) time.Duration {
	shift := attempt - 1
	if shift > 4 {
		shift = 4
	}
	d := base << shift
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d
}

type Option func(*Worker)

// WithLeaseStore attaches a claim/retry store. Without one the worker falls
// back to single-replica retries via MemLeaseStore.
func WithLeaseStore(s LeaseStore) Option { return func(w *Worker) { w.leases = s } }

// WithRetryMax sets how many attempts run before an item is marked failed.
func WithRetryMax(n int) Option {
	return func(w *Worker) {
		if n > 0 {
			w.retryMax = n
		}
	}
}

// WithEvents attaches a status publisher (SSE in the API layer). A nil
// publisher is tolerated.
func WithEvents(p events.Publisher) Option { return func(w *Worker) { w.events = p } }

// WithPollInterval overrides the default 10s poll cadence (tests only).
func WithPollInterval(d time.Duration) Option {
	return func(w *Worker) {
		if d > 0 {
			w.pollInterval = d
		}
	}
}

type Worker struct {
	repo           media.Repository
	storage        upload.Storage
	imageProcessor Processor
	videoProcessor Processor
	audioProcessor Processor
	pollInterval   time.Duration
	batchSize      int
	leases         LeaseStore
	retryMax       int
	events         events.Publisher
}

func New(
	repo media.Repository,
	storage upload.Storage,
	imageProcessor Processor,
	videoProcessor Processor,
	audioProcessor Processor,
	opts ...Option,
) *Worker {
	w := &Worker{
		repo:           repo,
		storage:        storage,
		imageProcessor: imageProcessor,
		videoProcessor: videoProcessor,
		audioProcessor: audioProcessor,
		pollInterval:   10 * time.Second,
		batchSize:      5,
		leases:         NewMemLeaseStore(30 * time.Second),
		retryMax:       5,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func (w *Worker) Start(ctx context.Context) {
	log.Println("Async worker started (poll interval: 10s)")
	sweepTicker := time.NewTicker(pendingSweepInterval)
	defer sweepTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("Async worker stopped")
			return
		case <-time.After(w.pollInterval):
			w.runBatch(ctx)
		case <-sweepTicker.C:
			w.sweepPending(ctx)
		}
	}
}

// runBatch wraps the batch in a recover so one bad batch (e.g. a panicking
// repository) cannot kill the whole poll loop.
func (w *Worker) runBatch(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Worker: recovered from panic during batch: %v", r)
		}
	}()

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

// processItem guards a single job against panics (released lease via
// defer), so a processor bug corrupts one item instead of the worker.
func (w *Worker) processItem(ctx context.Context, item media.Media) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Worker: recovered from panic processing %s: %v", item.ID, r)
			err = fmt.Errorf("panic processing %s: %v", item.ID, r)
		}
	}()

	jobCtx, cancel := context.WithTimeout(ctx, itemTimeout)
	defer cancel()

	leaseKey := leaseKeyFor(item.ID)
	if w.leases != nil {
		if pending, err := w.leases.PendingRetry(jobCtx, item.ID); err != nil {
			log.Printf("Worker: failed to check retry state for %s: %v", item.ID, err)
		} else if pending {
			return nil // still in backoff window
		}

		ok, err := w.leases.Acquire(jobCtx, leaseKey)
		if err != nil {
			log.Printf("Worker: failed to acquire lease for %s: %v", item.ID, err)
		} else if !ok {
			return nil // another replica owns this item
		}
		defer w.leases.Release(jobCtx, leaseKey)
	}

	return w.process(jobCtx, item)
}

func (w *Worker) process(ctx context.Context, item media.Media) error {
	processor := w.getProcessor(item.Type)
	if processor == nil {
		return w.failPermanently(ctx, item, fmt.Errorf("unsupported type: %s", item.Type))
	}

	log.Printf("Worker: processing item %s (%s)", item.ID, item.Type)

	// Stream the source object into a temp file; only bounded buffers live
	// in memory from here on.
	tmpDir, err := os.MkdirTemp("", "worker-*")
	if err != nil {
		return w.handleFailure(ctx, item, fmt.Errorf("failed to create work dir: %w", err))
	}
	defer os.RemoveAll(tmpDir)

	sourceKey := extractKey(item.OriginalURL)
	inputPath := filepath.Join(tmpDir, "input")
	if err := w.downloadTo(ctx, sourceKey, inputPath); err != nil {
		return w.handleFailure(ctx, item, fmt.Errorf("failed to download %s: %w", sourceKey, err))
	}

	result, err := processor.ProcessFile(ctx, inputPath, item.Format, tmpDir)
	if err != nil {
		return w.handleFailure(ctx, item, fmt.Errorf("failed to process %s: %w", item.ID, err))
	}

	mediaID := item.ID
	userID := item.UserID
	mediaType := item.Type

	processedKey := fmt.Sprintf("processed/%s/%s/%s", mediaType, userID, mediaID)
	if err := w.uploadFile(ctx, processedKey, result.OutputPath, result.OutputContentType); err != nil {
		return w.handleFailure(ctx, item, fmt.Errorf("failed to upload processed %s: %w", mediaID, err))
	}

	thumbnailKey := ""
	if result.ThumbnailPath != "" {
		thumbnailKey = fmt.Sprintf("thumbnail/%s/%s/%s", mediaType, userID, mediaID)
		if err := w.uploadFile(ctx, thumbnailKey, result.ThumbnailPath, result.ThumbnailContentType); err != nil {
			log.Printf("Worker: warning: failed to upload thumbnail for %s: %v", mediaID, err)
			thumbnailKey = ""
		}
	}

	processedPublic := fmt.Sprintf("%s/%s", w.storage.PublicBaseURL(), processedKey)

	if err := w.updateProcessedResult(ctx, item.ID, item.UserID, processedPublic, thumbnailKey, result); err != nil {
		return w.handleFailure(ctx, item, fmt.Errorf("failed to update media record %s: %w", mediaID, err))
	}

	if w.leases != nil {
		if err := w.leases.ResetRetry(ctx, item.ID); err != nil {
			log.Printf("Worker: warning: failed to clear retry state for %s: %v", item.ID, err)
		}
	}
	w.publish(ctx, item.UserID, "ready", item.ID)
	log.Printf("Worker: completed processing item %s (%s)", item.ID, item.Type)
	return nil
}

// downloadTo streams an object to a local file path.
func (w *Worker) downloadTo(ctx context.Context, key, dstPath string) error {
	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	_, err = w.storage.DownloadTo(ctx, key, f)
	cerr := f.Close()
	if err != nil {
		return err
	}
	return cerr
}

// uploadFile streams a local file into storage without buffering it.
func (w *Worker) uploadFile(ctx context.Context, key, path, contentType string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = w.storage.Upload(ctx, key, f, contentType)
	return err
}

// sweepPending reaps abandoned direct uploads: "pending" rows older than
// pendingSweepAge lose their object and their row. Runs on its own ticker;
// failures are logged and retried next tick (stale rows stay queryable).
func (w *Worker) sweepPending(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Worker: recovered from panic during pending sweep: %v", r)
		}
	}()

	cutoff := time.Now().Add(-pendingSweepAge)
	stale, err := w.repo.ListStalePending(ctx, cutoff, pendingSweepBatch)
	if err != nil {
		log.Printf("Worker: failed to list stale pending uploads: %v", err)
		return
	}
	for _, m := range stale {
		key := extractKey(m.OriginalURL)
		if err := w.storage.Delete(ctx, key); err != nil && !errors.Is(err, upload.ErrObjectNotFound) {
			log.Printf("Worker: sweep %s: object delete failed (will retry): %v", m.ID, err)
			continue
		}
		if err := w.repo.DeleteByID(ctx, m.ID, m.UserID); err != nil {
			log.Printf("Worker: sweep %s: row delete failed (will retry): %v", m.ID, err)
			continue
		}
		log.Printf("Worker: swept abandoned pending upload %s (%s, %d bytes)", m.ID, m.Type, m.SizeBytes)
	}
}

// handleFailure decides whether a transient failure gets another attempt.
// Transient failures stay "uploaded" and re-run after an escalating backoff;
// when the budget is exhausted the item is marked failed for good.
func (w *Worker) handleFailure(ctx context.Context, item media.Media, err error) error {
	attempt, incErr := w.leases.IncRetry(ctx, item.ID)
	if incErr != nil {
		log.Printf("Worker: retry bookkeeping failed for %s: %v", item.ID, incErr)
		return w.failPermanently(ctx, item, err)
	}

	if attempt >= w.retryMax {
		return w.failPermanently(ctx, item, err)
	}

	log.Printf("Worker: transient failure processing %s (attempt %d/%d): %v", item.ID, attempt, w.retryMax, err)
	return nil
}

// failPermanently marks the item as failed, clears its retry history, and
// announces the transition.
func (w *Worker) failPermanently(ctx context.Context, item media.Media, err error) error {
	log.Printf("Worker: giving up on item %s: %v", item.ID, err)
	if err := w.repo.UpdateStatus(ctx, item.ID, item.UserID, "failed"); err != nil {
		return fmt.Errorf("failed to mark %s as failed: %w", item.ID, err)
	}
	if w.leases != nil {
		w.leases.ResetRetry(ctx, item.ID)
	}
	w.publish(ctx, item.UserID, "failed", item.ID)
	return err
}

func (w *Worker) publish(ctx context.Context, userID, status, mediaID string) {
	if w.events == nil {
		return
	}
	if err := w.events.Publish(ctx, events.Event{Type: events.KindStatus, MediaID: mediaID, UserID: userID, Status: status}); err != nil {
		log.Printf("Worker: warning: failed to publish %s event for %s: %v", status, mediaID, err)
	}
}

func leaseKeyFor(mediaID string) string {
	return "wlease:" + mediaID
}

func (w *Worker) updateProcessedResult(ctx context.Context, id, userID, processedURL, thumbnailKey string, result *media.FileResult) error {
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
