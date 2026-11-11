package config

import (
	"encoding/base64"
	"log/slog"
	"os"
	"regexp"
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
	IngestWorkers             int
	IngestQueueSize           int
	IngestBatchSize           int
	IngestFlushIntervalMs     int
	IngestProjectRPS          float64
	ReplayRateRPS             float64
	IngestProjectEventsPerSec float64
	IngestKeyCacheTTLSec      int

	// Analytics Pipeline configuration
	AnalyticsWorkers             int
	AnalyticsQueueSize           int
	AnalyticsBatchSize           int
	AnalyticsFlushIntervalMs     int
	AnalyticsProjectRPS          float64
	AnalyticsProjectEventsPerSec float64

	// Auth. JWTSecret has a development-only default; production must set it.
	JWTSecret          string
	PublicURL          string // frontend base URL, post-login redirect target
	GoogleClientID     string
	GoogleClientSecret string

	// Session Replay & S3 / Cloudflare R2 Object Storage
	R2AccountID         string
	R2AccessKeyID       string
	R2AccessKeySecret   string
	R2BucketName        string
	R2Endpoint          string
	R2Region            string
	ReplayMaxChunkBytes int64
	ReplayProxyUpload   bool
	// ReplayLocalDir stores replay chunks on disk when R2 is not configured (development only).
	ReplayLocalDir string

	// Alerts. TelegramBotToken is a secret: never log it. AlertsWorker gates
	// Telegram polling and alert evaluation; run it on exactly one instance.
	TelegramBotToken string
	TelegramAPIBase  string
	AlertsWorker     bool
	// AlertsBotMode is "tenant" (each organisation connects its own bot, the
	// default) or "shared" (one operator bot from TELEGRAM_BOT_TOKEN).
	AlertsBotMode string
	// AlertsMasterKey is the 32-byte key that encrypts per-tenant bot tokens;
	// nil when unset (allowed in development and in shared mode). Never log it.
	AlertsMasterKey []byte
	// AlertsMasterKeyID labels the key and is bound into the encryption
	// associated data, so it is restricted to [A-Za-z0-9._-]{1,32}.
	AlertsMasterKeyID string
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
		ReplayRateRPS:             envFloat("REPLAY_RATE_RPS", 100.0),
		IngestProjectEventsPerSec: envFloat("INGEST_PROJECT_EVENTS_PER_SEC", 1000.0),
		IngestKeyCacheTTLSec:      envInt("INGEST_KEY_CACHE_TTL_SEC", 60),

		AnalyticsWorkers:             envInt("ANALYTICS_WORKERS", 4),
		AnalyticsQueueSize:           envInt("ANALYTICS_QUEUE_SIZE", 2048),
		AnalyticsBatchSize:           envInt("ANALYTICS_BATCH_SIZE", 500),
		AnalyticsFlushIntervalMs:     envInt("ANALYTICS_FLUSH_INTERVAL_MS", 250),
		AnalyticsProjectRPS:          envFloat("ANALYTICS_PROJECT_RPS", 50.0),
		AnalyticsProjectEventsPerSec: envFloat("ANALYTICS_PROJECT_EVENTS_PER_SEC", 1000.0),

		JWTSecret:          env("JWT_SECRET", ""),
		PublicURL:          strings.TrimRight(env("PUBLIC_URL", "http://localhost:5173"), "/"),
		GoogleClientID:     env("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: env("GOOGLE_CLIENT_SECRET", ""),

		R2AccountID:         env("R2_ACCOUNT_ID", ""),
		R2AccessKeyID:       env("R2_ACCESS_KEY_ID", ""),
		R2AccessKeySecret:   env("R2_ACCESS_KEY_SECRET", ""),
		R2BucketName:        env("R2_BUCKET_NAME", ""),
		R2Endpoint:          env("R2_ENDPOINT", ""),
		R2Region:            env("R2_REGION", "auto"),
		ReplayMaxChunkBytes: int64(envInt("REPLAY_MAX_CHUNK_BYTES", 5242880)),
		ReplayProxyUpload:   parseBool(env("REPLAY_PROXY_UPLOAD", "true")),
		ReplayLocalDir:      env("REPLAY_LOCAL_DIR", ""),

		TelegramBotToken: loadTelegramToken(),
		TelegramAPIBase:  env("TELEGRAM_API_BASE", "https://api.telegram.org"),
	}
	rawWorker := env("ALERTS_WORKER", "")
	var workerRecognised bool
	cfg.AlertsWorker, workerRecognised = parseAlertsWorker(rawWorker)
	if !workerRecognised {
		slog.Warn("unrecognised ALERTS_WORKER value, keeping the alerts worker enabled", "value_length", len(rawWorker))
	}

	cfg.AlertsBotMode = loadAlertsBotMode()
	cfg.AlertsMasterKey = loadAlertsMasterKey()
	cfg.AlertsMasterKeyID = env("ALERTS_MASTER_KEY_ID", "v1")
	if !masterKeyIDPattern.MatchString(cfg.AlertsMasterKeyID) {
		panic("ALERTS_MASTER_KEY_ID must match [A-Za-z0-9._-]{1,32}")
	}

	if cfg.R2Endpoint == "" && cfg.R2AccountID != "" {
		cfg.R2Endpoint = "https://" + cfg.R2AccountID + ".r2.cloudflarestorage.com"
	}
	if cfg.Env == "production" {
		if cfg.JWTSecret == "" {
			panic("JWT_SECRET must be set when APP_ENV=production")
		}
		if len(cfg.JWTSecret) < 32 || knownDummySecrets[cfg.JWTSecret] {
			panic("JWT_SECRET must be a random string of at least 32 characters (placeholder values are rejected)")
		}
	}
	// After the JWT checks so operators see that diagnostic first.
	if cfg.Env == "production" && cfg.AlertsBotMode == "tenant" && cfg.AlertsMasterKey == nil {
		panic("ALERTS_MASTER_KEY (or ALERTS_MASTER_KEY_FILE) must be set when APP_ENV=production and ALERTS_BOT_MODE=tenant; generate one with: openssl rand -base64 32")
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

// loadTelegramToken returns the bot token from TELEGRAM_BOT_TOKEN or, for
// secret stores that mount files (Docker/Kubernetes secrets), from the file named
// by TELEGRAM_BOT_TOKEN_FILE. Setting both, or pointing at a file that cannot be
// read or is empty, panics at boot: an operator who configured a secret must not
// silently end up with Telegram off. Panic messages never contain the token.
func loadTelegramToken() string {
	direct := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	path := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN_FILE"))
	if path == "" {
		return direct
	}
	if direct != "" {
		panic("set only one of TELEGRAM_BOT_TOKEN and TELEGRAM_BOT_TOKEN_FILE")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		panic("TELEGRAM_BOT_TOKEN_FILE cannot be read: " + err.Error())
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		panic("TELEGRAM_BOT_TOKEN_FILE is empty: " + path)
	}
	return token
}

var masterKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// loadAlertsBotMode reads ALERTS_BOT_MODE: unset means "tenant"; only "tenant"
// and "shared" are valid.
func loadAlertsBotMode() string {
	switch mode := strings.ToLower(strings.TrimSpace(os.Getenv("ALERTS_BOT_MODE"))); mode {
	case "":
		return "tenant"
	case "tenant", "shared":
		return mode
	default:
		panic("ALERTS_BOT_MODE must be \"tenant\" or \"shared\"")
	}
}

// loadAlertsMasterKey returns the 32-byte master key from ALERTS_MASTER_KEY
// (standard base64) or from the file named by ALERTS_MASTER_KEY_FILE, or nil
// when neither is set. A key that is set but unusable always panics, in every
// environment; whether an unset key is acceptable is decided by Load. Panic
// messages never contain key material.
func loadAlertsMasterKey() []byte {
	direct := strings.TrimSpace(os.Getenv("ALERTS_MASTER_KEY"))
	path := strings.TrimSpace(os.Getenv("ALERTS_MASTER_KEY_FILE"))
	name := "ALERTS_MASTER_KEY"
	encoded := direct
	if path != "" {
		if direct != "" {
			panic("set only one of ALERTS_MASTER_KEY and ALERTS_MASTER_KEY_FILE")
		}
		name = "ALERTS_MASTER_KEY_FILE"
		raw, err := os.ReadFile(path)
		if err != nil {
			panic("ALERTS_MASTER_KEY_FILE cannot be read: " + err.Error())
		}
		encoded = strings.TrimSpace(string(raw))
		if encoded == "" {
			panic("ALERTS_MASTER_KEY_FILE is empty: " + path)
		}
	}
	if encoded == "" {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		panic(name + " is not valid standard base64")
	}
	if len(key) != 32 {
		panic(name + " must decode to exactly 32 bytes (generate one with: openssl rand -base64 32)")
	}
	return key
}

// parseAlertsWorker reads ALERTS_WORKER. Unset or empty means enabled; any
// value other than the usual true/false spellings also keeps the worker
// enabled, with recognised false so a typo cannot silently stop alerts.
func parseAlertsWorker(raw string) (enabled bool, recognised bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return true, false
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
