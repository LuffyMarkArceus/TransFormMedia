package config

import (
	"log"
	"os"
)

type Config struct {
	ServerPort string

	DatabaseURL string
	RedisURL    string

	KafkaBrokers string
	KafkaTopic   string

	R2AccessKey string
	R2SecretKey string
	R2Bucket    string

	ClerkApiKey   string
	ClerkFrontend string
}

func Load() *Config {
	cfg := &Config{
		ServerPort:   env("SERVER_PORT", "8080"),
		DatabaseURL:  env("DATABASE_URL", "1"),
		RedisURL:     env("REDIS_URL", "2"),
		KafkaBrokers: env("KAFKA_BROKERS", "3"),
		KafkaTopic:   env("KAFKA_TOPIC", "media-jobs"),
		R2AccessKey:  env("R2_ACCESS_KEY", "4"),
		R2SecretKey:  env("R2_SECRET_KEY", "5"),
		R2Bucket:     env("R2_BUCKET", "6"),
	}

	return cfg
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		if fallback == "" {
			log.Fatalf("missing required environment variable: %s", key)
		}
		return fallback
	}
	return value
}
