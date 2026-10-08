package upload

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"universal-media-service/core/media"
)

// directRepo stores rows so begin/complete can round-trip.
type directRepo struct {
	stubRepo
	mediaByID map[string]*media.Media
	sumSize   int64
}

func newDirectRepo() *directRepo {
	return &directRepo{mediaByID: map[string]*media.Media{}}
}

func (r *directRepo) Create(_ context.Context, m *media.Media) error {
	cp := *m
	r.mediaByID[m.ID] = &cp
	return nil
}
func (r *directRepo) GetByIDForUser(_ context.Context, id, userID string) (*media.Media, error) {
	m, ok := r.mediaByID[id]
	if !ok || m.UserID != userID {
		return nil, media.ErrNotFound
	}
	cp := *m
	return &cp, nil
}
func (r *directRepo) UpdateContent(_ context.Context, m *media.Media) error {
	cp := *m
	r.mediaByID[m.ID] = &cp
	return nil
}
func (r *directRepo) DeleteByID(_ context.Context, id, _ string) error {
	delete(r.mediaByID, id)
	return nil
}
func (r *directRepo) SumSizeByUser(context.Context, string) (int64, error) {
	return r.sumSize, nil
}

// directStorage keeps simple byte objects and records presigns/deletes.
type directStorage struct {
	objects      map[string][]byte
	presignedKey string
	presignedCT  string
	deletedKeys  []string
}

func newDirectStorage() *directStorage {
	return &directStorage{objects: map[string][]byte{}}
}

func (s *directStorage) Upload(context.Context, string, io.Reader, string) (string, error) {
	return "", nil
}
func (s *directStorage) Delete(_ context.Context, key string) error {
	s.deletedKeys = append(s.deletedKeys, key)
	delete(s.objects, key)
	return nil
}
func (s *directStorage) Get(_ context.Context, key string) ([]byte, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	return data, nil
}
func (s *directStorage) DownloadTo(_ context.Context, key string, dst io.Writer) (int64, error) {
	data, ok := s.objects[key]
	if !ok {
		return 0, ErrObjectNotFound
	}
	n, err := dst.Write(data)
	return int64(n), err
}
func (s *directStorage) Head(_ context.Context, key string) (ObjectInfo, error) {
	data, ok := s.objects[key]
	if !ok {
		return ObjectInfo{}, ErrObjectNotFound
	}
	return ObjectInfo{Size: int64(len(data))}, nil
}
func (s *directStorage) GetRange(_ context.Context, key string, offset, length int64) ([]byte, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	if offset >= int64(len(data)) {
		return nil, nil
	}
	end := offset + length
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return data[offset:end], nil
}
func (s *directStorage) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	s.presignedKey = key
	s.presignedCT = contentType
	return "https://cdn.example.com/" + key + "?X-Amz-Signature=test", nil
}
func (s *directStorage) PublicBaseURL() string { return "https://cdn.example.com" }

func newDirectFixture() (*directRepo, *directStorage, *Service) {
	repo := newDirectRepo()
	st := newDirectStorage()
	svc := NewService(repo, st, WithLimits(Limits{
		MaxUploadBytes:    100 * 1024 * 1024,
		MaxImageBytes:     32 * 1024 * 1024,
		StorageQuotaBytes: 100 * 1024 * 1024,
	}))
	return repo, st, svc
}

// jpegBytes is a minimal sniffable JPEG header.
func jpegBytes(n int) []byte {
	if n < 4 {
		n = 4
	}
	b := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	for len(b) < n {
		b = append(b, 0x00)
	}
	return b
}

func TestBeginDirectUpload_RejectsUnsupportedType(t *testing.T) {
	_, _, svc := newDirectFixture()
	_, _, err := svc.BeginDirectUpload(context.Background(), "user-1", BeginUploadRequest{
		Name: "notes.txt", ContentType: "text/plain", Size: 10,
	})
	if err != ErrUnsupportedType {
		t.Fatalf("expected ErrUnsupportedType, got %v", err)
	}
}

func TestBeginDirectUpload_RejectsOversizeImage(t *testing.T) {
	_, _, svc := newDirectFixture()
	_, _, err := svc.BeginDirectUpload(context.Background(), "user-1", BeginUploadRequest{
		Name: "huge.jpg", ContentType: "image/jpeg", Size: 33 * 1024 * 1024,
	})
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
	if !strings.Contains(err.Error(), "max") {
		t.Fatalf("error must name the limit, got %v", err)
	}
}

