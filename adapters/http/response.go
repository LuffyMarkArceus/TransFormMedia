package http

import (
	"errors"
	"fmt"
	"net/http"

	"universal-media-service/core/image"
	"universal-media-service/core/media"
	"universal-media-service/core/upload"

	"github.com/gin-gonic/gin"
)

const (
	// maxUploadBody bounds multipart bodies: 500 MB of file plus envelope
	// overhead. Applied while reading, not after, so oversized uploads are
	// cut off instead of spooled to temp files.
	maxUploadBody = 512 * 1024 * 1024
	// maxJSONBody bounds JSON request bodies (rename, batch delete).
	maxJSONBody = 1 << 20
	// maxBatchDelete caps how many IDs one batch-delete request may carry.
	maxBatchDelete = 500
)

// respondMediaError maps domain errors to HTTP responses. Returns true if handled.
func respondMediaError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	// ErrTrashed maps to the same 404 as ErrNotFound so callers cannot
	// distinguish "deleted" from "never existed" (IDOR hygiene).
	if errors.Is(err, media.ErrNotFound) || errors.Is(err, media.ErrTrashed) {
		c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
		return true
	}
	switch {
	case errors.Is(err, upload.ErrUnsupportedType):
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file type"})
	case errors.Is(err, upload.ErrInvalidRequest):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	case errors.Is(err, upload.ErrFileTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
	case errors.Is(err, upload.ErrQuotaExceeded):
		c.JSON(http.StatusConflict, gin.H{"error": "storage quota exceeded"})
	case errors.Is(err, upload.ErrUploadNotStarted):
		c.JSON(http.StatusConflict, gin.H{"error": "uploaded file not found in storage"})
	case errors.Is(err, upload.ErrSizeMismatch):
		c.JSON(http.StatusBadRequest, gin.H{"error": "uploaded file is larger than declared"})
	case errors.Is(err, upload.ErrContentMismatch):
		c.JSON(http.StatusBadRequest, gin.H{"error": "uploaded file content does not match the declared type"})
	default:
		return false
	}
	return true
}

// fileTooLarge writes the standard 413 for a file that exceeds cap bytes.
func fileTooLarge(c *gin.Context, cap int64) {
	c.JSON(http.StatusRequestEntityTooLarge, gin.H{
		"error": fmt.Sprintf("file size exceeds the %d MB limit", cap/(1024*1024)),
	})
}

// requireVisible stops the handler with a 404 when media has been moved to
// the trash: trashed content must not be readable, served, or shareable.
func requireVisible(c *gin.Context, m *media.Media) bool {
	if m.Status == "trashed" {
		c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
		return false
	}
	return true
}

// isBodyTooLarge reports whether err came from http.MaxBytesReader.
func isBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// bindJSON reads a size-capped JSON body, mapping overflows to 413 instead of
// the generic 400.
func bindJSON(c *gin.Context, obj any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBody)
	if err := c.ShouldBindJSON(obj); err != nil {
		if isBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		}
		return false
	}
	return true
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
