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
	SkipDB      bool
	CORSOrigins string
	LogLevel    slog.Level
}

func Load() Config {
	rawDSN := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	cfg := Config{
		Env:         env("APP_ENV", "development"),
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://artracker:artracker@localhost:5433/artracker?sslmode=disable"),
		SkipDB:      parseBool(env("SKIP_DB", "false")) || strings.EqualFold(rawDSN, "none") || strings.EqualFold(rawDSN, "memory"),
		CORSOrigins: env("CORS_ORIGINS", "http://localhost:5173"),
		LogLevel:    parseLevel(env("LOG_LEVEL", "info")),
	}
	return cfg
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
