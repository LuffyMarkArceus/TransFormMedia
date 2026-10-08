package worker

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"universal-media-service/core/events"
	"universal-media-service/core/media"
	"universal-media-service/core/upload"
)

// --- fakes ---

type fakeRepo struct {
	items       []media.Media
	statuses    []string
	processed   int
	processedID string

	stale      []media.Media
	deletedIDs []string
	deleteErr  error
	sumSize    int64
}

func (f *fakeRepo) Create(context.Context, *media.Media) error { return nil }
func (f *fakeRepo) ListByUser(context.Context, string) ([]media.Media, error) {
	return nil, nil
}
func (f *fakeRepo) ListByUserAndType(context.Context, string, string) ([]media.Media, error) {
	return nil, nil
}
func (f *fakeRepo) ListPaginated(context.Context, media.ListParams) (*media.PaginatedResult, error) {
	return &media.PaginatedResult{Data: []media.Media{}, Total: 0}, nil
}
func (f *fakeRepo) ListByStatus(_ context.Context, _ string, _ int) ([]media.Media, error) {
	return f.items, nil
}
func (f *fakeRepo) GetByID(context.Context, string) (*media.Media, error) {
	return nil, media.ErrNotFound
}
func (f *fakeRepo) GetByIDForUser(context.Context, string, string) (*media.Media, error) {
	return nil, media.ErrNotFound
}
func (f *fakeRepo) DeleteByID(_ context.Context, id, _ string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}
func (f *fakeRepo) UpdateName(context.Context, string, string, string) error {
	return nil
}
func (f *fakeRepo) UpdateStatus(_ context.Context, _, _ string, status string) error {
	f.statuses = append(f.statuses, status)
	return nil
}
func (f *fakeRepo) UpdateProcessedResult(_ context.Context, id, _, _ string, _ string, _, _, _ int) error {
	f.processed++
	f.processedID = id
	return nil
}
func (f *fakeRepo) UpdateContent(context.Context, *media.Media) error { return nil }
func (f *fakeRepo) SumSizeByUser(context.Context, string) (int64, error) {
	return f.sumSize, nil
}
func (f *fakeRepo) ListStalePending(context.Context, time.Time, int) ([]media.Media, error) {
	return f.stale, nil
}

type fakeStorage struct {
	mu         sync.Mutex
	getBytes   []byte
	getErr     error
	getCount   int
	uploadKeys []string

	deleteErr   error
	deletedKeys []string
}

func (s *fakeStorage) Upload(_ context.Context, key string, _ io.Reader, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploadKeys = append(s.uploadKeys, key)
	return "https://cdn.example.com/" + key, nil
}
func (s *fakeStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deletedKeys = append(s.deletedKeys, key)
	return nil
}
func (s *fakeStorage) Get(_ context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCount++
	return s.getBytes, s.getErr
}

// DownloadTo mirrors Get: it counts as a download and streams getBytes into
// dst, so the worker's file-based pipeline sees the same fixture data.
func (s *fakeStorage) DownloadTo(_ context.Context, _ string, dst io.Writer) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCount++
	if s.getErr != nil {
		return 0, s.getErr
	}
	n, err := dst.Write(s.getBytes)
	return int64(n), err
}

func (s *fakeStorage) Head(context.Context, string) (upload.ObjectInfo, error) {
	return upload.ObjectInfo{}, upload.ErrObjectNotFound
}
func (s *fakeStorage) GetRange(context.Context, string, int64, int64) ([]byte, error) {
	return nil, upload.ErrObjectNotFound
}
func (s *fakeStorage) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	return "https://cdn.example.com/presigned", nil
}
func (s *fakeStorage) PublicBaseURL() string { return "https://cdn.example.com" }

type fakeProcessor struct {
	result *media.ProcessedResult
	err    error
	panic  bool
}

// ProcessFile materialises the configured ProcessedResult as files inside
// workDir, exactly like the real processors do.
func (p *fakeProcessor) ProcessFile(_ context.Context, _ string, _ string, workDir string) (*media.FileResult, error) {
	if p.panic {
		panic("processor exploded")
	}
	if p.err != nil {
		return nil, p.err
	}
	if p.result == nil {
		return nil, errors.New("no configured result")
	}
	r := p.result

	outPath := filepath.Join(workDir, "output")
	if err := os.WriteFile(outPath, r.ProcessedBytes, 0644); err != nil {
		return nil, err
	}
	fr := &media.FileResult{
		Width:             r.Width,
		Height:            r.Height,
		Duration:          r.Duration,
		OutputPath:        outPath,
		OutputContentType: r.ProcessedContentType,
	}
	if len(r.ThumbnailBytes) > 0 {
		thumbPath := filepath.Join(workDir, "thumb")
		if err := os.WriteFile(thumbPath, r.ThumbnailBytes, 0644); err != nil {
			return nil, err
		}
		fr.ThumbnailPath = thumbPath
		fr.ThumbnailContentType = r.ThumbnailContentType
	}
	return fr, nil
}
func (p *fakeProcessor) SupportedTypes() []string { return []string{"image"} }

