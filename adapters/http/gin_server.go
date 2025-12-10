package http

import (
	"universal-media-service/api"
	"universal-media-service/internal/config"

	"github.com/gin-gonic/gin"
)

func NewGinServer(cfg *config.Config) *gin.Engine {
	r := gin.Default()

	api.RegisterRoutes(r, cfg)

	return r
}
