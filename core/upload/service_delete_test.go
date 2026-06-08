package upload

import (
	"context"
	"io"
	"testing"

	"universal-media-service/core/media"
)

type stubRepo struct {
	mediaByID map[string]*media.Media
}

func (s *stubRepo) Create(context.Context, *media.Media) error { return nil }
func (s *stubRepo) ListByUser(context.Context, string) ([]media.Media, error) {
	return nil, nil
}
func (s *stubRepo) ListByUserAndType(context.Context, string, string) ([]media.Media, error) {
	return nil, nil
}
func (s *stubRepo) ListByStatus(context.Context, string, int) ([]media.Media, error) {
	return nil, nil
}
func (s *stubRepo) GetByID(_ context.Context, id string) (*media.Media, error) {
	m, ok := s.mediaByID[id]
	if !ok {
		return nil, media.ErrNotFound
	}
	return m, nil
}
func (s *stubRepo) GetByIDForUser(_ context.Context, id, userID string) (*media.Media, error) {
	m, ok := s.mediaByID[id]
	if !ok || m.UserID != userID {
		return nil, media.ErrNotFound
	}
	return m, nil
}
func (s *stubRepo) DeleteByID(context.Context, string, string) error { return nil }
func (s *stubRepo) ListPaginated(context.Context, media.ListParams) (*media.PaginatedResult, error) {
	return &media.PaginatedResult{Data: []media.Media{}, Total: 0}, nil
}
func (s *stubRepo) UpdateName(context.Context, string, string, string) error {
	return nil
}
func (s *stubRepo) UpdateStatus(context.Context, string, string, string) error {
	return nil
}
func (s *stubRepo) UpdateProcessedResult(context.Context, string, string, string, string, int, int, int) error {
	return nil
}

type stubStorage struct {
	deletedKeys []string
}

func (s *stubStorage) Upload(context.Context, string, io.Reader, string) (string, error) {
	return "", nil
}
func (s *stubStorage) Delete(_ context.Context, key string) error {
	s.deletedKeys = append(s.deletedKeys, key)
	return nil
}
func (s *stubStorage) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (s *stubStorage) PublicBaseURL() string                       { return "https://cdn.example.com" }

func TestDeleteMedia_SoftDeleteDoesNotTouchStorage(t *testing.T) {
	processed := "https://cdn.example.com/processed/x"
	thumb := "https://cdn.example.com/thumb/x"
	repo := &stubRepo{
		mediaByID: map[string]*media.Media{
			"media-1": {
				ID:           "media-1",
				UserID:       "owner-user",
				OriginalURL:  "https://cdn.example.com/raw/x",
				ProcessedURL: &processed,
				ThumbnailURL: &thumb,
			},
		},
	}
	storage := &stubStorage{}
	svc := NewService(repo, storage)

	if err := svc.DeleteMedia(context.Background(), "media-1", "owner-user"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(storage.deletedKeys) != 0 {
		t.Fatalf("expected 0 storage deletes for soft delete, got %v", storage.deletedKeys)
	}
}

func TestHardDeleteMedia_DeletesStorageWhenOwner(t *testing.T) {
	processed := "https://cdn.example.com/processed/x"
	repo := &stubRepo{
		mediaByID: map[string]*media.Media{
			"media-1": {
				ID:           "media-1",
				UserID:       "owner-user",
				OriginalURL:  "https://cdn.example.com/raw/x",
				ProcessedURL: &processed,
			},
		},
	}
	storage := &stubStorage{}
	svc := NewService(repo, storage)

	if err := svc.HardDeleteMedia(context.Background(), "media-1", "owner-user"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(storage.deletedKeys) != 2 {
		t.Fatalf("expected 2 storage deletes, got %d: %v", len(storage.deletedKeys), storage.deletedKeys)
	}
}

func TestHardDeleteMedia_DoesNotDeleteStorageWhenNotOwner(t *testing.T) {
	processed := "https://cdn.example.com/processed/x"
	thumb := "https://cdn.example.com/thumb/x"
	repo := &stubRepo{
		mediaByID: map[string]*media.Media{
			"media-1": {
				ID:           "media-1",
				UserID:       "owner-user",
				OriginalURL:  "https://cdn.example.com/raw/x",
				ProcessedURL: &processed,
				ThumbnailURL: &thumb,
			},
		},
	}
	storage := &stubStorage{}
	svc := NewService(repo, storage)

	err := svc.HardDeleteMedia(context.Background(), "media-1", "attacker-user")
	if err == nil {
		t.Fatal("expected error when hard-deleting another user's media")
	}
	if len(storage.deletedKeys) != 0 {
		t.Fatalf("expected no storage deletes, got %v", storage.deletedKeys)
	}
}
