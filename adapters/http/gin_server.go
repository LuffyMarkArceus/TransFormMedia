package http

import (
	"time"
	"universal-media-service/core/auth"
	"universal-media-service/internal/config"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func NewGinServer(cfg *config.Config) *gin.Engine {
	r := gin.Default()

	// Initialize Clerk JWKS
	auth.InitJWKS()

	// Enable CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"}, // or "*" for all origins,      // http://localhost:3000
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	return r
}
