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

	mimeType := http.DetectContentType(buf)
	if !isSupportedMediaType(mimeType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file type"})
		return
	}

	m, err := h.service.UploadMedia(
		c.Request.Context(),
		userID,
		file,
		fileHeader.Filename,
		fileHeader.Header.Get("Content-Type"),
		fileHeader.Size,
	)
	if err != nil {
		log.Printf("Upload Error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, m)
}

func isSupportedMediaType(mimeType string) bool {
	supported := []string{
		"image/jpeg", "image/jpg", "image/png",
		"video/mp4", "video/quicktime", "video/x-msvideo", "video/webm", "video/x-matroska",
		"audio/mpeg", "audio/mp3", "audio/wav", "audio/x-wav", "audio/ogg", "audio/flac", "audio/aac", "audio/mp4",
	}
	for _, t := range supported {
		if mimeType == t {
			return true
		}
	}
	return false
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "media renamed successfully"})
}

func (h *MediaListHandler) ServeProcessed(c *gin.Context) {
	mediaID := c.Param("id")

	m, err := h.repo.GetByID(c.Request.Context(), mediaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
		return
	}

	if m.Type != "image" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dynamic processing only supported for images"})
		return
	}

	originalKey := extractKey(m.OriginalURL)

	originalBytes, err := h.service.Storage.Get(c.Request.Context(), originalKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch original media"})
		return
	}

	processOpts := image.ParseProcessOptions(c.Request.URL.Query())

	result, contentType, err := image.ProcessSingle(originalBytes, processOpts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("media processing failed: %v", err.Error())})
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
