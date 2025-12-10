package api

import (
	"log"
	"universal-media-service/core/auth"
	"universal-media-service/core/image"
	"universal-media-service/internal/config"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, cfg *config.Config) {
	authService := auth.NewAuthService(cfg)
	imageService := image.NewImageService(cfg)

	v1 := r.Group("/api/v1")
	{
		// Auth - Open Rotues
		v1.POST("/register", authService.Register)
		v1.POST("/login", authService.Login)
		v1.GET("/hello", func(c *gin.Context) {
			log.Printf("SERVER IS ONLINE & OK")
			c.JSON(200, gin.H{
				"hello": "world",
			})
		})

		// Image - Auth enabled
		v1.POST("/images", auth.ClerkAuthMiddleware(), imageService.Upload)
		v1.GET("/images", imageService.List)
		v1.GET("/images/:id", auth.ClerkAuthMiddleware(), imageService.Get)
		v1.POST("/images/:id/transform", auth.ClerkAuthMiddleware(), imageService.Transform)

		// AUTH TEST ROUTE
		v1.GET("/auth-test", auth.ClerkAuthMiddleware(), func(c *gin.Context) {
			userID := c.GetString("userID")
			email := c.GetString("sub")

			log.Println(c)

			c.JSON(200, gin.H{
				"status":  "ok",
				"userID":  userID,
				"sub":     email,
				"message": "Clerk authentication works!",
			})
		})
	}
}
