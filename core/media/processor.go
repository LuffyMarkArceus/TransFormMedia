package media

import (
	"context"
	"mime/multipart"
)

type ProcessedResult struct {
	Width    int
	Height   int
	Duration int

	ProcessedBytes []byte
	ThumbnailBytes []byte

	ProcessedContentType string
	ThumbnailContentType string
}

// FileResult is the file-based counterpart of ProcessedResult: processors
// write outputs into a caller-owned work directory instead of returning whole
// files as byte slices, so multi-hundred-MB media can be processed without
// buffering it in memory. Paths are empty/absent only where noted; the
// caller is responsible for removing the work directory.
type FileResult struct {
	Width    int
	Height   int
	Duration int

	OutputPath           string // processed output, always set on success
	ThumbnailPath        string // "" when the processor produced no thumbnail
	OutputContentType    string
	ThumbnailContentType string
}

type MediaProcessor interface {
	Process(ctx context.Context, data []byte, contentType string) (*ProcessedResult, error)
	ProcessStream(ctx context.Context, file multipart.File, contentType string, size int64) (*ProcessedResult, error)
	SupportedTypes() []string
}

type ProcessingOptions struct {
	MaxWidth    int
	MaxHeight   int
	Quality     int
	Format      string
	MaxDuration int
}

func DefaultImageOptions() ProcessingOptions {
	return ProcessingOptions{
		MaxWidth:  1920,
		MaxHeight: 1080,
		Quality:   85,
		Format:    "jpeg",
	}
}

func DefaultThumbnailOptions() ProcessingOptions {
	return ProcessingOptions{
		MaxWidth:  320,
		MaxHeight: 180,
		Quality:   75,
		Format:    "jpeg",
	}
}

func DefaultVideoOptions() ProcessingOptions {
	return ProcessingOptions{
		MaxWidth:    1920,
		MaxHeight:   1080,
		Quality:     85,
		Format:      "mp4",
		MaxDuration: 300,
	}
}

func DefaultAudioOptions() ProcessingOptions {
	return ProcessingOptions{
		Format:      "mp3",
		MaxDuration: 600,
	}
}
