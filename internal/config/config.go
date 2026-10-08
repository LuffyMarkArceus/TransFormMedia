package config

import (
	"log"
	"os"
	"strconv"
)

type Config struct {
	ServerPort          string
	ClerkIssuer         string
	RedisURL            string
	RedisTTL            int // seconds
	ShareSecret         string
	ShareTTL            int // seconds
	WorkerRetryBaseSecs int // seconds; base backoff between processing attempts
	WorkerMaxAttempts   int // attempts before an item is marked failed

	// MaxUploadBytes caps one non-image file (also the multipart body cap's
	// per-file side). Cloud Run's ~32 MB request limit makes anything much
	// larger only reachable through the presigned direct-upload flow.
	MaxUploadBytes int
	// MaxImageBytes caps one image. Images are decoded synchronously, so
	// this stays small.
	MaxImageBytes int
	// StorageQuotaBytes caps the total stored bytes per user.
	StorageQuotaBytes int
}

func Load() *Config {
	return &Config{
		ServerPort:          env("SERVER_PORT", "8080"),
		ClerkIssuer:         env("CLERK_ISSUER", ""),
		RedisURL:            os.Getenv("REDIS_URL"),
		RedisTTL:            3600,
		ShareSecret:         required("SHARE_SECRET", 16),
		ShareTTL:            604800, // 7 days default
		WorkerRetryBaseSecs: envInt("WORKER_RETRY_BASE_SECONDS", 30),
		WorkerMaxAttempts:   envInt("WORKER_MAX_ATTEMPTS", 5),
		MaxUploadBytes:      envInt("MAX_UPLOAD_BYTES", 500*1024*1024),
		MaxImageBytes:       envInt("MAX_IMAGE_BYTES", 32*1024*1024),
		StorageQuotaBytes:   envInt("USER_STORAGE_QUOTA_BYTES", 10*1024*1024*1024),
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		if fallback == "" {
			log.Fatalf("Missing required environment variable: %s", key)
		}
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		log.Fatalf("Invalid value for %s: %q (expected a positive integer)", key, value)
	}
	return parsed
}

// required terminates startup when the variable is missing or too short.
// SHARE_SECRET in particular must never fall back to "": HMAC signing with an
// empty key is computable by an attacker, who could then forge share tokens
// for any media ID.
func required(key string, minLen int) string {
	value := os.Getenv(key)
	if len(value) < minLen {
		log.Fatalf(
			"Missing or too-short required environment variable: %s (need >= %d chars). Generate one with: openssl rand -hex 32",
			key, minLen,
		)
	}
	return value
}
