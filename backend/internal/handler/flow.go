package handler

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

type flowScreen struct {
	Screen        string `json:"screen"`
	Platform      string `json:"platform"`
	Sessions      int    `json:"sessions"`
	Views         int    `json:"views"`
	MedianMS      int    `json:"median_ms"`
	P75MS         int    `json:"p75_ms"`
	EntrySessions int    `json:"entry_sessions"`
	ExitSessions  int    `json:"exit_sessions"`
}

// ListFlowScreens handles GET /api/v1/projects/:projectID/flow/screens.
func (h *Handler) ListFlowScreens(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	from, to, err := heatmapRange(c)
	if err != nil {
		return err
	}
	platform := strings.TrimSpace(c.Query("platform"))
	version := strings.TrimSpace(c.Query("app_version"))

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	rows, err := h.db.Query(ctx, `
		SELECT screen, platform,
		       count(DISTINCT session_id)::int,
		       count(*)::int,
		       round(percentile_cont(0.5)  WITHIN GROUP (ORDER BY duration_ms))::int,
		       round(percentile_cont(0.75) WITHIN GROUP (ORDER BY duration_ms))::int,
		       count(DISTINCT session_id) FILTER (WHERE is_entry)::int,
		       count(DISTINCT session_id) FILTER (WHERE is_exit)::int
		  FROM screen_views
		 WHERE project_id = $1 AND entered_at >= $2 AND entered_at < $3
		   AND ($4 = '' OR platform = $4) AND ($5 = '' OR app_version = $5)
		 GROUP BY screen, platform
		 ORDER BY 3 DESC, 4 DESC, screen, platform`, projectID, from, to, platform, version)
	if err != nil {
		return err
	}
	defer rows.Close()
	screens := []flowScreen{}
	for rows.Next() {
		var s flowScreen
		if err := rows.Scan(&s.Screen, &s.Platform, &s.Sessions, &s.Views, &s.MedianMS, &s.P75MS, &s.EntrySessions, &s.ExitSessions); err != nil {
			return err
		}
		screens = append(screens, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	versions := []string{}
	vrows, err := h.db.Query(ctx, `
		SELECT DISTINCT app_version FROM screen_views
		 WHERE project_id = $1 AND entered_at >= $2 AND entered_at < $3 AND app_version <> '' AND ($4 = '' OR platform = $4)
		 LIMIT 500`, projectID, from, to, platform)
	if err != nil {
		return err
	}
	defer vrows.Close()
	for vrows.Next() {
		var v string
		if err := vrows.Scan(&v); err != nil {
			return err
		}
		versions = append(versions, v)
	}
	if err := vrows.Err(); err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(versions))) // byte order, independent of DB collation
	if len(versions) > 50 {
		versions = versions[:50]
	}

	// Processed sessions that produced no screen_views: their screens were not recognized.
	var unrec int
	if err := h.db.QueryRow(ctx, `
		SELECT count(*)::int FROM replay_sessions r
		 WHERE r.project_id = $1 AND r.taps_processed_at IS NOT NULL
		   AND r.start_time >= $2 AND r.start_time < $3
		   AND NOT EXISTS (SELECT 1 FROM screen_views v WHERE v.project_id = r.project_id AND v.session_id = r.id)`,
		projectID, from, to).Scan(&unrec); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"screens": screens, "app_versions": versions, "unrecognized_sessions": unrec})
}
