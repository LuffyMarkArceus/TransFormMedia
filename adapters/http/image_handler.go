package http

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
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
	cache   cacheGetter
}

type cacheGetter interface {
	GetProcessed(ctx context.Context, mediaID string, width, height, quality int, format string) ([]byte, bool, error)
	SetProcessed(ctx context.Context, mediaID string, width, height, quality int, format string, data []byte) error
}

type RenameMediaRequest struct {
	Name string `json:"name"`
}

type BatchDeleteRequest struct {
	IDs []string `json:"ids"`
}

func NewMediaUploadHandler(service *upload.Service) *MediaUploadHandler {
	return &MediaUploadHandler{service: service}
}

func NewMediaListHandler(repo media.Repository, service *upload.Service, cache cacheGetter) *MediaListHandler {
	return &MediaListHandler{
		repo:    repo,
		service: service,
		cache:   cache,
	}
}

func (h *MediaUploadHandler) Replace(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mediaID := c.Param("id")

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

	m, err := h.service.ReplaceMedia(
		c.Request.Context(),
		mediaID,
		userID,
		file,
		fileHeader.Filename,
		mimeType,
		fileHeader.Size,
	)
	if err != nil {
		log.Printf("Replace Error: %v", err)
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to replace media"})
		return
	}

	c.JSON(http.StatusOK, m)
}

func (h *MediaUploadHandler) Reprocess(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mediaID := c.Param("id")

	m, err := h.service.ReprocessMedia(c.Request.Context(), mediaID, userID)
	if err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reprocess media"})
		return
	}

	c.JSON(http.StatusOK, m)
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

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	sortBy := c.DefaultQuery("sortBy", "created_at")
	sortDir := c.DefaultQuery("sortDir", "desc")
	search := c.Query("search")
	mediaType := c.Query("type")
	status := c.DefaultQuery("status", "")

	params := media.ListParams{
		UserID:  userID,
		Type:    mediaType,
		Search:  search,
		Status:  status,
		SortBy:  sortBy,
		SortDir: sortDir,
		Limit:   limit,
		Offset:  offset,
	}

	result, err := h.repo.ListPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	page := 1
	if params.Limit > 0 {
		page = (params.Offset / params.Limit) + 1
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  result.Data,
		"total": result.Total,
		"page":  page,
		"limit": params.Limit,
	})
}

func (h *MediaUploadHandler) BatchDelete(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	var failed []string
	for _, id := range req.IDs {
		if err := h.service.DeleteMedia(c.Request.Context(), id, userID); err != nil {
			log.Printf("BatchDelete: failed to delete %s: %v", id, err)
			failed = append(failed, id)
		}
	}

	if len(failed) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"message":    "partial delete completed",
			"deleted":    len(req.IDs) - len(failed),
			"failed":     len(failed),
			"failed_ids": failed,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "all media deleted successfully"})
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

	c.JSON(http.StatusOK, gin.H{"message": "media moved to trash"})
}

func (h *MediaUploadHandler) PermanentDelete(c *gin.Context) {
	userID := c.GetString("userID")
	mediaID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if err := h.service.HardDeleteMedia(c.Request.Context(), mediaID, userID); err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to permanently delete media"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "media permanently deleted"})
}

func (h *MediaListHandler) Restore(c *gin.Context) {
	userID := c.GetString("userID")
	mediaID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if err := h.service.RestoreMedia(c.Request.Context(), mediaID, userID); err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to restore media"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "media restored from trash"})
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

func (h *MediaListHandler) Status(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	m, err := h.repo.GetByIDForUser(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load media"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":     m.ID,
		"status": m.Status,
	})
}

func (h *MediaListHandler) Info(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	m, err := h.repo.GetByIDForUser(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		if respondMediaError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load media"})
		return
	}

	info := gin.H{
		"id":           m.ID,
		"name":         m.Name,
		"type":         m.Type,
		"format":       m.Format,
		"sizeBytes":    m.SizeBytes,
		"width":        m.Width,
		"height":       m.Height,
		"duration":     m.Duration,
		"status":       m.Status,
		"createdAt":    m.CreatedAt,
		"hasProcessed": m.ProcessedURL != nil && *m.ProcessedURL != "",
		"hasThumbnail": m.ThumbnailURL != nil && *m.ThumbnailURL != "",
	}

	c.JSON(http.StatusOK, info)
}

func (h *MediaListHandler) ServeProcessed(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mediaID := c.Param("id")
	ctx := c.Request.Context()

	m, err := h.repo.GetByIDForUser(ctx, mediaID, userID)
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

	if h.cache != nil {
		if cached, ok, err := h.cache.GetProcessed(ctx, mediaID, processOpts.MaxWidth, processOpts.MaxHeight, processOpts.Quality, string(processOpts.Format)); err == nil && ok {
			contentType := "image/" + string(processOpts.Format)
			c.Header("Content-Type", contentType)
			c.Header("Content-Disposition", "inline")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Header("X-Cache", "HIT")
			c.Data(http.StatusOK, contentType, cached)
			return
		}
	}

	sourceURL := image.PickProcessSourceURL(m, processOpts)
	sourceKey := extractKey(sourceURL)

	sourceBytes, err := h.service.Storage.Get(ctx, sourceKey)
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

	if h.cache != nil {
		if err := h.cache.SetProcessed(ctx, mediaID, processOpts.MaxWidth, processOpts.MaxHeight, processOpts.Quality, string(processOpts.Format), result); err != nil {
			log.Printf("Warning: failed to cache processed %s: %v", mediaID, err)
		} else {
			log.Printf("Cached processed result for %s", mediaID)
		}
	}

	log.Printf("Successfully processed %s of size %d", mediaID, len(result))

	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Cache", "MISS")

	c.Data(http.StatusOK, contentType, result)
}

func extractKey(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return strings.TrimPrefix(publicURL, "/")
	}
	return strings.TrimPrefix(u.Path, "/")
}
