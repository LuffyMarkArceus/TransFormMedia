package video

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"universal-media-service/core/media"

	"github.com/google/uuid"
)

type Processor struct {
	ffmpegPath  string
	ffprobePath string
}

func NewProcessor() *Processor {
	return &Processor{
		ffmpegPath:  "ffmpeg",
		ffprobePath: "ffprobe",
	}
}

func (p *Processor) SupportedTypes() []string {
	return []string{"video/mp4", "video/quicktime", "video/x-msvideo", "video/webm", "video/x-matroska"}
}

func (p *Processor) Process(ctx context.Context, data []byte, contentType string) (*media.ProcessedResult, error) {
	tmpDir, err := os.MkdirTemp("", "video-process")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input")
	outputPath := filepath.Join(tmpDir, "output.mp4")
	thumbnailPath := filepath.Join(tmpDir, "thumb.jpg")

	if err := os.WriteFile(inputPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write input file: %w", err)
	}

	metadata, err := p.extractMetadata(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	if err := p.generateThumbnail(inputPath, thumbnailPath); err != nil {
		return nil, fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	if err := p.copyVideo(inputPath, outputPath); err != nil {
		return nil, fmt.Errorf("failed to copy video: %w", err)
	}

	processedBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read output file: %w", err)
	}

	thumbnailBytes, err := os.ReadFile(thumbnailPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read thumbnail: %w", err)
	}

	return &media.ProcessedResult{
		Width:                metadata.Width,
		Height:               metadata.Height,
		Duration:             metadata.Duration,
		ProcessedBytes:       processedBytes,
		ThumbnailBytes:       thumbnailBytes,
		ProcessedContentType: "video/mp4",
		ThumbnailContentType: "image/jpeg",
	}, nil
}

func (p *Processor) ProcessStream(ctx context.Context, file multipart.File, contentType string, size int64) (*media.ProcessedResult, error) {
	data := new(bytes.Buffer)
	if _, err := data.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return p.Process(ctx, data.Bytes(), contentType)
}

type videoMetadata struct {
	Width    int
	Height   int
	Duration int
}

func (p *Processor) extractMetadata(inputPath string) (*videoMetadata, error) {
	cmd := exec.Command(p.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	var width, height int
	var duration int

	for _, stream := range result.Streams {
		if stream.CodecType == "video" {
			width = stream.Width
			height = stream.Height
			break
		}
	}

	if dur, err := strconv.ParseFloat(result.Format.Duration, 64); err == nil {
		duration = int(dur)
	}

	return &videoMetadata{
		Width:    width,
		Height:   height,
		Duration: duration,
	}, nil
}

func (p *Processor) generateThumbnail(inputPath, outputPath string) error {
	cmd := exec.Command(p.ffmpegPath,
		"-i", inputPath,
		"-ss", "00:00:01",
		"-vframes", "1",
		"-vf", "scale=320:180:force_original_aspect_ratio=decrease,pad=320:180:(ow-iw)/2:(oh-ih)/2",
		"-y",
		outputPath,
	)

	return cmd.Run()
}

func (p *Processor) copyVideo(inputPath, outputPath string) error {
	cmd := exec.Command(p.ffmpegPath,
		"-i", inputPath,
		"-c", "copy",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg error: %w, %s", err, stderr.String())
	}

	return nil
}

func (p *Processor) transcode(inputPath, outputPath string, opts media.ProcessingOptions) error {
	cmd := exec.Command(p.ffmpegPath,
		"-i", inputPath,
		"-vf", fmt.Sprintf("scale='min(%d,iw)':min'(%d,ih)':force_original_aspect_ratio=decrease", opts.MaxWidth, opts.MaxHeight),
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg error: %w, %s", err, stderr.String())
	}

	return nil
}

func extractKey(publicURL string) string {
	if idx := strings.LastIndex(publicURL, "/raw/"); idx != -1 {
		return strings.TrimPrefix(publicURL[idx:], "/")
	}
	return publicURL
}

var _ io.ReadCloser = (*os.File)(nil)
var _ = uuid.NewString
