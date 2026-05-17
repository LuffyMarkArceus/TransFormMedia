package image

import (
	"testing"

	"universal-media-service/core/media"
)

func TestPickProcessSourceURL_UsesProcessedWhenWithinBounds(t *testing.T) {
	processed := "https://cdn.example.com/processed/x"
	m := &media.Media{
		OriginalURL:  "https://cdn.example.com/raw/x",
		ProcessedURL: &processed,
	}
	opts := ProcessOptions{MaxWidth: 800, MaxHeight: 600, Format: FormatJPEG, Quality: 85}
	if got := PickProcessSourceURL(m, opts); got != processed {
		t.Fatalf("expected processed URL, got %q", got)
	}
}

func TestPickProcessSourceURL_UsesOriginalWhenRequestLarger(t *testing.T) {
	processed := "https://cdn.example.com/processed/x"
	m := &media.Media{
		OriginalURL:  "https://cdn.example.com/raw/x",
		ProcessedURL: &processed,
	}
	opts := ProcessOptions{MaxWidth: 3000, MaxHeight: 2000, Format: FormatJPEG, Quality: 85}
	if got := PickProcessSourceURL(m, opts); got != m.OriginalURL {
		t.Fatalf("expected original URL, got %q", got)
	}
}
