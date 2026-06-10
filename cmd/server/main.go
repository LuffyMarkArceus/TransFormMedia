package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"universal-media-service/adapters/cache"
	adapterhttp "universal-media-service/adapters/http"
	"universal-media-service/adapters/neondb"
	"universal-media-service/adapters/r2"
	"universal-media-service/api"
	"universal-media-service/core/auth"
	"universal-media-service/core/media"
	"universal-media-service/core/upload"
	"universal-media-service/core/worker"
	"universal-media-service/internal/config"

	"github.com/joho/godotenv"
)

type cacheGetter interface {
	GetProcessed(ctx context.Context, mediaID string, width, height, quality int, format string) ([]byte, bool, error)
	SetProcessed(ctx context.Context, mediaID string, width, height, quality int, format string, data []byte) error
}

func main() {
	_ = godotenv.Load()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

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

	slog.Info("Starting Universal Media Service")

	auth.InitJWKS()

	r2Client, err := r2.NewClient(r2.Config{
		Bucket:     cfg.R2Bucket,
		AccessKey:  cfg.R2AccessKey,
		SecretKey:  cfg.R2SecretKey,
		AccountID:  cfg.R2AccountID,
		PublicBase: cfg.R2PublicBase,
	})
	if err != nil {
		slog.Error("Failed to create R2 client", "error", err)
		log.Fatal(err)
	}
	db := neondb.New()

	mediaRepo := media.NewPostgresRepository(db)
	uploadService := upload.NewService(mediaRepo, r2Client)

	var cacheClient cacheGetter
	if appCfg.RedisURL != "" {
		rc, err := cache.NewRedisCache(appCfg.RedisURL, time.Duration(appCfg.RedisTTL)*time.Second)
		if err != nil {
			slog.Warn("Redis unavailable, proceeding without cache", "error", err)
		} else {
			cacheClient = rc
		}
	}

	mediaHandler := adapterhttp.NewMediaUploadHandler(uploadService)
	listHandler := adapterhttp.NewMediaListHandler(mediaRepo, uploadService, cacheClient)

	shareHandler := adapterhttp.NewShareHandler(mediaRepo, appCfg.ShareSecret, appCfg.ShareTTL)

	router := adapterhttp.NewGinServer(appCfg)
	adapterhttp.RegisterHealthRoutes(router, db)
	api.RegisterRoutes(router, mediaHandler, listHandler, shareHandler)

	wpCtx, wpCancel := context.WithCancel(context.Background())
	wp := worker.New(mediaRepo, r2Client, uploadService.GetImageProcessor(), uploadService.GetVideoProcessor(), uploadService.GetAudioProcessor())
	go wp.Start(wpCtx)

	addr := ":" + cfg.ServerPort
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		slog.Info("Server started", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server error", "error", err)
			log.Fatal(err)
		}
	}()

	defer wpCancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown failed", "error", err)
		log.Fatalf("Server shutdown failed: %v", err)
	}
	slog.Info("Server stopped gracefully")
}
