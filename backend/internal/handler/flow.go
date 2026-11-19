package handler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/heatmap"
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
	// Sessions the worker gave up on (attempts exhausted) are excluded: they lack data
	// for other reasons, which is not an SDK naming problem.
	var unrec int
	if err := h.db.QueryRow(ctx, `
		SELECT count(*)::int FROM replay_sessions r
		 WHERE r.project_id = $1 AND r.taps_processed_at IS NOT NULL
		   AND r.taps_attempts < $4
		   AND r.start_time >= $2 AND r.start_time < $3
		   AND NOT EXISTS (SELECT 1 FROM screen_views v WHERE v.project_id = r.project_id AND v.session_id = r.id)`,
		projectID, from, to, heatmap.MaxAttempts).Scan(&unrec); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"screens": screens, "app_versions": versions, "unrecognized_sessions": unrec})
}

const (
	flowTopN     = 6
	flowMaxSteps = 5
	flowOther    = "Other"
	flowExit     = "Exit"
	flowStart    = "Start"
)

type flowNode struct {
	ID     string `json:"id"`
	Step   int    `json:"step"`
	Screen string `json:"screen"`
}

type flowLink struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Sessions int    `json:"sessions"`
}

func flowTerminal(screen string) bool { return screen == flowExit || screen == flowStart }

// buildFlowPaths aggregates per-session paths into Sankey nodes and links. paths[i][0] is the chosen
// screen; a path ends with a terminal ("Exit" going forward, "Start" going back) when the session
// ended there. At each step only the topN screens (by sessions, terminals excluded) keep their own
// node; the rest fold into "Other", which still continues to later steps. Nodes are ordered by step,
// then sessions desc, with Other and terminals last; links by source step, sessions desc, then ids.
func buildFlowPaths(paths [][]string, topN int) ([]flowNode, []flowLink) {
	// sessions per (step, screen) to decide the top screens of each step
	counts := map[int]map[string]int{}
	for _, p := range paths {
		for k, scr := range p {
			if flowTerminal(scr) {
				continue
			}
			if counts[k] == nil {
				counts[k] = map[string]int{}
			}
			counts[k][scr]++
		}
	}
	keep := map[int]map[string]bool{}
	for k, m := range counts {
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Slice(names, func(i, j int) bool {
			if m[names[i]] != m[names[j]] {
				return m[names[i]] > m[names[j]]
			}
			return names[i] < names[j]
		})
		keep[k] = map[string]bool{}
		for i, n := range names {
			if i < topN {
				keep[k][n] = true
			}
		}
	}
	fold := func(k int, scr string) string {
		if flowTerminal(scr) || keep[k][scr] {
			return scr
		}
		return flowOther
	}

	type nodeKey struct {
		step   int
		screen string
	}
	nodeSessions := map[nodeKey]int{}
	linkSessions := map[[2]nodeKey]int{}
	for _, p := range paths {
		for k, raw := range p {
			cur := nodeKey{k, fold(k, raw)}
			nodeSessions[cur]++
			if k > 0 {
				linkSessions[[2]nodeKey{{k - 1, fold(k-1, p[k-1])}, cur}]++
			}
		}
	}

	rank := func(screen string) int { // sort group inside a step
		switch {
		case flowTerminal(screen):
			return 2
		case screen == flowOther:
			return 1
		}
		return 0
	}
	keys := make([]nodeKey, 0, len(nodeSessions))
	for k := range nodeSessions {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.step != b.step {
			return a.step < b.step
		}
		if ra, rb := rank(a.screen), rank(b.screen); ra != rb {
			return ra < rb
		}
		if nodeSessions[a] != nodeSessions[b] {
			return nodeSessions[a] > nodeSessions[b]
		}
		return a.screen < b.screen
	})
	id := func(k nodeKey) string { return fmt.Sprintf("%d:%s", k.step, k.screen) }
	nodes := make([]flowNode, 0, len(keys))
	for _, k := range keys {
		nodes = append(nodes, flowNode{ID: id(k), Step: k.step, Screen: k.screen})
	}

	lkeys := make([][2]nodeKey, 0, len(linkSessions))
	for k := range linkSessions {
		lkeys = append(lkeys, k)
	}
	sort.Slice(lkeys, func(i, j int) bool {
		a, b := lkeys[i], lkeys[j]
		if a[0].step != b[0].step {
			return a[0].step < b[0].step
		}
		if linkSessions[a] != linkSessions[b] {
			return linkSessions[a] > linkSessions[b]
		}
		if id(a[0]) != id(b[0]) {
			return id(a[0]) < id(b[0])
		}
		return id(a[1]) < id(b[1])
	})
	links := make([]flowLink, 0, len(lkeys))
	for _, k := range lkeys {
		links = append(links, flowLink{Source: id(k[0]), Target: id(k[1]), Sessions: linkSessions[k]})
	}
	return nodes, links
}

