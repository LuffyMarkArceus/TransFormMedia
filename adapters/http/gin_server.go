package http

import (
	"github.com/gin-gonic/gin"
	"universal-media-service/api"
	"universal-media-service/internal/config"
)

func NewGinServer(cfg *config.Config) *gin.Engine {
	r := gin.Default()

	api.RegisterRoutes(r, cfg)

	return r
}
