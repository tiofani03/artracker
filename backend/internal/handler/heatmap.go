package handler

import (
	"context"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/tiofani03/artracker/backend/internal/heatmap"
)

const (
	heatmapDefaultCols  = 40
	heatmapMaxCols      = 100
	heatmapMaxRows      = 250
	heatmapMaxRage      = 500
	heatmapMaxExcluded  = 200
	heatmapMaxNameLen   = 200
	heatmapDefaultRange = 7 * 24 * time.Hour
	heatmapAspectTol    = 0.05
)

// heatmapRange parses ?from&to (RFC3339 or YYYY-MM-DD); default is the last 7 days. A date-only `to` is
// inclusive of that whole day.
func heatmapRange(c *fiber.Ctx) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	from, to := now.Add(-heatmapDefaultRange), now
	parse := func(s string, endOfDay bool) (time.Time, error) {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, nil
		}
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, fiber.NewError(fiber.StatusBadRequest, "invalid date, use RFC3339 or YYYY-MM-DD")
		}
		if endOfDay {
			t = t.Add(24 * time.Hour)
		}
		return t, nil
	}
	var err error
	if s := strings.TrimSpace(c.Query("from")); s != "" {
		if from, err = parse(s, false); err != nil {
			return from, to, err
		}
	}
	if s := strings.TrimSpace(c.Query("to")); s != "" {
		if to, err = parse(s, true); err != nil {
			return from, to, err
		}
	}
	if !to.After(from) {
		return from, to, fiber.NewError(fiber.StatusBadRequest, "'to' must be after 'from'")
	}
	return from, to, nil
}

type heatmapScreen struct {
	Screen   string `json:"screen"`
	Platform string `json:"platform"`
	Sessions int    `json:"sessions"`
	Taps     int    `json:"taps"`
}

// ListHeatmapScreens handles GET /api/v1/projects/:projectID/heatmap/screens.
func (h *Handler) ListHeatmapScreens(c *fiber.Ctx) error {
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
		SELECT v.screen, v.platform, v.sessions, COALESCE(t.taps, 0)
		FROM (SELECT screen, platform, count(DISTINCT session_id)::int AS sessions
		        FROM screen_visits
		       WHERE project_id = $1 AND ts >= $2 AND ts < $3
		         AND ($4 = '' OR platform = $4) AND ($5 = '' OR app_version = $5)
		       GROUP BY screen, platform) v
		LEFT JOIN (SELECT screen, platform, count(*)::int AS taps
		        FROM screen_taps
		       WHERE project_id = $1 AND ts >= $2 AND ts < $3
		         AND ($4 = '' OR platform = $4) AND ($5 = '' OR app_version = $5)
		       GROUP BY screen, platform) t USING (screen, platform)
		ORDER BY v.sessions DESC, v.screen, v.platform`, projectID, from, to, platform, version)
	if err != nil {
		return err
	}
	defer rows.Close()
	screens := []heatmapScreen{}
	for rows.Next() {
		var s heatmapScreen
		if err := rows.Scan(&s.Screen, &s.Platform, &s.Sessions, &s.Taps); err != nil {
			return err
		}
		screens = append(screens, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	versions := []string{}
	vrows, err := h.db.Query(ctx, `
		SELECT DISTINCT app_version FROM screen_visits
		 WHERE project_id = $1 AND ts >= $2 AND ts < $3 AND app_version <> '' AND ($4 = '' OR platform = $4)
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
	var unrec int
	if err := h.db.QueryRow(ctx,
		`SELECT COALESCE(sum(taps_unrecognized), 0)::int FROM replay_sessions
		  WHERE project_id = $1 AND start_time >= $2 AND start_time < $3`, projectID, from, to).Scan(&unrec); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"screens": screens, "unrecognized_taps": unrec, "app_versions": versions})
}

type heatmapCell struct {
	C        int `json:"c"`
	R        int `json:"r"`
	Sessions int `json:"sessions"`
	Taps     int `json:"taps"`
}

type heatmapRagePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type heatmapAspect struct {
	Aspect   float64 `json:"aspect"`
	Sessions int     `json:"sessions"`
}

type heatmapBackground struct {
	URL        string  `json:"url"`
	FrameIndex int     `json:"frame_index"`
	VW         float64 `json:"vw"`
	VH         float64 `json:"vh"`
	AppVersion string  `json:"app_version"`
}

type heatmapResponse struct {
	Cols           int                `json:"cols"`
	Rows           int                `json:"rows"`
	Cells          []heatmapCell      `json:"cells"`
	Rage           []heatmapRagePoint `json:"rage"`
	ScreenSessions int                `json:"screen_sessions"`
	Aspect         float64            `json:"aspect"`
	Aspects        []heatmapAspect    `json:"aspects"`
	Background     *heatmapBackground `json:"background"`
}