// scriptedLease is a deterministic LeaseStore for worker logic tests.
type scriptedLease struct {
	acquireResult bool
	acquireErr    error
	pending       bool
	attempts      []int
	incErr        error

	acquired int
	released int
	resets   int
	incCalls int
}

func (s *scriptedLease) Acquire(context.Context, string) (bool, error) {
	s.acquired++
	return s.acquireResult, s.acquireErr
}
func (s *scriptedLease) Release(context.Context, string) error {
	s.released++
	return nil
}
func (s *scriptedLease) PendingRetry(context.Context, string) (bool, error) {
	return s.pending, nil
}
func (s *scriptedLease) IncRetry(context.Context, string) (int, error) {
	s.incCalls++
	if s.incErr != nil {
		return 0, s.incErr
	}
	if s.incCalls <= len(s.attempts) {
		return s.attempts[s.incCalls-1], nil
	}
	return s.attempts[len(s.attempts)-1], nil
}
func (s *scriptedLease) ResetRetry(context.Context, string) error {
	s.resets++
	return nil
}

type fakeEvents struct {
	mu        sync.Mutex
	published []events.Event
}

func (e *fakeEvents) Publish(_ context.Context, ev events.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.published = append(e.published, ev)
	return nil
}

func (e *fakeEvents) statuses() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.published {
		out = append(out, ev.Status)
	}
	return out
}

func testWorker(repo *fakeRepo, storage *fakeStorage, proc *fakeProcessor, lease LeaseStore, retryMax int) *Worker {
	return New(repo, storage, proc, &fakeProcessor{result: nil, err: errors.New("unused")}, &fakeProcessor{result: nil, err: errors.New("unused")},
		WithLeaseStore(lease),
		WithRetryMax(retryMax),
	)
}

func uploadedItem() media.Media {
	return media.Media{
		ID:          "media-1",
		UserID:      "user-1",
		Type:        "image",
		Format:      "image/jpeg",
		OriginalURL: "https://cdn.example.com/raw/1",
		Status:      "uploaded",
	}
}

// --- tests ---

func TestWorker_TransientFailuresStayUploadedUntilBudgetExhausted(t *testing.T) {
	repo := &fakeRepo{items: []media.Media{uploadedItem()}}
	storage := &fakeStorage{getBytes: []byte("jpeg")}
	proc := &fakeProcessor{err: errors.New("encode failed")}
	lease := &scriptedLease{acquireResult: true, attempts: []int{1, 2, 3}}
	eventsSink := &fakeEvents{}
	w := testWorker(repo, storage, proc, lease, 3)
	w.events = eventsSink

	for i := 0; i < 2; i++ {
		if err := w.processItem(context.Background(), repo.items[0]); err != nil {
			t.Fatalf("attempt %d: transient failures must return nil (retry scheduled), got %v", i+1, err)
		}
	}

	if len(repo.statuses) != 0 {
		t.Fatalf("must NOT be marked failed before the budget is exhausted, got statuses %v", repo.statuses)
	}
	if lease.incCalls != 2 {
		t.Fatalf("expected 2 retry bookkeeping calls, got %d", lease.incCalls)
	}
	if lease.resets != 0 {
		t.Fatalf("no reset expected while still retrying, got %d", lease.resets)
	}

	// Third attempt reaches the budget: permanent failure.
	err := w.processItem(context.Background(), repo.items[0])
	if err == nil {
		t.Fatal("expected an error once the retry budget is exhausted")
	}
	if len(repo.statuses) != 1 || repo.statuses[0] != "failed" {
		t.Fatalf("expected [failed], got %v", repo.statuses)
	}
	if lease.resets != 1 {
		t.Fatalf("expected retry history reset after permanent failure, got %d resets", lease.resets)
	}
	if got := eventsSink.statuses(); len(got) != 1 || got[0] != "failed" {
		t.Fatalf("expected one failed event, got %v", got)
	}
}

