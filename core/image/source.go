package image

import "universal-media-service/core/media"

// DefaultUploadMaxWidth/Height match media.DefaultImageOptions (pre-generated processed asset bounds).
const (
	DefaultUploadMaxWidth  = 1920
	DefaultUploadMaxHeight = 1080
)

// ShouldUseProcessedSource returns true when on-the-fly transforms can use the
// already-processed upload (smaller, faster) instead of re-decoding the full original.
func ShouldUseProcessedSource(opts ProcessOptions) bool {
	return opts.MaxWidth <= DefaultUploadMaxWidth && opts.MaxHeight <= DefaultUploadMaxHeight
}

// PickProcessSourceURL chooses which stored asset to read for dynamic processing.
func PickProcessSourceURL(m *media.Media, opts ProcessOptions) string {
	if m.ProcessedURL != nil && *m.ProcessedURL != "" && ShouldUseProcessedSource(opts) {
		return *m.ProcessedURL
	}
	return m.OriginalURL
}
