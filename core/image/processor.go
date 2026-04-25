package image

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"mime/multipart"

	_ "image/jpeg"
	_ "image/png"

	"universal-media-service/core/media"

	"github.com/disintegration/imaging"
)

type Processor struct{}

func NewProcessor() *Processor {
	return &Processor{}
}

func (p *Processor) SupportedTypes() []string {
	return []string{"image/jpeg", "image/jpg", "image/png"}
}

func (p *Processor) Process(ctx context.Context, data []byte, contentType string) (*media.ProcessedResult, error) {
	result, err := p.process(data, media.DefaultImageOptions(), media.DefaultThumbnailOptions())
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (p *Processor) ProcessStream(ctx context.Context, file multipart.File, contentType string, size int64) (*media.ProcessedResult, error) {
	data := new(bytes.Buffer)
	if _, err := data.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return p.Process(ctx, data.Bytes(), contentType)
}

func (p *Processor) process(
	original []byte,
	opts media.ProcessingOptions,
	thumbOpts media.ProcessingOptions,
) (*media.ProcessedResult, error) {

	img, err := imaging.Decode(
		bytes.NewReader(original),
		imaging.AutoOrientation(true),
	)
	if err != nil {
		return nil, fmt.Errorf("decode image failed: %w", err)
	}

	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	processed := resize(img, opts.MaxWidth, opts.MaxHeight)

	var processedBuf bytes.Buffer
	processedCT, err := encode(
		&processedBuf,
		processed,
		Format(opts.Format),
		opts.Quality,
	)
	if err != nil {
		return nil, err
	}

	thumb := resize(img, thumbOpts.MaxWidth, thumbOpts.MaxHeight)

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

	return &media.ProcessedResult{
		Width:                width,
		Height:               height,
		ProcessedBytes:       processedBuf.Bytes(),
		ThumbnailBytes:       thumbBuf.Bytes(),
		ProcessedContentType: processedCT,
		ThumbnailContentType: thumbCT,
	}, nil
}

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

func Process(
	original []byte,
	opts ProcessOptions,
	thumbOpts ThumbnailOptions,
) (*ProcessedResult, error) {

	img, err := imaging.Decode(
		bytes.NewReader(original),
		imaging.AutoOrientation(true),
	)
	if err != nil {
		return nil, fmt.Errorf("decode image failed: %w", err)
	}

	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

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

func ProcessSingle(
	original []byte,
	opts ProcessOptions,
) ([]byte, string, error) {

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
