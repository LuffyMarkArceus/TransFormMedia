package http

import (
	"fmt"
	"log"
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

type RenameImageRequest struct {
	Name string `json:"name"`
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
		log.Printf("%s", fmt.Sprintf("Upload Error :%v", err))
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
