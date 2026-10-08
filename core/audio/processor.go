package audio

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

	"universal-media-service/core/media"
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
	return []string{"audio/mpeg", "audio/mp3", "audio/wav", "audio/x-wav", "audio/ogg", "audio/flac", "audio/aac", "audio/mp4"}
}

// ProcessFile converts an existing input file into an MP3 under the
// caller-owned workDir, returning its path. No whole-file buffering.
func (p *Processor) ProcessFile(ctx context.Context, inputPath, contentType, workDir string) (*media.FileResult, error) {
	outputPath := filepath.Join(workDir, "output.mp3")

	duration, err := p.extractDuration(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract duration: %w", err)
	}

	if err := p.convert(inputPath, outputPath, media.DefaultAudioOptions()); err != nil {
		return nil, fmt.Errorf("failed to convert: %w", err)
	}

	return &media.FileResult{
		Duration:             duration,
		OutputPath:           outputPath,
		OutputContentType:    "audio/mpeg",
		ThumbnailContentType: "",
	}, nil
}

func (p *Processor) Process(ctx context.Context, data []byte, contentType string) (*media.ProcessedResult, error) {
	tmpDir, err := os.MkdirTemp("", "audio-process")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input")
	if err := os.WriteFile(inputPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write input file: %w", err)
	}

	result, err := p.ProcessFile(ctx, inputPath, contentType, tmpDir)
	if err != nil {
		return nil, err
	}

	processedBytes, err := os.ReadFile(result.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read output file: %w", err)
	}

	return &media.ProcessedResult{
		Duration:             result.Duration,
		ProcessedBytes:       processedBytes,
		ProcessedContentType: result.OutputContentType,
	}, nil
}

func (p *Processor) ProcessStream(ctx context.Context, file multipart.File, contentType string, size int64) (*media.ProcessedResult, error) {
	data := new(bytes.Buffer)
	if _, err := data.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return p.Process(ctx, data.Bytes(), contentType)
}

func (p *Processor) extractDuration(inputPath string) (int, error) {
	cmd := exec.Command(p.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		inputPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return 0, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	if dur, err := strconv.ParseFloat(result.Format.Duration, 64); err == nil {
		return int(dur), nil
	}

	return 0, nil
}

func (p *Processor) convert(inputPath, outputPath string, opts media.ProcessingOptions) error {
	cmd := exec.Command(p.ffmpegPath,
		"-i", inputPath,
		"-codec:a", "libmp3lame",
		"-b:a", "192k",
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

var _ io.ReadCloser = (*os.File)(nil)
