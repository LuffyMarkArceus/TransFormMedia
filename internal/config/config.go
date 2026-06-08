package config

import (
	"log"
	"os"
)

type Config struct {
	ServerPort  string
	ClerkIssuer string
	RedisURL    string
	RedisTTL    int // seconds
	ShareSecret string
	ShareTTL    int // seconds
}

func Load() *Config {
	return &Config{
		ServerPort:  env("SERVER_PORT", "8080"),
		ClerkIssuer: env("CLERK_ISSUER", ""),
		RedisURL:    os.Getenv("REDIS_URL"),
		RedisTTL:    3600,
		ShareSecret: os.Getenv("SHARE_SECRET"),
		ShareTTL:    604800, // 7 days default
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
