package image

import (
	"errors"
	"fmt"
)

var (
	// ErrDimensionsExceeded is returned when pixel dimensions exceed MaxAllowedWidth/Height.
	ErrDimensionsExceeded = errors.New("image dimensions exceed allowed maximum")
)

func errDimensionsExceeded(w, h int) error {
	return fmt.Errorf("%w: %dx%d exceed %dx%d", ErrDimensionsExceeded, w, h, MaxAllowedWidth, MaxAllowedHeight)
}