func heatmapIntParam(c *fiber.Ctx, name string, def, max int) (int, error) {
	s := strings.TrimSpace(c.Query(name))
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fiber.NewError(fiber.StatusBadRequest, "invalid "+name)
	}
	if n > max {
		n = max
	}
	return n, nil
}

// GetHeatmap handles GET /api/v1/projects/:projectID/heatmap.
func (h *Handler) GetHeatmap(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	screen := strings.TrimSpace(c.Query("screen"))
	platform := strings.TrimSpace(c.Query("platform"))
	orientation := strings.TrimSpace(c.Query("orientation"))
	if screen == "" || platform == "" || (orientation != "portrait" && orientation != "landscape") {
		return fiber.NewError(fiber.StatusBadRequest, "screen, platform and orientation (portrait|landscape) are required")
	}
	landscape := orientation == "landscape"
	version := strings.TrimSpace(c.Query("app_version"))
	from, to, err := heatmapRange(c)
	if err != nil {
		return err
	}
	cols, err := heatmapIntParam(c, "cols", heatmapDefaultCols, heatmapMaxCols)
	if err != nil {
		return err
	}
	var aspect float64
	if s := strings.TrimSpace(c.Query("aspect")); s != "" {
		aspect, err = strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(aspect) || aspect < 1 || aspect > 10 {
			return fiber.NewError(fiber.StatusBadRequest, "invalid aspect")
		}
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	out := heatmapResponse{Cols: cols, Cells: []heatmapCell{}, Rage: []heatmapRagePoint{}, Aspects: []heatmapAspect{}}

	arows, err := h.db.Query(ctx, `
		SELECT round(aspect_bucket::numeric, 1)::float8 AS a, count(DISTINCT session_id)::int AS n
		  FROM screen_visits
		 WHERE project_id = $1 AND screen = $2 AND platform = $3 AND landscape = $4
		   AND ts >= $5 AND ts < $6 AND ($7 = '' OR app_version = $7)
		 GROUP BY a ORDER BY n DESC, a`, projectID, screen, platform, landscape, from, to, version)
	if err != nil {
		return err
	}
	for arows.Next() {
		var a heatmapAspect
		if err := arows.Scan(&a.Aspect, &a.Sessions); err != nil {
			arows.Close()
			return err
		}
		out.Aspects = append(out.Aspects, a)
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return err
	}
	if aspect == 0 && len(out.Aspects) > 0 {
		aspect = out.Aspects[0].Aspect
	}
	out.Aspect = aspect
	rowsN := 0
	if aspect == 0 {
		aspect = 2
	}
	// aspect is long side / short side: a portrait screen is taller than wide, a landscape one wider
	defRows := float64(cols) * aspect
	if landscape {
		defRows = float64(cols) / aspect
	}
	if rowsN, err = heatmapIntParam(c, "rows", max(1, int(math.Round(defRows))), heatmapMaxRows); err != nil {
		return err
	}
	if rowsN > heatmapMaxRows {
		rowsN = heatmapMaxRows
	}
	out.Rows = rowsN
	if len(out.Aspects) == 0 {
		return c.JSON(out)
	}

	if err := h.db.QueryRow(ctx, `
		SELECT count(DISTINCT session_id)::int FROM screen_visits
		 WHERE project_id = $1 AND screen = $2 AND platform = $3 AND landscape = $4
		   AND abs(aspect_bucket - $5) < $8 AND ts >= $6 AND ts < $7 AND ($9 = '' OR app_version = $9)`,
		projectID, screen, platform, landscape, aspect, from, to, heatmapAspectTol, version).Scan(&out.ScreenSessions); err != nil {
		return err
	}

	crows, err := h.db.Query(ctx, `
		SELECT LEAST($1::int - 1, floor(x_norm * $1)::int) AS c,
		       LEAST($2::int - 1, floor(y_norm * $2)::int) AS r,
		       count(DISTINCT session_id)::int, count(*)::int
		  FROM screen_taps
		 WHERE project_id = $3 AND screen = $4 AND platform = $5 AND landscape = $6
		   AND abs(aspect_bucket - $7) < $10 AND ts >= $8 AND ts < $9 AND ($11 = '' OR app_version = $11)
		 GROUP BY c, r ORDER BY c, r`,
		cols, rowsN, projectID, screen, platform, landscape, aspect, from, to, heatmapAspectTol, version)
	if err != nil {
		return err
	}
	for crows.Next() {
		var cell heatmapCell
		if err := crows.Scan(&cell.C, &cell.R, &cell.Sessions, &cell.Taps); err != nil {
			crows.Close()
			return err
		}
		out.Cells = append(out.Cells, cell)
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		return err
	}

	rrows, err := h.db.Query(ctx, `
		SELECT x_norm, y_norm FROM screen_taps
		 WHERE project_id = $1 AND screen = $2 AND platform = $3 AND landscape = $4 AND rage
		   AND abs(aspect_bucket - $5) < $8 AND ts >= $6 AND ts < $7 AND ($9 = '' OR app_version = $9)
		 ORDER BY ts DESC LIMIT $10`,
		projectID, screen, platform, landscape, aspect, from, to, heatmapAspectTol, version, heatmapMaxRage)
	if err != nil {
		return err
	}
	for rrows.Next() {
		var p heatmapRagePoint
		var x, y float32
		if err := rrows.Scan(&x, &y); err != nil {
			rrows.Close()
			return err
		}
		p.X, p.Y = float64(x), float64(y)
		out.Rage = append(out.Rage, p)
	}
	rrows.Close()
	if err := rrows.Err(); err != nil {
		return err
	}

	var (
		bg        heatmapBackground
		sessionID string
		chunkSeq  int
		vw, vh    float32
	)
	// Prefer a background captured inside the requested range; fall back to the latest overall.
	err = h.db.QueryRow(ctx, `
		SELECT session_id, chunk_seq, frame_index, vw, vh, app_version FROM screen_backgrounds
		 WHERE project_id = $1 AND screen = $2 AND platform = $3 AND landscape = $4
		   AND abs(aspect_bucket - $5) < $6 AND ($7 = '' OR app_version = $7)
		 ORDER BY (captured_at >= $8 AND captured_at < $9) DESC, captured_at DESC LIMIT 1`,
		projectID, screen, platform, landscape, aspect, heatmapAspectTol, version, from, to).
		Scan(&sessionID, &chunkSeq, &bg.FrameIndex, &vw, &vh, &bg.AppVersion)
	if err == nil {
		bg.VW, bg.VH = float64(vw), float64(vh)
		var objectKey string
		kerr := h.db.QueryRow(ctx,
			`SELECT object_key FROM replay_chunks WHERE project_id = $1 AND session_id = $2 AND chunk_seq = $3`,
			projectID, sessionID, chunkSeq).Scan(&objectKey)
		if kerr == nil {
			if bg.URL = h.chunkDownloadURL(c, ctx, objectKey); bg.URL != "" {
				out.Background = &bg
			}
		} else if !errors.Is(kerr, pgx.ErrNoRows) {
			return kerr
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return c.JSON(out)
}

// GetHeatmapSettings handles GET /api/v1/projects/:projectID/heatmap-settings.
func (h *Handler) GetHeatmapSettings(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	var excluded []string
	if err := h.db.QueryRow(ctx, `SELECT heatmap_excluded_screens FROM projects WHERE id = $1`, projectID).Scan(&excluded); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errProjectNotFound
		}
		return err
	}
	if excluded == nil {
		excluded = []string{}
	}
	return c.JSON(fiber.Map{"excluded_screens": excluded, "sensitive_rule": heatmap.SensitiveRule})
}

type heatmapSettingsRequest struct {
	ExcludedScreens []string `json:"excluded_screens"`
}

// UpdateHeatmapSettings handles PATCH /api/v1/projects/:projectID/heatmap-settings (admin only). Newly
// excluded screens have their taps, visits and backgrounds deleted in the same transaction.
func (h *Handler) UpdateHeatmapSettings(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	var req heatmapSettingsRequest
	if err := c.BodyParser(&req); err != nil || req.ExcludedScreens == nil {
		return fiber.NewError(fiber.StatusBadRequest, "excluded_screens is required")
	}
	if len(req.ExcludedScreens) > heatmapMaxExcluded {
		return fiber.NewError(fiber.StatusBadRequest, "too many excluded screens (max 200)")
	}
	seen := map[string]bool{}
	list := []string{}
	for _, s := range req.ExcludedScreens {
		s = strings.TrimSpace(s)
		if len(s) > heatmapMaxNameLen {
			return fiber.NewError(fiber.StatusBadRequest, "screen name too long (max 200 characters)")
		}
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		list = append(list, s)
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var old []string
	if err := tx.QueryRow(ctx, `SELECT heatmap_excluded_screens FROM projects WHERE id = $1 FOR UPDATE`, projectID).Scan(&old); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errProjectNotFound
		}
		return err
	}
	had := map[string]bool{}
	for _, s := range old {
		had[s] = true
	}
	var added []string
	for _, s := range list {
		if !had[s] {
			added = append(added, s)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE projects SET heatmap_excluded_screens = $2 WHERE id = $1`, projectID, list); err != nil {
		return err
	}
	if len(added) > 0 {
		for _, tbl := range []string{"screen_taps", "screen_visits", "screen_backgrounds"} {
			if _, err := tx.Exec(ctx, `DELETE FROM `+tbl+` WHERE project_id = $1 AND screen = ANY($2)`, projectID, added); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"excluded_screens": list, "sensitive_rule": heatmap.SensitiveRule})
}
