package upload

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"

	"universal-media-service/core/media"
)

// replaceRepo records every mutating call so tests can assert on the exact
// persistence behaviour of ReplaceMedia.
type replaceRepo struct {
	existing         *media.Media
	created          []*media.Media
	deletedIDs       []string
	updatedContents  []*media.Media
	createErr        error
	updateContentErr error
}

func (r *replaceRepo) Create(_ context.Context, m *media.Media) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = append(r.created, m)
	return nil
}
func (r *replaceRepo) ListByUser(context.Context, string) ([]media.Media, error) {
	return nil, nil
}
func (r *replaceRepo) ListByUserAndType(context.Context, string, string) ([]media.Media, error) {
	return nil, nil
}
func (r *replaceRepo) ListPaginated(context.Context, media.ListParams) (*media.PaginatedResult, error) {
	return &media.PaginatedResult{Data: []media.Media{}, Total: 0}, nil
}
func (r *replaceRepo) ListByStatus(context.Context, string, int) ([]media.Media, error) {
	return nil, nil
}
func (r *replaceRepo) GetByID(context.Context, string) (*media.Media, error) {
	return nil, media.ErrNotFound
}
func (r *replaceRepo) GetByIDForUser(_ context.Context, id, userID string) (*media.Media, error) {
	if r.existing == nil || r.existing.ID != id || r.existing.UserID != userID {
		return nil, media.ErrNotFound
	}
	cp := *r.existing
	return &cp, nil
}
func (r *replaceRepo) DeleteByID(_ context.Context, id, _ string) error {
	r.deletedIDs = append(r.deletedIDs, id)
	return nil
}
func (r *replaceRepo) UpdateName(context.Context, string, string, string) error {
	return nil
}
func (r *replaceRepo) UpdateStatus(context.Context, string, string, string) error {
	return nil
}
func (r *replaceRepo) UpdateProcessedResult(context.Context, string, string, string, string, int, int, int) error {
	return nil
}
func (r *replaceRepo) UpdateContent(_ context.Context, m *media.Media) error {
	if r.updateContentErr != nil {
		return r.updateContentErr
	}
	cp := *m
	r.updatedContents = append(r.updatedContents, &cp)
	return nil
}

// replaceStorage records uploads and deletes by key.
type replaceStorage struct {
	uploadedKeys []string
	deletedKeys  []string
}

func (s *replaceStorage) Upload(_ context.Context, key string, _ io.Reader, _ string) (string, error) {
	s.uploadedKeys = append(s.uploadedKeys, key)
	return key, nil
}
func (s *replaceStorage) Delete(_ context.Context, key string) error {
	s.deletedKeys = append(s.deletedKeys, key)
	return nil
}
func (s *replaceStorage) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (s *replaceStorage) PublicBaseURL() string {
	return "https://cdn.example.com"
}

// memFile adapts a bytes.Reader to the multipart.File interface.
type memFile struct{ *bytes.Reader }

func (memFile) Close() error { return nil }

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("failed to encode fixture png: %v", err)
	}
	return buf.Bytes()
}

func newReplaceFixture() (*replaceRepo, *replaceStorage, *Service) {
	processed := "https://cdn.example.com/processed/image/owner-user/media-1"
	thumb := "https://cdn.example.com/thumbnail/image/owner-user/media-1"
	repo := &replaceRepo{
		existing: &media.Media{
			ID:           "media-1",
			UserID:       "owner-user",
			Name:         "old.png",
			Type:         "image",
			OriginalURL:  "https://cdn.example.com/raw/image/owner-user/media-1",
			ProcessedURL: &processed,
			ThumbnailURL: &thumb,
			Format:       "image/png",
			Status:       "ready",
		},
	}
	storage := &replaceStorage{}
	return repo, storage, NewService(repo, storage)
}

// Regression test for the duplicate-row bug: ReplaceMedia used to insert a
// brand-new row (fresh UUID from the inner upload) and then re-insert under
// the old ID, orphaning a duplicate record on every replace.
func TestReplaceMedia_UpdatesInPlaceWithoutDuplicateRows(t *testing.T) {
	repo, storage, svc := newReplaceFixture()

	m, err := svc.ReplaceMedia(
		context.Background(),
		"media-1",
		"owner-user",
		memFile{bytes.NewReader(tinyPNG(t))},
		"new.png",
		"image/png",
		int64(len(tinyPNG(t))),
	)
	if err != nil {
		t.Fatalf("ReplaceMedia failed: %v", err)
	}

	if len(repo.created) != 0 {
		t.Errorf("expected 0 Create calls (in-place update), got %d — replace must not insert new rows", len(repo.created))
	}
	if len(repo.deletedIDs) != 0 {
		t.Errorf("expected 0 DeleteByID calls, got %v — replace must not delete rows", repo.deletedIDs)
	}
	if len(repo.updatedContents) != 1 {
		t.Fatalf("expected exactly 1 UpdateContent call, got %d", len(repo.updatedContents))
	}
	if got := repo.updatedContents[0].ID; got != "media-1" {
		t.Errorf("UpdateContent wrote wrong ID: %s", got)
	}
	if m.ID != "media-1" {
		t.Errorf("returned media ID = %s, want media-1", m.ID)
	}
	if m.Name != "new.png" {
		t.Errorf("returned media name = %s, want new.png", m.Name)
	}
	// Same media type reuses the same storage keys, so nothing is stale.
	if len(storage.deletedKeys) != 0 {
		t.Errorf("expected no storage deletes for same-key replace, got %v", storage.deletedKeys)
	}
}

