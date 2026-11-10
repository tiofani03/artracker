package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

type replaySettingsRequest struct {
	ScreenshotEnabled *bool `json:"screenshot_enabled"`
}

// UpdateReplaySettings handles PATCH /api/v1/projects/:projectID/replay-settings (admin only).
func (h *Handler) UpdateReplaySettings(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return errProjectNotFound
	}

	var req replaySettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	if req.ScreenshotEnabled == nil {
		return fiber.NewError(fiber.StatusBadRequest, "screenshot_enabled is required")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var p Project
	if err := scanProject(h.db.QueryRow(ctx,
		`UPDATE projects SET replay_screenshot_enabled = $2 WHERE id = $1 RETURNING `+projectColumns,
		projectID, *req.ScreenshotEnabled), &p); err != nil {
		return err
	}
	h.replaySwitch.invalidate(projectID)
	return c.JSON(p)
}
