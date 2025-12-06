package main

import (
	"log"

	"universal-media-service/adapters/http"
	"universal-media-service/internal/config"
)

func main() {
	cfg := config.Load()

	server := http.NewGinServer(cfg)

	log.Println("🚀 Server starting on port:", cfg.ServerPort)
	server.Run(":" + cfg.ServerPort)
}
