package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tiofani03/artracker/backend/internal/appstart"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

const (
	appStartDefaultLimit = 50
	appStartMaxLimit     = 200
	appStartQueryTimeout = 10 * time.Second
	appStartMaxVersions  = 50
	appStartTopVersions  = 10
	appStartTopFunctions = 50
)

type appStartScope struct {
	Environment  string   `json:"environment"`
	Environments []string `json:"environments"`
	Versions     []string `json:"versions"`
}

type appStartPhaseMedian struct {
	Name     string `json:"name"`
	MedianMs int    `json:"median_ms"`
}

type appStartDaily struct {
	Day   string `json:"day"`
	P50   int    `json:"p50"`
	P90   int    `json:"p90"`
	Count int    `json:"count"`
}

type appStartByVersion struct {
	Version string `json:"version"`
	P50     int    `json:"p50"`
	P90     int    `json:"p90"`
	Count   int    `json:"count"`
}

type appStartSummary struct {
	appStartScope
	Count       int                   `json:"count"`
	P50         int                   `json:"p50"`
	P75         int                   `json:"p75"`
	P90         int                   `json:"p90"`
	P95         int                   `json:"p95"`
	ProfiledPct float64               `json:"profiled_pct"`
	Phases      []appStartPhaseMedian `json:"phases"`
	Daily       []appStartDaily       `json:"daily"`
	ByVersion   []appStartByVersion   `json:"by_version"`
}

type appStartFunction struct {
	Function           string  `json:"function"`
	IsApp              bool    `json:"is_app"`
	StartsPct          float64 `json:"starts_pct"`
	MedianTotalMs      int     `json:"median_total_ms"`
	P90TotalMs         int     `json:"p90_total_ms"`
	MedianSelfMs       int     `json:"median_self_ms"`
	MedianAttributedMs int     `json:"median_attributed_ms"`
	ExampleStartID     string  `json:"example_start_id"`
}

type appStartFunctions struct {
	appStartScope
	Profiles  int                `json:"profiles"`
	Functions []appStartFunction `json:"functions"`
}

type appStartRow struct {
	StartID     string    `json:"start_id"`
	Type        string    `json:"type"`
	Platform    string    `json:"platform"`
	AppVersion  string    `json:"app_version"`
	Build       string    `json:"build"`
	DeviceModel string    `json:"device_model"`
	OSVersion   string    `json:"os_version"`
	FirstScreen string    `json:"first_screen"`
	SessionID   string    `json:"session_id"`
	DurationMs  int       `json:"duration_ms"`
	Profiled    bool      `json:"profiled"`
	StartedAt   time.Time `json:"started_at"`
}

type appStartProfile struct {
	Status string                 `json:"status"`
	Data   *appstart.Symbolicated `json:"data"`
}

type appStartDetail struct {
	appStartRow
	Phases     []ingest.AppStartPhase `json:"phases"`
	RAMMB      *int                   `json:"ram_mb"`
	Trampoline bool                   `json:"trampoline"`
	Profile    *appStartProfile       `json:"profile"`
}

// appStartFilter is the app_starts filter shared by summary and list.
type appStartFilter struct {
	projectID   int64
	from, to    time.Time
	environment string
	platform    string
	version     string
	typ         string
	ramTier     string
}

func badRequest(msg string) error { return fiber.NewError(fiber.StatusBadRequest, msg) }

// parseAppStartFilter reads the common query parameters. Summary and list also pass withType.
func parseAppStartFilter(c *fiber.Ctx, projectID int64, withType bool) (appStartFilter, error) {
	from, to, err := heatmapRange(c)
	if err != nil {
		return appStartFilter{}, err
	}
	f := appStartFilter{
		projectID:   projectID,
		from:        from,
		to:          to,
		environment: strings.TrimSpace(c.Query("environment")),
		platform:    strings.TrimSpace(c.Query("platform")),
		version:     strings.TrimSpace(c.Query("app_version")),
	}
	if !withType {
		return f, nil
	}
	f.typ = strings.TrimSpace(c.Query("type"))
	if f.typ == "" {
		f.typ = "cold"
	}
	switch f.typ {
	case "cold", "warm", "hot":
	default:
		return f, badRequest("type must be cold, warm or hot")
	}
	f.ramTier = strings.TrimSpace(c.Query("ram_tier"))
	switch f.ramTier {
	case "", "low", "mid", "high":
	default:
		return f, badRequest("ram_tier must be low, mid or high")
	}
	return f, nil
}

