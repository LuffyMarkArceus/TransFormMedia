package http

import (
	"net/http"
	"universal-media-service/core/image"

	"github.com/gin-gonic/gin"
)

type ImageHandler struct {
	Service *image.ImageService
}

func NewImageHandler(service *image.ImageService) *ImageHandler {
	return &ImageHandler{Service: service}
}

func (h *ImageHandler) Upload(c *gin.Context) {
	userID := c.GetString("userID")

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open file"})
		return
	}
	defer file.Close()

	img, err := h.Service.Upload(userID, file, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), fileHeader.Size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, img)
}
