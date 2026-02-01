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

// -------------------- Handlers Types --------------------

type ImageUploadHandler struct {
	service *upload.Service
}

type ImageListHandler struct {
	repo    media.Repository
	service *upload.Service
}

type RenameImageRequest struct {
	Name string `json:"name"`
}

// -------------------- Constructors --------------------

func NewImageUploadHandler(service *upload.Service) *ImageUploadHandler {
	return &ImageUploadHandler{service: service}
}

func NewImageListHandler(repo media.Repository, service *upload.Service) *ImageListHandler {
	return &ImageListHandler{
		repo:    repo,
		service: service,
	}
}

// -------------------- Upload --------------------

func (h *ImageUploadHandler) Upload(c *gin.Context) {
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

	// ----------- Size Validation ----------
	const maxFileSize = 50 * 1024 * 1024 // 20 MB
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
	file.Seek(0, 0) // Reset read pointer

	mimeType := http.DetectContentType(buf)
	switch mimeType {
	case "image/jpeg", "image/jpg":
		// JPEG image
	case "image/png":
		// PNG image
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file type"})
		return
	}

	img, err := h.service.UploadImage(
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

	c.JSON(http.StatusOK, img)
}

// -------------------- List --------------------

func (h *ImageListHandler) List(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	images, err := h.repo.ListByUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, images)
}

// -------------------- Delete --------------------

func (h *ImageUploadHandler) Delete(c *gin.Context) {
	userID := c.GetString("userID")
	imageID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if err := h.service.DeleteImage(c.Request.Context(), imageID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "image deleted successfully"})
}

// -------------------- Rename --------------------

func (h *ImageListHandler) Rename(c *gin.Context) {
	userID := c.GetString("userID")
	imageID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req RenameImageRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if err := h.repo.UpdateName(c.Request.Context(), imageID, userID, req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "image renamed successfully"})
}

// -------------------- Dynamic Image Processing --------------------

func (h *ImageListHandler) ServeProcessed(c *gin.Context) {
	imageID := c.Param("id")

	// 1. Fetch image metadata
	img, err := h.repo.GetByID(c.Request.Context(), imageID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
		return
	}

	// 2. Extract R2 key from original URL
	originalKey := extractKey(img.OriginalURL)

	// 3. Download original image bytes
	originalBytes, err := h.service.Storage.Get(c.Request.Context(), originalKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch original image"})
		return
	}

	// 4. Parse processing options from URL
	processOpts := image.ParseProcessOptions(c.Request.URL.Query())

	// if processOpts.MaxHeight == 0 && processOpts.MaxWidth == 0 {
	// 	// No processing requested; serve original
	// 	return c.Redirect(http.StatusFound, img.OriginalURL)
	// }

	// 5. Process image dynamically
	result, contentType, err := image.ProcessSingle(
		originalBytes,
		processOpts,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("image processing failed: %v", err.Error())})
		return
	}

	log.Printf("Successfully processed %s of size %d", imageID, len(result))

	// Set caching headers & content type
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=31536000, immutable") // Cache for 1 year

	// 6. Return processed image
	c.Data(
		http.StatusOK,
		contentType,
		result,
	)
}

// -------------------- Utils --------------------

func extractKey(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return strings.TrimPrefix(publicURL, "/")
	}
	return strings.TrimPrefix(u.Path, "/")
}
