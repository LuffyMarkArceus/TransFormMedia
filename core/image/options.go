package image

type Format string

const (
	FormatJPEG Format = "jpeg"
	FormatPNG  Format = "png"
	FormatWebP Format = "webp" // future
)

type ProcessOptions struct {
	// Resize
	MaxWidth  int
	MaxHeight int

	// Output
	Format  Format
	Quality int // JPEG/WebP quality (1–100)
}

func DefaultOptions() ProcessOptions {
	return ProcessOptions{
		MaxWidth:  1920,
		MaxHeight: 1080,
		Format:    FormatJPEG,
		Quality:   85,
	}
}

type ThumbnailOptions struct {
	// Quality is for JPEG/WebP quality (1–100)
	Width   int
	Height  int
	Quality int
}

func DefaultThumbnailOptions() ThumbnailOptions {
	return ThumbnailOptions{
		Width:   320,
		Height:  180,
		Quality: 75,
	}
}
