package api

import (
	"universal-media-service/adapters/http"
	"universal-media-service/core/auth"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, imageHandler *http.ImageUploadHandler, imageListHandler *http.ImageListHandler) {
	v1 := r.Group("/api/v1")
	{
		v1.POST("/images", auth.ClerkAuthMiddleware(), imageHandler.Upload)
		v1.GET("/images", auth.ClerkAuthMiddleware(), imageListHandler.List)
		v1.DELETE("/images/:id", auth.ClerkAuthMiddleware(), imageHandler.Delete)

	}
}
