package image

// ProcessedResult represents the output of image processing.
// This is intentionally storage-agnostic.
type ProcessedResult struct {
	Width  int
	Height int

	ProcessedBytes []byte
	ThumbnailBytes []byte

	ProcessedContentType string
	ThumbnailContentType string
}
