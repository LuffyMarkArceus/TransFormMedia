package image

type Format string

const (
	FormatJPEG Format = "jpeg"
	FormatPNG  Format = "png"
	FormatWebP Format = "webp" // future
)

const (
	MaxAllowedWidth  = 4096
	MaxAllowedHeight = 4096

	MinAllowedQuality = 1
	MaxAllowedQuality = 100
)

type Gravity string

const (
	GravityCenter      Gravity = "center"
	GravityTop         Gravity = "top"
	GravityBottom      Gravity = "bottom"
	GravityLeft        Gravity = "left"
	GravityRight       Gravity = "right"
	GravityTopLeft     Gravity = "topleft"
	GravityTopRight    Gravity = "topright"
	GravityBottomLeft  Gravity = "bottomleft"
	GravityBottomRight Gravity = "bottomright"
	DefaultGravity     Gravity = "center"
)

type ProcessOptions struct {
	// Resize
	MaxWidth  int
	MaxHeight int

	// Crop (set both to non-zero to enable)
	CropWidth  int
	CropHeight int
	Gravity    Gravity

	// Output
	Format  Format
	Quality int // JPEG/WebP quality (1–100)

	// Effects
	Blur      float64 // Gaussian blur sigma (0 = disabled)
	Grayscale bool    // convert to grayscale
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
