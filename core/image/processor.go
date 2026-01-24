package image

import (
	"bytes"
	"fmt"
	"image"

	_ "image/jpeg"
	_ "image/png"

	"github.com/disintegration/imaging"
)

func Process(
	original []byte,
	opts ProcessOptions,
	thumbOpts ThumbnailOptions,
) (*ProcessedResult, error) {

	// ---- Decode with EXIF auto-orientation ----
	img, err := imaging.Decode(
		bytes.NewReader(original),
		imaging.AutoOrientation(true),
	)
	if err != nil {
		return nil, fmt.Errorf("decode image failed: %w", err)
	}

	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	// ---- Processed Image ----
	processed := resize(img, opts.MaxWidth, opts.MaxHeight)

	var processedBuf bytes.Buffer
	processedCT, err := encode(
		&processedBuf,
		processed,
		opts.Format,
		opts.Quality,
	)
	if err != nil {
		return nil, err
	}

	// ---- Thumbnail ----
	thumb := resize(img, thumbOpts.Width, thumbOpts.Height)

	var thumbBuf bytes.Buffer
	thumbCT, err := encode(
		&thumbBuf,
		thumb,
		FormatJPEG,
		thumbOpts.Quality,
	)
	if err != nil {
		return nil, err
	}

	return &ProcessedResult{
		Width:                width,
		Height:               height,
		ProcessedBytes:       processedBuf.Bytes(),
		ThumbnailBytes:       thumbBuf.Bytes(),
		ProcessedContentType: processedCT,
		ThumbnailContentType: thumbCT,
	}, nil
}

// ---- Helpers ----

func resize(img image.Image, maxW, maxH int) image.Image {
	if maxW == 0 && maxH == 0 {
		return img
	}
	return imaging.Fit(img, maxW, maxH, imaging.Lanczos)
}

func encode(
	buf *bytes.Buffer,
	img image.Image,
	format Format,
	quality int,
) (string, error) {

	switch format {
	case FormatJPEG:
		err := imaging.Encode(
			buf,
			img,
			imaging.JPEG,
			imaging.JPEGQuality(quality),
		)
		return "image/jpeg", err

	case FormatPNG:
		err := imaging.Encode(buf, img, imaging.PNG)
		return "image/png", err

	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

func ProcessSingle(
	original []byte,
	opts ProcessOptions,
) ([]byte, string, error) {
	// ---- Decode with EXIF auto-orientation ----
	img, err := imaging.Decode(
		bytes.NewReader(original),
		imaging.AutoOrientation(true),
	)
	if err != nil {
		return nil, "", fmt.Errorf("decode image failed: %w", err)
	}

	processed := resize(img, opts.MaxWidth, opts.MaxHeight)

	var processedBuf bytes.Buffer
	processedCT, err := encode(
		&processedBuf,
		processed,
		opts.Format,
		opts.Quality,
	)
	if err != nil {
		return nil, "", err
	}

	return processedBuf.Bytes(), processedCT, nil
}
