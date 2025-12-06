package api

import (
	"github.com/gin-gonic/gin"
	"universal-media-service/core/auth"
	"universal-media-service/core/image"
	"universal-media-service/internal/config"
)

func RegisterRoutes(r *gin.Engine, cfg *config.Config) {
	authService := auth.NewAuthService(cfg)
	imageService := image.NewImageService(cfg)

	v1 := r.Group("/api/v1")
	{
		// Auth
		v1.POST("/register", authService.Register)
		v1.POST("/login", authService.Login)

		// Image
		v1.POST("/images", imageService.Upload)
		v1.GET("/images", imageService.List)
		v1.GET("/images/:id", imageService.Get)
		v1.POST("/images/:id/transform", imageService.Transform)
	}
}
