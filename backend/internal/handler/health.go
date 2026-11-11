package handler

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/alerts"
	"github.com/tiofani03/artracker/backend/internal/analytics"
	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
	"github.com/tiofani03/artracker/backend/internal/ingest"
	"github.com/tiofani03/artracker/backend/internal/storage"
)

// Handler carries the dependencies every route needs.
type Handler struct {
	db           *pgxpool.Pool
	cfg          config.Config
	issuer       *auth.Issuer
	providers    map[string]OAuthProvider
	ingestSvc    *ingest.Service
	analyticsSvc *analytics.Service
	replayStore  storage.ReplayStore
	alertsSvc    *alerts.Service
	replaySwitch replaySwitchCache

	// in-memory artwork store, used only when the database is disabled (SKIP_DB)
	mu     sync.RWMutex
	nextID int64
	mem    []Artwork
}

func New(pool *pgxpool.Pool, cfg config.Config, issuer *auth.Issuer, providers map[string]OAuthProvider) *Handler {
	return &Handler{
		db:        pool,
		cfg:       cfg,
		issuer:    issuer,
		providers: providers,
		ingestSvc: ingest.NewService(ingest.Config{
			Workers:         cfg.IngestWorkers,
			QueueSize:       cfg.IngestQueueSize,
			BatchSize:       cfg.IngestBatchSize,
			FlushIntervalMs: cfg.IngestFlushIntervalMs,
			ProjectRPS:      cfg.IngestProjectRPS,
			ProjectEventsPS: cfg.IngestProjectEventsPerSec,
			KeyCacheTTLSec:  cfg.IngestKeyCacheTTLSec,
		}, pool),
		analyticsSvc: analytics.NewService(analytics.Config{
			Workers:         cfg.AnalyticsWorkers,
			QueueSize:       cfg.AnalyticsQueueSize,
			BatchSize:       cfg.AnalyticsBatchSize,
			FlushIntervalMs: cfg.AnalyticsFlushIntervalMs,
			ProjectRPS:      cfg.AnalyticsProjectRPS,
			ProjectEventsPS: cfg.AnalyticsProjectEventsPerSec,
		}, pool),
	}
}

// WithIngest replaces the ingest service (useful for custom sinks or test mocks).
func (h *Handler) WithIngest(svc *ingest.Service) *Handler {
	h.ingestSvc = svc
	return h
}

// WithAlerts sets the alerts service that issue ingestion reports to.
func (h *Handler) WithAlerts(s *alerts.Service) *Handler {
	h.alertsSvc = s
	return h
}

// WithAnalytics replaces the analytics service (useful for custom sinks or test mocks).
func (h *Handler) WithAnalytics(svc *analytics.Service) *Handler {
	h.analyticsSvc = svc
	return h
}

// WithReplayStore injects the object storage provider for session replays.
func (h *Handler) WithReplayStore(s storage.ReplayStore) *Handler {
	h.replayStore = s
	return h
}

// AnalyticsService returns the analytics service.
func (h *Handler) AnalyticsService() *analytics.Service {
	return h.analyticsSvc
}

// Health reports liveness plus database reachability and ingest/analytics buffer health.
func (h *Handler) Health(c *fiber.Ctx) error {
	var ingestStats any
	isOverloaded := false
	if h.ingestSvc != nil {
		stats := h.ingestSvc.WorkerPool().Stats()
		ingestStats = stats
		if stats.Status == "overloaded" {
			isOverloaded = true
		}
	}

	var analyticsStats any
	if h.analyticsSvc != nil {
		stats := h.analyticsSvc.WorkerPool().Stats()
		analyticsStats = stats
		if stats.UtilizationPct >= 95.0 {
			isOverloaded = true
		}
	}

	if h.db == nil {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":    "ok",
			"db":        "disabled",
			"ingest":    ingestStats,
			"analytics": analyticsStats,
		})
	}

	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "up"
	status := fiber.StatusOK
	if err := h.db.Ping(ctx); err != nil {
		dbStatus = "down"
		status = fiber.StatusServiceUnavailable
	} else if isOverloaded {
		status = fiber.StatusServiceUnavailable
	}

	return c.Status(status).JSON(fiber.Map{
		"status":    map[bool]string{true: "ok", false: "degraded"}[status == fiber.StatusOK],
		"db":        dbStatus,
		"ingest":    ingestStats,
		"analytics": analyticsStats,
	})
}
