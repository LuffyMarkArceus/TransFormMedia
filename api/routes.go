package api

import (
	"time"

	"universal-media-service/adapters/http"
	"universal-media-service/core/auth"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, mediaHandler *http.MediaUploadHandler, mediaListHandler *http.MediaListHandler, shareHandler *http.ShareHandler) {
	rateLimiter := auth.NewRateLimiter(100, time.Minute)

	v1 := r.Group("/api/v1")
	{
		v1.Use(rateLimiter.Middleware())
		v1.POST("/media", auth.ClerkAuthMiddleware(), mediaHandler.Upload)
		v1.PUT("/media/:id", auth.ClerkAuthMiddleware(), mediaHandler.Replace)
		v1.GET("/media", auth.ClerkAuthMiddleware(), mediaListHandler.List)
		v1.DELETE("/media/:id", auth.ClerkAuthMiddleware(), mediaHandler.Delete)
		v1.DELETE("/media/:id/permanent", auth.ClerkAuthMiddleware(), mediaHandler.PermanentDelete)
		v1.POST("/media/batch-delete", auth.ClerkAuthMiddleware(), mediaHandler.BatchDelete)
		v1.PATCH("/media/:id/rename", auth.ClerkAuthMiddleware(), mediaListHandler.Rename)
		v1.PATCH("/media/:id/restore", auth.ClerkAuthMiddleware(), mediaListHandler.Restore)

		v1.GET("/media/:id/process", auth.ClerkAuthMiddleware(), mediaListHandler.ServeProcessed)
		v1.GET("/media/:id/status", auth.ClerkAuthMiddleware(), mediaListHandler.Status)
		v1.GET("/media/:id/info", auth.ClerkAuthMiddleware(), mediaListHandler.Info)

		v1.POST("/media/:id/share", auth.ClerkAuthMiddleware(), shareHandler.Generate)
		v1.POST("/media/:id/reprocess", auth.ClerkAuthMiddleware(), mediaHandler.Reprocess)
		v1.GET("/share/:token", shareHandler.ServeShared)
	}
}