func TestWorker_SuccessClearsRetryAndPublishesReady(t *testing.T) {
	repo := &fakeRepo{items: []media.Media{uploadedItem()}}
	storage := &fakeStorage{getBytes: []byte("jpeg")}
	proc := &fakeProcessor{result: &media.ProcessedResult{
		ProcessedBytes:       []byte("webp"),
		ProcessedContentType: "image/webp",
		ThumbnailBytes:       []byte("thumb"),
		ThumbnailContentType: "image/jpeg",
		Width:                100,
		Height:               50,
	}}
	lease := &scriptedLease{acquireResult: true}
	eventsSink := &fakeEvents{}
	w := testWorker(repo, storage, proc, lease, 5)
	w.events = eventsSink

	if err := w.processItem(context.Background(), repo.items[0]); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.processed != 1 || repo.processedID != "media-1" {
		t.Fatalf("expected processed result for media-1, got processed=%d id=%s", repo.processed, repo.processedID)
	}
	if lease.resets != 1 {
		t.Fatalf("retry history must reset after success, got %d resets", lease.resets)
	}
	if lease.released != 1 {
		t.Fatalf("lease must be released after success, got %d releases", lease.released)
	}
	if got := eventsSink.statuses(); len(got) != 1 || got[0] != "ready" {
		t.Fatalf("expected one ready event, got %v", got)
	}
}

func TestWorker_UnsupportedTypeFailsImmediately(t *testing.T) {
	item := uploadedItem()
	item.Type = "vector"
	repo := &fakeRepo{items: []media.Media{item}}
	lease := &scriptedLease{acquireResult: true}
	eventsSink := &fakeEvents{}
	w := testWorker(repo, &fakeStorage{}, &fakeProcessor{}, lease, 5)
	w.events = eventsSink

	err := w.processItem(context.Background(), item)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if len(repo.statuses) != 1 || repo.statuses[0] != "failed" {
		t.Fatalf("expected permanent failure, got %v", repo.statuses)
	}
	if lease.incCalls != 0 {
		t.Fatalf("unsupported type must not consume retry budget, got %d inc calls", lease.incCalls)
	}
	if lease.resets != 1 {
		t.Fatalf("expected retry history reset, got %d", lease.resets)
	}
}

func TestWorker_LeaseNotAcquiredSkipsItem(t *testing.T) {
	repo := &fakeRepo{items: []media.Media{uploadedItem()}}
	storage := &fakeStorage{getBytes: []byte("jpeg")}
	w := testWorker(repo, storage, &fakeProcessor{result: &media.ProcessedResult{ProcessedBytes: []byte("x"), ProcessedContentType: "image/webp"}},
		&scriptedLease{acquireResult: false}, 5)

	if err := w.processItem(context.Background(), repo.items[0]); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if storage.getCount != 0 {
		t.Fatalf("item owned by another replica must not be downloaded, got %d gets", storage.getCount)
	}
	if len(repo.statuses) != 0 {
		t.Fatalf("status must not change for an unleased item, got %v", repo.statuses)
	}
}

func TestWorker_PendingBackoffSkipsItem(t *testing.T) {
	repo := &fakeRepo{items: []media.Media{uploadedItem()}}
	storage := &fakeStorage{getBytes: []byte("jpeg")}
	w := testWorker(repo, storage, &fakeProcessor{result: &media.ProcessedResult{ProcessedBytes: []byte("x"), ProcessedContentType: "image/webp"}},
		&scriptedLease{acquireResult: true, pending: true}, 5)

	if err := w.processItem(context.Background(), repo.items[0]); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if storage.getCount != 0 {
		t.Fatalf("item inside its backoff window must not be reprocessed, got %d gets", storage.getCount)
	}
	if repo.processed != 0 {
		t.Fatalf("backed-off item must not reach the result update, got %d", repo.processed)
	}
}

func TestWorker_PanicInProcessorIsContained(t *testing.T) {
	repo := &fakeRepo{items: []media.Media{uploadedItem()}}
	storage := &fakeStorage{getBytes: []byte("jpeg")}
	proc := &fakeProcessor{panic: true}
	lease := &scriptedLease{acquireResult: true}
	eventsSink := &fakeEvents{}
	w := testWorker(repo, storage, proc, lease, 5)
	w.events = eventsSink

	err := w.processItem(context.Background(), repo.items[0])
	if err == nil {
		t.Fatal("expected a recovered panic to surface as an error")
	}
	if lease.released != 1 {
		t.Fatalf("lease must be released even when the processor panics, got %d releases", lease.released)
	}
	if len(repo.statuses) != 0 {
		t.Fatalf("item must stay uploaded after a contained panic, got %v", repo.statuses)
	}

	// The worker loop must survive: a second batch with a healthy processor
	// still runs.
	proc.panic = false
	proc.result = &media.ProcessedResult{ProcessedBytes: []byte("x"), ProcessedContentType: "image/webp"}
	if err := w.processItem(context.Background(), repo.items[0]); err != nil {
		t.Fatalf("worker must keep working after a panic, got %v", err)
	}
	if repo.processed != 1 {
		t.Fatalf("expected the follow-up item to be processed, got %d", repo.processed)
	}
}

