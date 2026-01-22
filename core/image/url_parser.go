package image

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParseProcessOptions parses query params into ProcessOptions.
// Defaults are returned if param is missing or invalid.
func ParseProcessOptions(values url.Values) ProcessOptions {
	opts := DefaultOptions()

	if w := values.Get("w"); w != "" {
		if v, err := strconv.Atoi(w); err == nil && v > 0 {
			opts.MaxWidth = v
		}
	}

	if h := values.Get("h"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v > 0 {
			opts.MaxHeight = v
		}
	}

	if f := values.Get("format"); f != "" {
		switch strings.ToLower(f) {
		case "jpeg", "jpg":
			opts.Format = FormatJPEG
		case "png":
			opts.Format = FormatPNG
		case "webp":
			opts.Format = FormatWebP
		}
	}

	if q := values.Get("q"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 100 {
			opts.Quality = v
		}
	}

	return opts
}

// ParseThumbnailOptions parses query params into ThumbnailOptions.
func ParseThumbnailOptions(values url.Values) ThumbnailOptions {
	opts := DefaultThumbnailOptions()

	if w := values.Get("tw"); w != "" {
		if v, err := strconv.Atoi(w); err == nil && v > 0 {
			opts.Width = v
		}
	}

	if h := values.Get("th"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v > 0 {
			opts.Height = v
		}
	}

	if q := values.Get("tq"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 100 {
			opts.Quality = v
		}
	}

	return opts
}

// Convenience: parse full URL string (optional)
func ParseURL(urlStr string) (ProcessOptions, ThumbnailOptions, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return DefaultOptions(), DefaultThumbnailOptions(), fmt.Errorf("invalid url: %w", err)
	}

	values := u.Query()
	return ParseProcessOptions(values), ParseThumbnailOptions(values), nil
}
