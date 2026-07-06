package config

import (
	"log"
	"os"
	"time"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	RedisAddr     string
	RedisPassword string

	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	EncryptionKey string

	SourceDir string
	RepoSlug  string
}

func Load() Config {
	cfg := Config{
		HTTPAddr:        getEnv("PANEL_HTTP_ADDR", ":8080"),
		DatabaseURL:     getEnv("PANEL_DATABASE_URL", "postgres://panel:panel@localhost:5432/panel?sslmode=disable"),
		RedisAddr:       getEnv("PANEL_REDIS_ADDR", "localhost:6379"),
		RedisPassword:   getEnv("PANEL_REDIS_PASSWORD", ""),
		JWTSecret:       os.Getenv("PANEL_JWT_SECRET"),
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
		EncryptionKey:   os.Getenv("PANEL_ENCRYPTION_KEY"),
		SourceDir:       getEnv("PANEL_SOURCE_DIR", ""),
		RepoSlug:        getEnv("PANEL_UPDATE_REPO", "superbodik/sbPanel"),
	}

	if cfg.JWTSecret == "" || cfg.JWTSecret == "change-me-in-production" {
		log.Fatal("PANEL_JWT_SECRET is not set to a real secret — refusing to start with a known-insecure default. The installer generates one automatically in panel.env; set it manually if running the binary directly.")
	}
	if cfg.EncryptionKey == "" {
		log.Fatal("PANEL_ENCRYPTION_KEY is not set — refusing to start with a blank encryption key. The installer generates one automatically in panel.env; set it manually if running the binary directly.")
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