// where renders the filter as a WHERE body over app_starts plus its positional arguments.
func (f appStartFilter) where() (string, []any) {
	clause := `project_id = $1 AND started_at >= $2 AND started_at < $3
		AND ($4 = '' OR environment = $4) AND ($5 = '' OR platform = $5) AND ($6 = '' OR app_version = $6)
		AND type = $7`
	switch f.ramTier { // constants only, never user text
	case "low":
		clause += ` AND ram_mb < 4096`
	case "mid":
		clause += ` AND ram_mb BETWEEN 4096 AND 8192`
	case "high":
		clause += ` AND ram_mb > 8192`
	}
	return clause, []any{f.projectID, f.from, f.to, f.environment, f.platform, f.version, f.typ}
}

// resolveAppStartScope lists the environments seen in the range and settles the effective environment:
// an explicit one is kept, otherwise production when present, otherwise ” (all). It also lists the
// app versions seen under that environment and platform, newest first.
func (h *Handler) resolveAppStartScope(ctx context.Context, projectID int64, from, to time.Time, environment, platform string) (appStartScope, error) {
	scope := appStartScope{Environment: environment, Environments: []string{}, Versions: []string{}}
	rows, err := h.db.Query(ctx,
		`SELECT DISTINCT environment FROM app_starts WHERE project_id = $1 AND started_at >= $2 AND started_at < $3`,
		projectID, from, to)
	if err != nil {
		return scope, err
	}
	defer rows.Close()
	for rows.Next() {
		var env string
		if err := rows.Scan(&env); err != nil {
			return scope, err
		}
		scope.Environments = append(scope.Environments, env)
	}
	if err := rows.Err(); err != nil {
		return scope, err
	}
	rows.Close()
	sort.Strings(scope.Environments)
	if scope.Environment == "" {
		for _, env := range scope.Environments {
			if env == "production" {
				scope.Environment = "production"
				break
			}
		}
	}

	vrows, err := h.db.Query(ctx,
		`SELECT DISTINCT app_version FROM app_starts
		  WHERE project_id = $1 AND started_at >= $2 AND started_at < $3 AND app_version <> ''
		    AND ($4 = '' OR environment = $4) AND ($5 = '' OR platform = $5)
		  LIMIT 500`, projectID, from, to, scope.Environment, platform)
	if err != nil {
		return scope, err
	}
	defer vrows.Close()
	for vrows.Next() {
		var v string
		if err := vrows.Scan(&v); err != nil {
			return scope, err
		}
		scope.Versions = append(scope.Versions, v)
	}
	if err := vrows.Err(); err != nil {
		return scope, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(scope.Versions))) // byte order, independent of DB collation
	if len(scope.Versions) > appStartMaxVersions {
		scope.Versions = scope.Versions[:appStartMaxVersions]
	}
	return scope, nil
}

// pct returns percentile i of a percentile_cont array result, rounded; 0 when the group was empty.
func pct(vals []float64, i int) int {
	if i >= len(vals) {
		return 0
	}
	return int(math.Round(vals[i]))
}

