package api

import (
	"time"

	"universal-media-service/adapters/http"
	"universal-media-service/core/auth"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, mediaHandler *http.MediaUploadHandler, mediaListHandler *http.MediaListHandler, shareHandler *http.ShareHandler, eventsHandler *http.EventsHandler) {
	// Coarse pre-auth flood guard, keyed by client IP. Its job is only to
	// slow down anonymous floods; it is not spoof-proof behind a proxy.
	floodLimiter := auth.NewRateLimiter(300, time.Minute)
	// Authoritative limit, keyed by the verified JWT subject (falls back to
	// IP only if no user is attached). Unspoofable and NAT-safe.
	userLimiter := auth.NewRateLimiter(100, time.Minute)

	authMW := auth.ClerkAuthMiddleware()
	userLimitMW := userLimiter.UserMiddleware()

	// protected composes auth → per-user rate limit → handler as one route
	// entry, so ordering stays visible at every registration site.
	protected := func(h ...gin.HandlerFunc) []gin.HandlerFunc {
		return append([]gin.HandlerFunc{authMW, userLimitMW}, h...)
	}

	v1 := r.Group("/api/v1")
	{
		v1.Use(floodLimiter.IPMiddleware())

		v1.POST("/media", protected(mediaHandler.Upload)...)
		v1.PUT("/media/:id", protected(mediaHandler.Replace)...)
		v1.GET("/media", protected(mediaListHandler.List)...)
		v1.DELETE("/media/:id", protected(mediaHandler.Delete)...)
		v1.DELETE("/media/:id/permanent", protected(mediaHandler.PermanentDelete)...)
		v1.POST("/media/batch-delete", protected(mediaHandler.BatchDelete)...)
		v1.PATCH("/media/:id/rename", protected(mediaListHandler.Rename)...)
		v1.PATCH("/media/:id/restore", protected(mediaListHandler.Restore)...)

		v1.GET("/media/:id/process", protected(mediaListHandler.ServeProcessed)...)
		v1.GET("/media/:id/status", protected(mediaListHandler.Status)...)
		v1.GET("/media/:id/info", protected(mediaListHandler.Info)...)

		v1.POST("/media/:id/share", protected(shareHandler.Generate)...)
		v1.POST("/media/:id/reprocess", protected(mediaHandler.Reprocess)...)

		// Public share redirect: anonymous, so only the IP flood guard applies.
		v1.GET("/share/:token", shareHandler.ServeShared)

		// SSE status stream. Authorize is protected (Clerk) and trades the
		// session for a short-lived random token that EventSource can present
		// via query string; the stream endpoint itself is unauthenticated so
		// a browser can open it without setting headers. Only registered when
		// Redis (the event bus) is available; clients fall back to polling.
		if eventsHandler != nil {
			v1.POST("/events/authorize", protected(eventsHandler.Authorize)...)
			v1.GET("/events/stream", eventsHandler.Stream)
		}
	}
}