// Regression test for data-loss ordering: the old code deleted the existing
// storage objects BEFORE attempting the new upload, so a failed replace left
// the record pointing at deleted files.
func TestReplaceMedia_FailedIngestLeavesOldRecordAndStorageIntact(t *testing.T) {
	repo, storage, svc := newReplaceFixture()

	_, err := svc.ReplaceMedia(
		context.Background(),
		"media-1",
		"owner-user",
		memFile{bytes.NewReader([]byte("not an image"))},
		"evil.png",
		"application/pdf",
		12,
	)
	if err == nil {
		t.Fatal("expected error for unsupported content type")
	}
	if len(repo.updatedContents) != 0 {
		t.Errorf("expected no UpdateContent on failed ingest, got %d", len(repo.updatedContents))
	}
	if len(repo.deletedIDs) != 0 {
		t.Errorf("expected no row deletion on failed ingest, got %v", repo.deletedIDs)
	}
	if len(storage.deletedKeys) != 0 {
		t.Errorf("expected no storage deletes on failed ingest, got %v", storage.deletedKeys)
	}
	if len(storage.uploadedKeys) != 0 {
		t.Errorf("expected no storage uploads on failed ingest, got %v", storage.uploadedKeys)
	}
}

func TestReplaceMedia_RejectsTrashedMedia(t *testing.T) {
	repo, storage, svc := newReplaceFixture()
	repo.existing.Status = "trashed"

	_, err := svc.ReplaceMedia(
		context.Background(),
		"media-1",
		"owner-user",
		memFile{bytes.NewReader(tinyPNG(t))},
		"new.png",
		"image/png",
		42,
	)
	if !errors.Is(err, media.ErrTrashed) {
		t.Fatalf("expected ErrTrashed, got %v", err)
	}
	if len(storage.deletedKeys) != 0 || len(storage.uploadedKeys) != 0 {
		t.Errorf("expected zero storage operations for trashed media, uploads=%v deletes=%v", storage.uploadedKeys, storage.deletedKeys)
	}
	if len(repo.updatedContents) != 0 {
		t.Errorf("expected no UpdateContent for trashed media")
	}
}

// A large (async-path) replace produces no processed/thumbnail variants, so
// the previous version's derived objects must be cleaned up — but only after
// the DB row has been updated, and never the raw object that was reused.
func TestReplaceMedia_AsyncReplaceDeletesOnlyStaleDerivedAssets(t *testing.T) {
	repo, storage, svc := newReplaceFixture()

	pngBytes := tinyPNG(t)
	_, err := svc.ReplaceMedia(
		context.Background(),
		"media-1",
		"owner-user",
		memFile{bytes.NewReader(pngBytes)},
		"new.png",
		"image/png",
		MaxSyncProcessingSize+1, // force the deferred-processing path
	)
	if err != nil {
		t.Fatalf("ReplaceMedia failed: %v", err)
	}

	wantDeleted := map[string]bool{
		"processed/image/owner-user/media-1": true,
		"thumbnail/image/owner-user/media-1": true,
	}
	gotDeleted := map[string]bool{}
	for _, k := range storage.deletedKeys {
		gotDeleted[k] = true
	}
	if len(gotDeleted) != len(wantDeleted) {
		t.Errorf("deleted keys = %v, want exactly %v", storage.deletedKeys, wantDeleted)
	}
	for k := range wantDeleted {
		if !gotDeleted[k] {
			t.Errorf("expected stale asset %q to be deleted", k)
		}
	}
	if gotDeleted["raw/image/owner-user/media-1"] {
		t.Error("raw object was reused by the new version and must not be deleted")
	}
	if len(repo.updatedContents) != 1 {
		t.Fatalf("expected 1 UpdateContent, got %d", len(repo.updatedContents))
	}
	if repo.updatedContents[0].ProcessedURL != nil {
		t.Error("async replace should leave processed_url unset")
	}
}

func TestReplaceMedia_NotOwnerFails(t *testing.T) {
	_, storage, svc := newReplaceFixture()

	_, err := svc.ReplaceMedia(
		context.Background(),
		"media-1",
		"attacker-user",
		memFile{bytes.NewReader(tinyPNG(t))},
		"new.png",
		"image/png",
		42,
	)
	if !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-owner, got %v", err)
	}
	if len(storage.uploadedKeys) != 0 || len(storage.deletedKeys) != 0 {
		t.Errorf("expected zero storage operations for non-owner, got uploads=%v deletes=%v", storage.uploadedKeys, storage.deletedKeys)
	}
}
