package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

// NavCounts feeds the badges in the dashboard sidebar.
type NavCounts struct {
	// FrustratedReplays24h counts sessions of the last 24 hours that hit an error, a rage click or a dead click.
	FrustratedReplays24h int `json:"frustrated_replays_24h"`
	// OpenIssues counts issues whose status is still "open".
	OpenIssues int `json:"open_issues"`
	// FiringAlerts counts alert incidents that are still firing.
	FiringAlerts int `json:"firing_alerts"`
}

// GetNavCounts returns the sidebar badge counts of one project in a single round trip.
func (h *Handler) GetNavCounts(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var out NavCounts
	err := h.db.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM replay_sessions
		    WHERE project_id = $1
		      AND start_time >= now() - interval '24 hours'
		      AND (has_error OR has_rage_click OR has_dead_click)),
		  (SELECT count(*) FROM issues WHERE project_id = $1 AND status = 'open'),
		  (SELECT count(*) FROM alert_incidents WHERE project_id = $1 AND status = 'firing')`,
		projectID).Scan(&out.FrustratedReplays24h, &out.OpenIssues, &out.FiringAlerts)
	if err != nil {
		return err
	}
	return c.JSON(out)
}
