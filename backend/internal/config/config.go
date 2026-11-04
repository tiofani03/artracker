package config

import (
	"log/slog"
	"os"
	"strconv"
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
	DBMaxConns  int

	// Ingest Pipeline configuration
	IngestWorkers          int
	IngestQueueSize        int
	IngestBatchSize        int
	IngestFlushIntervalMs  int
	IngestProjectRPS       float64
	IngestProjectEventsPerSec float64
	IngestKeyCacheTTLSec   int

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
	rawDSN := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	cfg := Config{
		Env:         env("APP_ENV", "development"),
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://artracker:artracker@localhost:5433/artracker?sslmode=disable"),
		SkipDB:      parseBool(env("SKIP_DB", "false")) || strings.EqualFold(rawDSN, "none") || strings.EqualFold(rawDSN, "memory"),
		CORSOrigins: env("CORS_ORIGINS", "http://localhost:5173"),
		LogLevel:    parseLevel(env("LOG_LEVEL", "info")),
		DBMaxConns:  envInt("DB_MAX_CONNS", 25),

		IngestWorkers:             envInt("INGEST_WORKERS", 4),
		IngestQueueSize:           envInt("INGEST_QUEUE_SIZE", 2048),
		IngestBatchSize:           envInt("INGEST_BATCH_SIZE", 500),
		IngestFlushIntervalMs:     envInt("INGEST_FLUSH_INTERVAL_MS", 250),
		IngestProjectRPS:          envFloat("INGEST_PROJECT_RPS", 50.0),
		IngestProjectEventsPerSec: envFloat("INGEST_PROJECT_EVENTS_PER_SEC", 1000.0),
		IngestKeyCacheTTLSec:      envInt("INGEST_KEY_CACHE_TTL_SEC", 60),

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

func envInt(key string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
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
