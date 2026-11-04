package handler

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
)

// Handler carries the dependencies every route needs.
type Handler struct {
	db        *pgxpool.Pool
	cfg       config.Config
	issuer    *auth.Issuer
	providers map[string]OAuthProvider

	// in-memory artwork store, used only when the database is disabled (SKIP_DB)
	mu     sync.RWMutex
	nextID int64
	mem    []Artwork
}

func New(pool *pgxpool.Pool, cfg config.Config, issuer *auth.Issuer, providers map[string]OAuthProvider) *Handler {
	return &Handler{db: pool, cfg: cfg, issuer: issuer, providers: providers}
}

// Health reports liveness plus database reachability.
func (h *Handler) Health(c *fiber.Ctx) error {
	if h.db == nil {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status": "ok",
			"db":     "disabled",
		})
	}

	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "up"
	status := fiber.StatusOK
	if err := h.db.Ping(ctx); err != nil {
		dbStatus = "down"
		status = fiber.StatusServiceUnavailable
	}

	return c.Status(status).JSON(fiber.Map{
		"status": map[bool]string{true: "ok", false: "degraded"}[status == fiber.StatusOK],
		"db":     dbStatus,
	})
}
