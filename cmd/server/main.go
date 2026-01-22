package main

import (
	"log"
	"os"

	"universal-media-service/adapters/http"
	"universal-media-service/adapters/neondb"
	"universal-media-service/adapters/r2"
	"universal-media-service/api"
	"universal-media-service/core/auth"
	"universal-media-service/core/media"
	"universal-media-service/core/upload"
	"universal-media-service/internal/config"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	cfg := struct {
		R2Bucket     string
		R2AccessKey  string
		R2SecretKey  string
		R2AccountID  string
		R2PublicBase string
		ServerPort   string
	}{
		R2Bucket:     os.Getenv("R2_BUCKET"),
		R2AccessKey:  os.Getenv("R2_ACCESS_KEY"),
		R2SecretKey:  os.Getenv("R2_SECRET_KEY"),
		R2AccountID:  os.Getenv("R2_ACCOUNT_ID"),
		R2PublicBase: os.Getenv("R2_PUBLIC_BASE_URL"),
		ServerPort:   os.Getenv("SERVER_PORT"),
	}

	auth.InitJWKS()

	r2Client, err := r2.NewClient(r2.Config{
		Bucket:     cfg.R2Bucket,
		AccessKey:  cfg.R2AccessKey,
		SecretKey:  cfg.R2SecretKey,
		AccountID:  cfg.R2AccountID,
		PublicBase: cfg.R2PublicBase,
	})
	if err != nil {
		log.Fatal(err)
	}
	db := neondb.New()

	mediaRepo := media.NewPostgresRepository(db)
	uploadService := upload.NewService(mediaRepo, r2Client)

	uploadHandler := http.NewImageUploadHandler(uploadService)
	listHandler := http.NewImageListHandler(mediaRepo, uploadService)

	router := http.NewGinServer(&config.Config{
		ServerPort: cfg.ServerPort,
	})
	api.RegisterRoutes(router, uploadHandler, listHandler)

	log.Println("🚀 Server running on port", cfg.ServerPort)
	if err := router.Run(":" + cfg.ServerPort); err != nil {
		log.Fatal(err)
	}
}
