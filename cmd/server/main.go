package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	adapterhttp "universal-media-service/adapters/http"
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
	appCfg := config.Load()

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
		ServerPort:   appCfg.ServerPort,
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

	mediaHandler := adapterhttp.NewMediaUploadHandler(uploadService)
	listHandler := adapterhttp.NewMediaListHandler(mediaRepo, uploadService)

	router := adapterhttp.NewGinServer(appCfg)
	adapterhttp.RegisterHealthRoutes(router, db)
	api.RegisterRoutes(router, mediaHandler, listHandler)

	addr := ":" + cfg.ServerPort
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		log.Println("🚀 Server running on port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
	log.Println("Server stopped gracefully")
}
