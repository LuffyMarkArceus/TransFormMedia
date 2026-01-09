package api

import (
	"universal-media-service/adapters/http"
	"universal-media-service/core/auth"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, imageHandler *http.ImageHandler) {
	v1 := r.Group("/api/v1")
	{
		v1.POST("/images", auth.ClerkAuthMiddleware(), imageHandler.Upload)
	}
}
