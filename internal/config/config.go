package config

import (
	"log"
	"os"
)

type Config struct {
	ServerPort  string
	ClerkIssuer string
}

func Load() *Config {
	return &Config{
		ServerPort:  env("SERVER_PORT", "8080"),
		ClerkIssuer: env("CLERK_ISSUER", ""),
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