func TestBeginDirectUpload_RejectsOverQuota(t *testing.T) {
	repo, _, svc := newDirectFixture()
	repo.sumSize = 99 * 1024 * 1024
	_, _, err := svc.BeginDirectUpload(context.Background(), "user-1", BeginUploadRequest{
		Name: "clip.mp4", ContentType: "video/mp4", Size: 10 * 1024 * 1024,
	})
	if err != ErrQuotaExceeded {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}

func TestBeginDirectUpload_CreatesPendingRowAndPresigns(t *testing.T) {
	repo, st, svc := newDirectFixture()
	id, uploadURL, err := svc.BeginDirectUpload(context.Background(), "user-1", BeginUploadRequest{
		Name: "../evil/camera roll.jpg", ContentType: "image/jpeg", Size: 1024,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uploadURL == "" {
		t.Fatal("expected a presigned URL")
	}
	if st.presignedCT != "image/jpeg" {
		t.Fatalf("presign must bind the declared content type, got %q", st.presignedCT)
	}
	wantKey := "raw/image/user-1/" + id
	if st.presignedKey != wantKey {
		t.Fatalf("presign key = %q, want %q", st.presignedKey, wantKey)
	}

	m := repo.mediaByID[id]
	if m == nil {
		t.Fatal("expected a stored row")
	}
	if m.Status != "pending" {
		t.Fatalf("status = %q, want pending", m.Status)
	}
	if m.Name != "camera roll.jpg" {
		t.Fatalf("name must be sanitized, got %q", m.Name)
	}
	if m.OriginalURL != "https://cdn.example.com/"+wantKey {
		t.Fatalf("OriginalURL = %q", m.OriginalURL)
	}
}

func TestCompleteDirectUpload_NotStarted(t *testing.T) {
	repo, st, svc := newDirectFixture()
	id, _, err := svc.BeginDirectUpload(context.Background(), "user-1", BeginUploadRequest{
		Name: "a.jpg", ContentType: "image/jpeg", Size: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st // object was never PUT

	_, err = svc.CompleteDirectUpload(context.Background(), "user-1", id)
	if err != ErrUploadNotStarted {
		t.Fatalf("expected ErrUploadNotStarted, got %v", err)
	}
	if repo.mediaByID[id].Status != "pending" {
		t.Fatalf("row must stay pending, got %q", repo.mediaByID[id].Status)
	}
}

func TestCompleteDirectUpload_SizeMismatchDeletesObject(t *testing.T) {
	repo, st, svc := newDirectFixture()
	id, _, err := svc.BeginDirectUpload(context.Background(), "user-1", BeginUploadRequest{
		Name: "a.jpg", ContentType: "image/jpeg", Size: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := st.presignedKey
	st.objects[key] = jpegBytes(200) // larger than declared 100

	_, err = svc.CompleteDirectUpload(context.Background(), "user-1", id)
	if err != ErrSizeMismatch {
		t.Fatalf("expected ErrSizeMismatch, got %v", err)
	}
	if _, ok := st.objects[key]; ok {
		t.Fatal("oversized object must be deleted")
	}
	if len(st.deletedKeys) != 1 {
		t.Fatalf("expected exactly one delete, got %v", st.deletedKeys)
	}
	if repo.mediaByID[id].Status != "pending" {
		t.Fatalf("row must stay pending, got %q", repo.mediaByID[id].Status)
	}
}

func TestCompleteDirectUpload_ContentMismatchDeletesObject(t *testing.T) {
	repo, st, svc := newDirectFixture()
	id, _, err := svc.BeginDirectUpload(context.Background(), "user-1", BeginUploadRequest{
		Name: "a.mp4", ContentType: "video/mp4", Size: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := st.presignedKey
	st.objects[key] = jpegBytes(64) // image bytes under a video declaration

	_, err = svc.CompleteDirectUpload(context.Background(), "user-1", id)
	if err != ErrContentMismatch {
		t.Fatalf("expected ErrContentMismatch, got %v", err)
	}
	if _, ok := st.objects[key]; ok {
		t.Fatal("mismatched object must be deleted")
	}
	if repo.mediaByID[id].Status != "pending" {
		t.Fatalf("row must stay pending, got %q", repo.mediaByID[id].Status)
	}
}

func TestCompleteDirectUpload_SuccessFlipsToUploaded(t *testing.T) {
	_, st, svc := newDirectFixture()
	id, _, err := svc.BeginDirectUpload(context.Background(), "user-1", BeginUploadRequest{
		Name: "a.jpg", ContentType: "image/jpeg", Size: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	st.objects[st.presignedKey] = jpegBytes(64)

	m, err := svc.CompleteDirectUpload(context.Background(), "user-1", id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Status != "uploaded" {
		t.Fatalf("status = %q, want uploaded", m.Status)
	}
	if m.SizeBytes != 64 {
		t.Fatalf("SizeBytes must reflect the actual object size, got %d", m.SizeBytes)
	}
	if len(st.deletedKeys) != 0 {
		t.Fatalf("nothing must be deleted on success, got %v", st.deletedKeys)
	}

	// Idempotent: a second complete is a no-op that still returns the row.
	again, err := svc.CompleteDirectUpload(context.Background(), "user-1", id)
	if err != nil {
		t.Fatalf("repeat complete: %v", err)
	}
	if again.Status != "uploaded" {
		t.Fatalf("repeat complete status = %q", again.Status)
	}
}
