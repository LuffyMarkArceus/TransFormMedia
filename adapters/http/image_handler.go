package http

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"universal-media-service/core/image"
	"universal-media-service/core/media"
	"universal-media-service/core/upload"

	"github.com/gin-gonic/gin"
)

type MediaUploadHandler struct {
	service *upload.Service
}

type MediaListHandler struct {
	repo    media.Repository
	service *upload.Service
}

type RenameMediaRequest struct {
	Name string `json:"name"`
}

func NewMediaUploadHandler(service *upload.Service) *MediaUploadHandler {
	return &MediaUploadHandler{service: service}
}

func NewMediaListHandler(repo media.Repository, service *upload.Service) *MediaListHandler {
	return &MediaListHandler{
		repo:    repo,
		service: service,
	}
}

func (h *MediaUploadHandler) Upload(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}

	const maxFileSize = 500 * 1024 * 1024
	if fileHeader.Size > maxFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("file size exceeds %d MB limit", maxFileSize/(1024*1024))})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open file"})
		return
	}
	defer file.Close()

	buf := make([]byte, 512)
	if _, err := file.Read(buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot read file"})
		return
	}
	file.Seek(0, 0)

	mimeType := normalizeContentType(http.DetectContentType(buf))
	if !isSupportedMediaType(mimeType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file type"})
		return
	}

	m, err := h.service.UploadMedia(
		c.Request.Context(),
		userID,
		file,
		fileHeader.Filename,
		mimeType,
		fileHeader.Size,
	)
	if err != nil {
		log.Printf("Upload Error: %v", err)
		if respondProcessingError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload media"})
		return
	}

	c.JSON(http.StatusOK, m)
}

func (h *MediaListHandler) List(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mediaType := c.Query("type")

	var items []media.Media
	var err error

	if mediaType != "" {
		items, err = h.repo.ListByUserAndType(c.Request.Context(), userID, mediaType)
	} else {
		items, err = h.repo.ListByUser(c.Request.Context(), userID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, items)
}

func (h *MediaUploadHandler) Delete(c *gin.Context) {
	userID := c.GetString("userID")
	mediaID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if err := h.service.DeleteMedia(c.Request.Context(), mediaID, userID); err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete media"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "media deleted successfully"})
}

func (h *MediaListHandler) Rename(c *gin.Context) {
	userID := c.GetString("userID")
	mediaID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req RenameMediaRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if err := h.repo.UpdateName(c.Request.Context(), mediaID, userID, req.Name); err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rename media"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "media renamed successfully"})
}

func (h *MediaListHandler) ServeProcessed(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mediaID := c.Param("id")

	m, err := h.repo.GetByIDForUser(c.Request.Context(), mediaID, userID)
	if err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load media"})
		return
	}

	if m.Type != "image" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dynamic processing only supported for images"})
		return
	}

	processOpts := image.ParseProcessOptions(c.Request.URL.Query())
	sourceURL := image.PickProcessSourceURL(m, processOpts)
	sourceKey := extractKey(sourceURL)

	sourceBytes, err := h.service.Storage.Get(c.Request.Context(), sourceKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch media from storage"})
		return
	}

	result, contentType, err := image.ProcessSingle(sourceBytes, processOpts)
	if err != nil {
		if respondProcessingError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "media processing failed"})
		return
	}

	log.Printf("Successfully processed %s of size %d", mediaID, len(result))

	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=31536000, immutable")

	c.Data(http.StatusOK, contentType, result)
}

func extractKey(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return strings.TrimPrefix(publicURL, "/")
	}
	return strings.TrimPrefix(u.Path, "/")
}