// AppStartSummary handles GET /api/v1/projects/:projectID/app-starts/summary.
func (h *Handler) AppStartSummary(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	f, err := parseAppStartFilter(c, projectID, true)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), appStartQueryTimeout)
	defer cancel()

	scope, err := h.resolveAppStartScope(ctx, projectID, f.from, f.to, f.environment, f.platform)
	if err != nil {
		return err
	}
	f.environment = scope.Environment
	where, args := f.where()
	out := appStartSummary{
		appStartScope: scope,
		Phases:        []appStartPhaseMedian{},
		Daily:         []appStartDaily{},
		ByVersion:     []appStartByVersion{},
	}

	var qs []float64
	if err := h.db.QueryRow(ctx, `
		SELECT count(*)::int,
		       percentile_cont(ARRAY[0.5, 0.75, 0.9, 0.95]) WITHIN GROUP (ORDER BY duration_ms),
		       COALESCE(round(100.0 * count(*) FILTER (WHERE profiled) / NULLIF(count(*), 0), 1), 0)::float8
		  FROM app_starts WHERE `+where, args...).Scan(&out.Count, &qs, &out.ProfiledPct); err != nil {
		return err
	}
	out.P50, out.P75, out.P90, out.P95 = pct(qs, 0), pct(qs, 1), pct(qs, 2), pct(qs, 3)
	if out.Count == 0 {
		return c.JSON(out)
	}

	// Phases keep their order: sort by the median position each phase occupies.
	prows, err := h.db.Query(ctx, `
		WITH s AS (SELECT phases FROM app_starts WHERE `+where+`),
		     p AS (SELECT e ->> 'name' AS name, (e ->> 'ms')::numeric AS ms, ord
		             FROM s, jsonb_array_elements(s.phases) WITH ORDINALITY AS t(e, ord))
		SELECT name, round(percentile_cont(0.5) WITHIN GROUP (ORDER BY ms))::int
		  FROM p GROUP BY name
		 ORDER BY percentile_cont(0.5) WITHIN GROUP (ORDER BY ord), name`, args...)
	if err != nil {
		return err
	}
	defer prows.Close()
	for prows.Next() {
		var p appStartPhaseMedian
		if err := prows.Scan(&p.Name, &p.MedianMs); err != nil {
			return err
		}
		out.Phases = append(out.Phases, p)
	}
	if err := prows.Err(); err != nil {
		return err
	}
	prows.Close()

	drows, err := h.db.Query(ctx, `
		SELECT to_char(date_trunc('day', started_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day,
		       percentile_cont(ARRAY[0.5, 0.9]) WITHIN GROUP (ORDER BY duration_ms), count(*)::int
		  FROM app_starts WHERE `+where+` GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		return err
	}
	defer drows.Close()
	for drows.Next() {
		var d appStartDaily
		var q []float64
		if err := drows.Scan(&d.Day, &q, &d.Count); err != nil {
			return err
		}
		d.P50, d.P90 = pct(q, 0), pct(q, 1)
		out.Daily = append(out.Daily, d)
	}
	if err := drows.Err(); err != nil {
		return err
	}
	drows.Close()

	vrows, err := h.db.Query(ctx, `
		SELECT app_version, percentile_cont(ARRAY[0.5, 0.9]) WITHIN GROUP (ORDER BY duration_ms), count(*)::int
		  FROM app_starts WHERE `+where+`
		 GROUP BY app_version ORDER BY max(started_at) DESC, app_version DESC LIMIT `+strconv.Itoa(appStartTopVersions), args...)
	if err != nil {
		return err
	}
	defer vrows.Close()
	for vrows.Next() {
		var v appStartByVersion
		var q []float64
		if err := vrows.Scan(&v.Version, &q, &v.Count); err != nil {
			return err
		}
		v.P50, v.P90 = pct(q, 0), pct(q, 1)
		out.ByVersion = append(out.ByVersion, v)
	}
	if err := vrows.Err(); err != nil {
		return err
	}
	return c.JSON(out)
}

// AppStartFunctions handles GET /api/v1/projects/:projectID/app-starts/functions.
func (h *Handler) AppStartFunctions(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	f, err := parseAppStartFilter(c, projectID, false)
	if err != nil {
		return err
	}
	appOnly := true
	switch strings.ToLower(strings.TrimSpace(c.Query("app_only"))) {
	case "false", "0":
		appOnly = false
	}
	ctx, cancel := context.WithTimeout(c.Context(), appStartQueryTimeout)
	defer cancel()

	scope, err := h.resolveAppStartScope(ctx, projectID, f.from, f.to, f.environment, f.platform)
	if err != nil {
		return err
	}
	out := appStartFunctions{appStartScope: scope, Functions: []appStartFunction{}}

	rows, err := h.db.Query(ctx, `
		WITH f AS (SELECT * FROM app_start_fn_stats WHERE project_id = $1 AND started_at >= $2 AND started_at < $3
		             AND ($4 = '' OR platform = $4) AND ($5 = '' OR app_version = $5) AND ($6 = '' OR environment = $6)),
		     n AS (SELECT count(DISTINCT start_id) AS profiles FROM f)
		SELECT function, bool_or(is_app),
		       round(100.0 * count(*) / NULLIF(max(n.profiles), 0), 1)::float8,
		       round(percentile_cont(0.5) WITHIN GROUP (ORDER BY total_ms))::int,
		       round(percentile_cont(0.9) WITHIN GROUP (ORDER BY total_ms))::int,
		       round(percentile_cont(0.5) WITHIN GROUP (ORDER BY self_ms))::int,
		       round(percentile_cont(0.5) WITHIN GROUP (ORDER BY attributed_ms))::int,
		       (array_agg(start_id ORDER BY total_ms DESC))[1], max(n.profiles)::int
		  FROM f CROSS JOIN n
		 WHERE (NOT $7 OR is_app)
		 GROUP BY function
		 ORDER BY CASE WHEN $7 THEN sum(attributed_ms) ELSE sum(self_ms) END DESC, function
		 LIMIT `+strconv.Itoa(appStartTopFunctions),
		projectID, f.from, f.to, f.platform, f.version, scope.Environment, appOnly)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var fn appStartFunction
		if err := rows.Scan(&fn.Function, &fn.IsApp, &fn.StartsPct, &fn.MedianTotalMs, &fn.P90TotalMs,
			&fn.MedianSelfMs, &fn.MedianAttributedMs, &fn.ExampleStartID, &out.Profiles); err != nil {
			return err
		}
		out.Functions = append(out.Functions, fn)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	if len(out.Functions) == 0 { // profiles exist but none has a function that passes the filter
		if err := h.db.QueryRow(ctx, `
			SELECT count(DISTINCT start_id)::int FROM app_start_fn_stats
			 WHERE project_id = $1 AND started_at >= $2 AND started_at < $3
			   AND ($4 = '' OR platform = $4) AND ($5 = '' OR app_version = $5) AND ($6 = '' OR environment = $6)`,
			projectID, f.from, f.to, f.platform, f.version, scope.Environment).Scan(&out.Profiles); err != nil {
			return err
		}
	}
	return c.JSON(out)
}

// appStartListResponse is the paged list of starts.
type appStartListResponse struct {
	Starts     []appStartRow `json:"starts"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func encodeAppStartCursor(key string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key + "|" + strconv.FormatInt(id, 10)))
}

