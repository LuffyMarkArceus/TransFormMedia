package http

import (
	"errors"
	"net/http"

	"universal-media-service/core/image"
	"universal-media-service/core/media"

	"github.com/gin-gonic/gin"
)

// respondMediaError maps domain errors to HTTP responses. Returns true if handled.
func respondMediaError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, media.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
		return true
	}
	return false
}

// respondProcessingError maps image processing failures to 4xx where appropriate.
func respondProcessingError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, image.ErrDimensionsExceeded) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Image dimensions exceed the maximum of 4096×4096 pixels. Resize the file and try again.",
		})
		return true
	}
	return false
}
