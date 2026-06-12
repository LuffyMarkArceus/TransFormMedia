package http

import (
	"os"
	"strings"
	"time"
	"universal-media-service/internal/config"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func NewGinServer(cfg *config.Config) *gin.Engine {
	r := gin.Default()

	allowedOrigins := corsAllowedOrigins()
	r.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			for _, allowed := range allowedOrigins {
				if allowed == origin {
					return true
				}
			}
			if strings.HasSuffix(origin, ".vercel.app") {
				return true
			}
			return false
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	_ = cfg // reserved for future server-level config

	return r
}

func corsAllowedOrigins() []string {
	raw := os.Getenv("ALLOWED_ORIGINS")
	if raw == "" {
		return []string{"http://localhost:3000"}
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, o := range parts {
		o = strings.TrimSpace(o)
		if o != "" {
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return []string{"http://localhost:3000"}
	}
	return origins
}
