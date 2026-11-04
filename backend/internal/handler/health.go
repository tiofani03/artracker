package handler

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler carries the dependencies every route needs.
type Handler struct {
	db     *pgxpool.Pool
	mu     sync.RWMutex
	nextID int64
	mem    []Artwork
}

func New(pool *pgxpool.Pool) *Handler {
	return &Handler{db: pool}
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
