package api

import (
	"universal-media-service/adapters/http"
	"universal-media-service/core/auth"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, mediaHandler *http.MediaUploadHandler, mediaListHandler *http.MediaListHandler) {
	v1 := r.Group("/api/v1")
	{
		v1.POST("/media", auth.ClerkAuthMiddleware(), mediaHandler.Upload)
		v1.GET("/media", auth.ClerkAuthMiddleware(), mediaListHandler.List)
		v1.DELETE("/media/:id", auth.ClerkAuthMiddleware(), mediaHandler.Delete)
		v1.PATCH("/media/:id/rename", auth.ClerkAuthMiddleware(), mediaListHandler.Rename)

		v1.GET("/media/:id/process", mediaListHandler.ServeProcessed)
	}
}
