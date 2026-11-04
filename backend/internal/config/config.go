package config

import (
	"log/slog"
	"os"
	"strings"
)

// Config is loaded once at boot from the environment. Nothing here has a
// secret default: DATABASE_URL must always be provided.
type Config struct {
	Env         string
	Port        string
	DatabaseURL string
	CORSOrigins string
	LogLevel    slog.Level

	// Auth. JWTSecret has a development-only default; production must set it.
	JWTSecret          string
	PublicURL          string // frontend base URL, post-login redirect target
	GoogleClientID     string
	GoogleClientSecret string
}

// Placeholders shipped in .env.example / used as the dev default; never valid in production.
var knownDummySecrets = map[string]bool{
	"dummy-jwt-secret-change-me": true,
	"dev-only-secret-change-me":  true,
}

func Load() Config {
	cfg := Config{
		Env:         env("APP_ENV", "development"),
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://artracker:artracker@localhost:5433/artracker?sslmode=disable"),
		CORSOrigins: env("CORS_ORIGINS", "http://localhost:5173"),
		LogLevel:    parseLevel(env("LOG_LEVEL", "info")),

		JWTSecret:          env("JWT_SECRET", ""),
		PublicURL:          strings.TrimRight(env("PUBLIC_URL", "http://localhost:5173"), "/"),
		GoogleClientID:     env("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: env("GOOGLE_CLIENT_SECRET", ""),
	}
	if cfg.Env == "production" {
		if cfg.JWTSecret == "" {
			panic("JWT_SECRET must be set when APP_ENV=production")
		}
		if len(cfg.JWTSecret) < 32 || knownDummySecrets[cfg.JWTSecret] {
			panic("JWT_SECRET must be a random string of at least 32 characters (placeholder values are rejected)")
		}
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "dev-only-secret-change-me"
	}
	return cfg
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