func TestMemLeaseStore_AcquireIsExclusiveUntilReleased(t *testing.T) {
	store := NewMemLeaseStore(30 * time.Second)
	ctx := context.Background()

	ok, err := store.Acquire(ctx, "wlease:media-1")
	if err != nil || !ok {
		t.Fatalf("first acquire must succeed (ok=%v err=%v)", ok, err)
	}
	if ok, _ := store.Acquire(ctx, "wlease:media-1"); ok {
		t.Fatal("a second acquire on a held lease must fail")
	}
	if ok, _ := store.Acquire(ctx, "wlease:media-2"); !ok {
		t.Fatal("a different key must be acquirable")
	}

	if err := store.Release(ctx, "wlease:media-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if ok, _ := store.Acquire(ctx, "wlease:media-1"); !ok {
		t.Fatal("key must be acquirable after release")
	}
}

func TestMemLeaseStore_RetryBackoffGateAndReset(t *testing.T) {
	store := NewMemLeaseStore(time.Hour)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		attempt, err := store.IncRetry(ctx, "media-1")
		if err != nil {
			t.Fatalf("IncRetry: %v", err)
		}
		if attempt != i {
			t.Fatalf("expected attempt %d, got %d", i, attempt)
		}
		if pending, _ := store.PendingRetry(ctx, "media-1"); !pending {
			t.Fatalf("attempt %d must arm the backoff gate", i)
		}
	}
	if pending, _ := store.PendingRetry(ctx, "media-other"); pending {
		t.Fatal("an unrelated media must not be gated")
	}

	if err := store.ResetRetry(ctx, "media-1"); err != nil {
		t.Fatalf("ResetRetry: %v", err)
	}
	if pending, _ := store.PendingRetry(ctx, "media-1"); pending {
		t.Fatal("backoff gate must clear after ResetRetry")
	}
	attempt, _ := store.IncRetry(ctx, "media-1")
	if attempt != 1 {
		t.Fatalf("retry budget must restart after reset, got attempt %d", attempt)
	}
}

func TestSweepPending_DeletesStaleRowAndObject(t *testing.T) {
	repo := &fakeRepo{stale: []media.Media{{
		ID:          "media-1",
		UserID:      "user-1",
		Type:        "video",
		Status:      "pending",
		OriginalURL: "https://cdn.example.com/raw/video/user-1/media-1",
	}}}
	storage := &fakeStorage{}
	w := testWorker(repo, storage, &fakeProcessor{}, &scriptedLease{acquireResult: true}, 5)

	w.sweepPending(context.Background())

	if len(storage.deletedKeys) != 1 || storage.deletedKeys[0] != "raw/video/user-1/media-1" {
		t.Fatalf("expected the object to be deleted, got %v", storage.deletedKeys)
	}
	if len(repo.deletedIDs) != 1 || repo.deletedIDs[0] != "media-1" {
		t.Fatalf("expected the stale row to be deleted, got %v", repo.deletedIDs)
	}
}

func TestSweepPending_KeepsRowWhenObjectDeleteFails(t *testing.T) {
	repo := &fakeRepo{stale: []media.Media{{
		ID:          "media-1",
		UserID:      "user-1",
		Status:      "pending",
		OriginalURL: "https://cdn.example.com/raw/video/user-1/media-1",
	}}}
	storage := &fakeStorage{deleteErr: errors.New("r2 unavailable")}
	w := testWorker(repo, storage, &fakeProcessor{}, &scriptedLease{acquireResult: true}, 5)

	w.sweepPending(context.Background())

	if len(repo.deletedIDs) != 0 {
		t.Fatalf("row must survive a failed object delete for retry, got %v", repo.deletedIDs)
	}
}

func TestSweepPending_DeletesRowWhenObjectAlreadyGone(t *testing.T) {
	repo := &fakeRepo{stale: []media.Media{{
		ID:          "media-1",
		UserID:      "user-1",
		Status:      "pending",
		OriginalURL: "https://cdn.example.com/raw/video/user-1/media-1",
	}}}
	storage := &fakeStorage{deleteErr: upload.ErrObjectNotFound}
	w := testWorker(repo, storage, &fakeProcessor{}, &scriptedLease{acquireResult: true}, 5)

	w.sweepPending(context.Background())

	if len(repo.deletedIDs) != 1 {
		t.Fatalf("a missing object must not block the row sweep, got %v", repo.deletedIDs)
	}
}
