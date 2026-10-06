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
		ShareSecret: required("SHARE_SECRET", 16),
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
