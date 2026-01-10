package http

import (
	"net/http"
	"universal-media-service/core/media"
	"universal-media-service/core/upload"

	"github.com/gin-gonic/gin"
)

type ImageUploadHandler struct {
	service *upload.Service
}

type ImageListHandler struct {
	repo media.Repository
}

func NewImageUploadHandler(service *upload.Service) *ImageUploadHandler {
	return &ImageUploadHandler{service: service}
}

func NewImageListHandler(repo media.Repository) *ImageListHandler {
	return &ImageListHandler{repo: repo}
}

func (h *ImageUploadHandler) Upload(c *gin.Context) {
	userID := c.GetString("userID")

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open file"})
		return
	}
	defer file.Close()

	img, err := h.service.UploadImage(
		c.Request.Context(),
		userID,
		file,
		fileHeader.Filename,
		fileHeader.Header.Get("Content-Type"),
		fileHeader.Size,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, img)
}

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
