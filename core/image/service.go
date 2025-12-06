package image

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"universal-media-service/internal/config"
)

type ImageService struct {
	cfg *config.Config
}

func NewImageService(cfg *config.Config) *ImageService {
	return &ImageService{cfg: cfg}
}

func (s *ImageService) Upload(c *gin.Context) {
	c.JSON(http.StatusCreated, gin.H{"message": "image uploaded"})
}

func (s *ImageService) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "list images"})
}

func (s *ImageService) Get(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "image details"})
}

func (s *ImageService) Transform(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "transform image"})
}
