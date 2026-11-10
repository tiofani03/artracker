package handler

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ScreenChunkFormat is the chunk format of screenshot-mode mobile replay.
const ScreenChunkFormat = "artracker-screen-v1"

// replayMaxFPS is the frame-rate ceiling the SDKs are told to respect in screenshot mode.
const replayMaxFPS = 10

const replaySwitchTTL = 30 * time.Second

type switchEntry struct {
	enabled bool
	expires time.Time
}

// replaySwitchCache keeps each project's screenshot switch in memory for replaySwitchTTL.
// The zero value is ready to use.
type replaySwitchCache struct {
	mu sync.Mutex
	m  map[int64]switchEntry
}

func (c *replaySwitchCache) get(projectID int64) (enabled, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.m[projectID]
	if !found || time.Now().After(e.expires) {
		return false, false
	}
	return e.enabled, true
}

func (c *replaySwitchCache) put(projectID int64, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[int64]switchEntry)
	}
	c.m[projectID] = switchEntry{enabled: enabled, expires: time.Now().Add(replaySwitchTTL)}
}

func (c *replaySwitchCache) invalidate(projectID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, projectID)
}

// replayScreenshotEnabled reports the project's screenshot switch. Without a database it is always on.
// A database error is returned, never read as "enabled": screenshots must not slip through a failed check.
func (h *Handler) replayScreenshotEnabled(ctx context.Context, projectID int64) (bool, error) {
	if enabled, ok := h.replaySwitch.get(projectID); ok {
		return enabled, nil
	}
	if h.db == nil {
		return true, nil
	}
	var enabled bool
	if err := h.db.QueryRow(ctx, `SELECT replay_screenshot_enabled FROM projects WHERE id = $1`, projectID).Scan(&enabled); err != nil {
		return false, err
	}
	h.replaySwitch.put(projectID, enabled)
	return enabled, nil
}

type ReplayConfigResponse struct {
	ScreenshotEnabled bool `json:"screenshot_enabled"`
	MaxFPS            int  `json:"max_fps"`
}

// GetReplayConfig handles GET /api/v1/replay/config (ingest key auth).
func (h *Handler) GetReplayConfig(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	enabled, err := h.replayScreenshotEnabled(ctx, projectID)
	if err != nil {
		return err
	}
	return c.JSON(ReplayConfigResponse{ScreenshotEnabled: enabled, MaxFPS: replayMaxFPS})
}
