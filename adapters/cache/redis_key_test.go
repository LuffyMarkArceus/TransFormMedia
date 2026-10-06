package cache

import (
	"testing"

	"universal-media-service/core/image"
)

// The cache key regression the content-versioning fix exists for: after a
// media record is replaced (updated_at bumps), previously cached transformed
// bytes of the OLD content must never be served. Versioning the key by the
// content version guarantees a replace produces a different key even when the
// transform options and media ID are identical.
func TestProcessCacheKey_EmitsVersionChangesKey(t *testing.T) {
	opts := image.ProcessOptions{MaxWidth: 100, Format: image.FormatJPEG}

	oldKey := processCacheKey("media-1", "1", opts)
	newKey := processCacheKey("media-1", "2", opts)
	if oldKey == newKey {
		t.Errorf("replace with same ID and same opts must change the cache key, got %q for both versions", oldKey)
	}
}

func TestProcessCacheKey_SameInputsAreStable(t *testing.T) {
	opts := image.ProcessOptions{MaxWidth: 100, Format: image.FormatJPEG}
	a := processCacheKey("media-1", "5", opts)
	b := processCacheKey("media-1", "5", opts)
	if a != b {
		t.Errorf("identical inputs must map to one key, got %q and %q", a, b)
	}
}

func TestProcessCacheKey_DifferentOptsNeverShareKey(t *testing.T) {
	w := image.ProcessOptions{MaxWidth: 100, Format: image.FormatJPEG}
	crop := image.ProcessOptions{MaxWidth: 100, CropWidth: 50, CropHeight: 50, Format: image.FormatJPEG}
	if processCacheKey("media-1", "5", w) == processCacheKey("media-1", "5", crop) {
		t.Error("differing transform options must produce distinct keys")
	}
}

func TestProcessCacheKey_DifferentMediaNeverShareKey(t *testing.T) {
	opts := image.ProcessOptions{MaxWidth: 100, Format: image.FormatJPEG}
	if processCacheKey("media-1", "5", opts) == processCacheKey("media-2", "5", opts) {
		t.Error("differing media IDs must produce distinct keys")
	}
}