// GetFlowPaths handles GET /api/v1/projects/:projectID/flow/paths.
func (h *Handler) GetFlowPaths(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	screen := strings.TrimSpace(c.Query("screen"))
	platform := strings.TrimSpace(c.Query("platform"))
	if screen == "" || platform == "" {
		return fiber.NewError(fiber.StatusBadRequest, "screen and platform are required")
	}
	direction := strings.TrimSpace(c.Query("direction"))
	if direction == "" {
		direction = "next"
	}
	if direction != "next" && direction != "prev" {
		return fiber.NewError(fiber.StatusBadRequest, "direction must be next or prev")
	}
	steps, err := heatmapIntParam(c, "steps", 3, flowMaxSteps+1)
	if err != nil {
		return err
	}
	if steps > flowMaxSteps {
		return fiber.NewError(fiber.StatusBadRequest, "steps must be between 1 and 5")
	}
	from, to, err := heatmapRange(c)
	if err != nil {
		return err
	}
	version := strings.TrimSpace(c.Query("app_version"))

	// next: views s0..s0+steps in order; prev: views s0..s0-steps, nearest first.
	before, after, dir := 0, steps, 1
	if direction == "prev" {
		before, after, dir = steps, 0, -1
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	rows, err := h.db.Query(ctx, `
		WITH start AS (
			SELECT session_id, min(seq) AS s0
			  FROM screen_views
			 WHERE project_id = $1 AND screen = $2 AND platform = $3
			   AND entered_at >= $4 AND entered_at < $5
			   AND ($6 = '' OR app_version = $6)
			 GROUP BY session_id)
		SELECT v.session_id, v.screen, v.is_entry, v.is_exit
		  FROM start s
		  JOIN screen_views v ON v.project_id = $1 AND v.session_id = s.session_id
		   AND v.seq BETWEEN s.s0 - $7 AND s.s0 + $8
		 ORDER BY v.session_id, (v.seq - s.s0) * $9`,
		projectID, screen, platform, from, to, version, before, after, dir)
	if err != nil {
		return err
	}
	defer rows.Close()

	paths := [][]string{}
	var cur []string
	var curSID string
	finish := func() {
		if cur != nil {
			paths = append(paths, cur)
		}
	}
	for rows.Next() {
		var sid, scr string
		var entry, exit bool
		if err := rows.Scan(&sid, &scr, &entry, &exit); err != nil {
			return err
		}
		if cur == nil || sid != curSID {
			finish()
			cur, curSID = nil, sid
		}
		if n := len(cur); n > 0 && flowTerminal(cur[n-1]) {
			continue // path already ended at this session's first/last view
		}
		cur = append(cur, scr)
		// a terminal fits only if the view itself is within `steps` positions of the start
		if len(cur) <= steps {
			if direction == "next" && exit {
				cur = append(cur, flowExit)
			} else if direction == "prev" && entry {
				cur = append(cur, flowStart)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	finish()

	nodes, links := buildFlowPaths(paths, flowTopN)
	return c.JSON(fiber.Map{"screen": screen, "direction": direction, "sessions": len(paths), "nodes": nodes, "links": links})
}