// decodeAppStartCursor splits a cursor into its sort key and row id.
func decodeAppStartCursor(s string) (string, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", 0, badRequest("invalid cursor")
	}
	i := strings.LastIndexByte(string(raw), '|')
	if i < 0 {
		return "", 0, badRequest("invalid cursor")
	}
	id, err := strconv.ParseInt(string(raw[i+1:]), 10, 64)
	if err != nil {
		return "", 0, badRequest("invalid cursor")
	}
	return string(raw[:i]), id, nil
}

// ListAppStarts handles GET /api/v1/projects/:projectID/app-starts.
func (h *Handler) ListAppStarts(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	f, err := parseAppStartFilter(c, projectID, true)
	if err != nil {
		return err
	}
	sortBy := strings.TrimSpace(c.Query("sort"))
	switch sortBy {
	case "":
		sortBy = "slowest"
	case "slowest", "recent":
	default:
		return badRequest("sort must be slowest or recent")
	}
	limit := appStartDefaultLimit
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && n > 0 {
		limit = n
	}
	if limit > appStartMaxLimit {
		limit = appStartMaxLimit
	}

	ctx, cancel := context.WithTimeout(c.Context(), appStartQueryTimeout)
	defer cancel()
	scope, err := h.resolveAppStartScope(ctx, projectID, f.from, f.to, f.environment, f.platform)
	if err != nil {
		return err
	}
	f.environment = scope.Environment
	where, args := f.where()

	orderCol := "duration_ms"
	if sortBy == "recent" {
		orderCol = "started_at"
	}
	if cur := strings.TrimSpace(c.Query("cursor")); cur != "" {
		key, id, err := decodeAppStartCursor(cur)
		if err != nil {
			return err
		}
		var keyArg any
		if sortBy == "recent" {
			t, err := time.Parse(time.RFC3339Nano, key)
			if err != nil {
				return badRequest("invalid cursor")
			}
			keyArg = t
		} else {
			n, err := strconv.Atoi(key)
			if err != nil {
				return badRequest("invalid cursor")
			}
			keyArg = n
		}
		args = append(args, keyArg, id)
		where += fmt.Sprintf(` AND (%s, id) < ($%d, $%d)`, orderCol, len(args)-1, len(args))
	}

	rows, err := h.db.Query(ctx, `
		SELECT id, start_id, type, platform, app_version, build, device_model, os_version, first_screen, session_id,
		       duration_ms, profiled, started_at
		  FROM app_starts WHERE `+where+`
		 ORDER BY `+orderCol+` DESC, id DESC LIMIT `+strconv.Itoa(limit+1), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := appStartListResponse{Starts: []appStartRow{}}
	var ids []int64
	for rows.Next() {
		var r appStartRow
		var id int64
		if err := rows.Scan(&id, &r.StartID, &r.Type, &r.Platform, &r.AppVersion, &r.Build, &r.DeviceModel, &r.OSVersion,
			&r.FirstScreen, &r.SessionID, &r.DurationMs, &r.Profiled, &r.StartedAt); err != nil {
			return err
		}
		out.Starts = append(out.Starts, r)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(out.Starts) > limit {
		out.Starts, ids = out.Starts[:limit], ids[:limit]
		last := out.Starts[limit-1]
		key := strconv.Itoa(last.DurationMs)
		if sortBy == "recent" {
			key = last.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		out.NextCursor = encodeAppStartCursor(key, ids[limit-1])
	}
	return c.JSON(out)
}

// GetAppStart handles GET /api/v1/projects/:projectID/app-starts/:startID. The start row and its profile
// are independent: either may exist without the other.
func (h *Handler) GetAppStart(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	startID := c.Params("startID")
	ctx, cancel := context.WithTimeout(c.Context(), appStartQueryTimeout)
	defer cancel()

	d := appStartDetail{appStartRow: appStartRow{StartID: startID}, Phases: []ingest.AppStartPhase{}}
	haveRow := true
	var phases []byte
	err := h.db.QueryRow(ctx, `
		SELECT type, platform, app_version, build, device_model, os_version, first_screen, session_id,
		       duration_ms, profiled, started_at, phases, ram_mb, trampoline
		  FROM app_starts WHERE project_id = $1 AND start_id = $2`, projectID, startID).
		Scan(&d.Type, &d.Platform, &d.AppVersion, &d.Build, &d.DeviceModel, &d.OSVersion, &d.FirstScreen, &d.SessionID,
			&d.DurationMs, &d.Profiled, &d.StartedAt, &phases, &d.RAMMB, &d.Trampoline)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		haveRow = false
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(phases, &d.Phases); err != nil || d.Phases == nil {
			d.Phases = []ingest.AppStartPhase{}
		}
	}

	var status string
	var symbolicated []byte
	err = h.db.QueryRow(ctx,
		`SELECT status, symbolicated FROM app_start_profiles WHERE project_id = $1 AND start_id = $2`,
		projectID, startID).Scan(&status, &symbolicated)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if !haveRow {
			return fiber.NewError(fiber.StatusNotFound, "app start not found")
		}
	case err != nil:
		return err
	default:
		d.Profile = &appStartProfile{Status: status}
		if len(symbolicated) > 0 {
			var data appstart.Symbolicated
			if err := json.Unmarshal(symbolicated, &data); err != nil {
				slog.Warn("app start profile unreadable", "project_id", projectID, "start_id", startID, "err", err)
			} else {
				d.Profile.Data = &data
			}
		}
	}
	return c.JSON(d)
}
