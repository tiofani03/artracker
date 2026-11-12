package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/alerts"
	"github.com/tiofani03/artracker/backend/internal/ingest"
	"github.com/tiofani03/artracker/backend/internal/issues"
	"github.com/tiofani03/artracker/backend/internal/symbolication"
)

// AssignedUser is the minimal assignee reference surfaced on issues.
type AssignedUser struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Issue is a grouped crash summary.
type Issue struct {
	ID            int64         `json:"id"`
	ProjectID     int64         `json:"project_id"`
	Fingerprint   string        `json:"fingerprint"`
	Title         string        `json:"title"`
	Type          string        `json:"type"`
	Status        string        `json:"status"`
	FirstSeen     time.Time     `json:"first_seen"`
	LastSeen      time.Time     `json:"last_seen"`
	Count         int64         `json:"count"`
	AssignedTo    *AssignedUser `json:"assigned_to"`
	LatestMessage string        `json:"latest_message"`
}

// IssueEvent is a single crash occurrence belonging to an issue.
type IssueEvent struct {
	ID                 int64           `json:"id"`
	SessionID          string          `json:"session_id"`
	UserID             string          `json:"user_id"`
	Message            string          `json:"message"`
	StackTrace         string          `json:"stack_trace"`
	StackTraceResolved string          `json:"stack_trace_resolved"`
	Breadcrumbs        json.RawMessage `json:"breadcrumbs"`
	DeviceInfo         json.RawMessage `json:"device_info"`
	ReleaseVersion     string          `json:"release_version"`
	CreatedAt          time.Time       `json:"created_at"`
}

// IssueBucket is one value of a breakdown (OS version, device, app version) and
// how many occurrences carried it.
type IssueBucket struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// IssueAffectedUser is one user hit by the issue, with the context of their
// most recent occurrence.
type IssueAffectedUser struct {
	UserID     string    `json:"user_id"`
	Count      int64     `json:"count"`
	LastSeen   time.Time `json:"last_seen"`
	SessionID  string    `json:"session_id"`
	Device     string    `json:"device"`
	OS         string    `json:"os"`
	AppVersion string    `json:"app_version"`
}

// IssueDailyCount is the number of occurrences on one UTC day.
type IssueDailyCount struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

// IssueSummary aggregates every stored occurrence of an issue.
type IssueSummary struct {
	TotalEvents   int64               `json:"total_events"`
	AffectedUsers int64               `json:"affected_users"`
	OSVersions    []IssueBucket       `json:"os_versions"`
	Devices       []IssueBucket       `json:"devices"`
	AppVersions   []IssueBucket       `json:"app_versions"`
	Users         []IssueAffectedUser `json:"users"`
	Daily         []IssueDailyCount   `json:"daily"`
}

// IssueDetail is the full issue plus its occurrences.
type IssueDetail struct {
	Issue
	Summary IssueSummary `json:"summary"`
	Events  []IssueEvent `json:"events"`
}

const issueSelect = `
	SELECT i.id, i.project_id, i.fingerprint, i.title, i.type, i.status, i.first_seen, i.last_seen, i.count,
	       COALESCE(u.id, 0), COALESCE(u.name, ''), COALESCE(u.email, ''),
	       COALESCE((SELECT e.message FROM issue_events e WHERE e.issue_id = i.id ORDER BY e.created_at DESC, e.id DESC LIMIT 1), '')
	FROM issues i
	LEFT JOIN users u ON u.id = i.assigned_to`

func scanIssue(row pgx.Row) (Issue, error) {
	var it Issue
	var assignedID int64
	var assignedName, assignedEmail string
	err := row.Scan(
		&it.ID, &it.ProjectID, &it.Fingerprint, &it.Title, &it.Type, &it.Status,
		&it.FirstSeen, &it.LastSeen, &it.Count,
		&assignedID, &assignedName, &assignedEmail, &it.LatestMessage,
	)
	if err != nil {
		return it, err
	}
	if assignedID != 0 {
		it.AssignedTo = &AssignedUser{ID: assignedID, Name: assignedName, Email: assignedEmail}
	}
	return it, nil
}

var errIssueNotFound = fiber.NewError(fiber.StatusNotFound, "issue not found")

func validIssueStatus(s string) bool {
	switch s {
	case "open", "resolved", "ignored":
		return true
	default:
		return false
	}
}

// ListIssues returns grouped issues for a project with status/search/sort support.
func (h *Handler) ListIssues(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)

	status := strings.ToLower(strings.TrimSpace(c.Query("status", "open")))
	if status != "all" && !validIssueStatus(status) {
		status = "open"
	}
	search := strings.TrimSpace(c.Query("q", ""))
	sort := strings.ToLower(strings.TrimSpace(c.Query("sort", "frequency")))
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	orderBy := "i.count DESC, i.last_seen DESC"
	switch sort {
	case "newest":
		orderBy = "i.last_seen DESC, i.id DESC"
	case "oldest":
		orderBy = "i.first_seen ASC, i.id ASC"
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	query := issueSelect + `
		WHERE i.project_id = $1
		  AND ($2 = 'all' OR i.status = $2)
		  AND ($3 = '' OR i.title ILIKE '%' || $3 || '%' OR i.type ILIKE '%' || $3 || '%')
		ORDER BY ` + orderBy + `
		LIMIT $4`

	rows, err := h.db.Query(ctx, query, projectID, status, search, limit)
	if err != nil {
		return err
	}
	defer rows.Close()

	items := []Issue{}
	for rows.Next() {
		it, err := scanIssue(rows)
		if err != nil {
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items})
}

// GetIssue returns an issue with all its occurrences and demangled stack traces.
func (h *Handler) GetIssue(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	issueID, err := strconv.ParseInt(c.Params("issueID"), 10, 64)
	if err != nil {
		return errIssueNotFound
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	detail, err := h.loadIssueDetail(ctx, projectID, issueID, 200)
	if err != nil {
		return err
	}
	return c.JSON(detail)
}

// loadIssueDetail returns an issue scoped to projectID with its newest
// eventLimit occurrences (stack traces symbolicated) and aggregated summary.
// It returns errIssueNotFound when the issue is not in that project.
func (h *Handler) loadIssueDetail(ctx context.Context, projectID, issueID int64, eventLimit int) (IssueDetail, error) {
	it, err := scanIssue(h.db.QueryRow(ctx, issueSelect+` WHERE i.id = $1 AND i.project_id = $2`, issueID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueDetail{}, errIssueNotFound
	}
	if err != nil {
		return IssueDetail{}, err
	}

	rows, err := h.db.Query(ctx, `
		SELECT id, session_id, user_id, message, stack_trace, breadcrumbs, device_info, release_version, created_at
		FROM issue_events
		WHERE issue_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, issueID, eventLimit)
	if err != nil {
		return IssueDetail{}, err
	}
	defer rows.Close()

	// Load symbolication maps once per release version referenced by these events.
	sourceMaps := map[string][]*issues.SourceMap{}
	proguardMaps := map[string]*symbolication.ProguardRetracer{}
	iosSymbolicators := map[string]*symbolication.IOSSymbolicator{}

	events := []IssueEvent{}
	for rows.Next() {
		var ev IssueEvent
		var breadcrumbs, deviceInfo []byte
		if err := rows.Scan(
			&ev.ID, &ev.SessionID, &ev.UserID, &ev.Message, &ev.StackTrace,
			&breadcrumbs, &deviceInfo, &ev.ReleaseVersion, &ev.CreatedAt,
		); err != nil {
			return IssueDetail{}, err
		}
		if len(breadcrumbs) == 0 {
			breadcrumbs = []byte("[]")
		}
		if len(deviceInfo) == 0 {
			deviceInfo = []byte("{}")
		}
		ev.Breadcrumbs = breadcrumbs
		ev.DeviceInfo = deviceInfo
		ev.StackTraceResolved = ev.StackTrace

		if ev.StackTrace != "" {
			// 1. Web JS Source Maps
			maps, ok := sourceMaps[ev.ReleaseVersion]
			if !ok {
				maps = h.loadSourceMaps(ctx, projectID, ev.ReleaseVersion)
				sourceMaps[ev.ReleaseVersion] = maps
			}
			for _, sm := range maps {
				ev.StackTraceResolved = sm.DemangleStackTrace(ev.StackTraceResolved)
			}

			// 2. Android ProGuard / R8 Mapping
			pg, ok := proguardMaps[ev.ReleaseVersion]
			if !ok {
				pg = h.loadProguardRetracer(ctx, projectID, ev.ReleaseVersion)
				proguardMaps[ev.ReleaseVersion] = pg
			}
			if pg != nil {
				ev.StackTraceResolved = pg.RetraceStackTrace(ev.StackTraceResolved)
			}

			// 3. iOS dSYM / DWARF Symbolicator
			iosSym, ok := iosSymbolicators[ev.ReleaseVersion]
			if !ok {
				iosSym = h.loadIOSSymbolicator(ctx, projectID, ev.ReleaseVersion)
				iosSymbolicators[ev.ReleaseVersion] = iosSym
			}
			if iosSym != nil {
				ev.StackTraceResolved = symbolication.DemangleIOSStackTrace(ev.StackTraceResolved, iosSym, 0)
			}
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return IssueDetail{}, err
	}

	summary, err := h.loadIssueSummary(ctx, issueID)
	if err != nil {
		return IssueDetail{}, err
	}

	return IssueDetail{Issue: it, Summary: summary, Events: events}, nil
}

// issueEventContext normalises the free-form device_info of every occurrence
// into display labels. SDKs disagree on key names, so each label falls back
// through the common spellings and ends at 'Unknown'.
const issueEventContext = `
	WITH ev AS (
		SELECT e.user_id, e.session_id, e.created_at,
		       COALESCE(NULLIF(CASE
		           WHEN COALESCE(d.os_ver, '') = '' THEN d.os
		           WHEN COALESCE(d.os, '') = '' THEN d.os_ver
		           WHEN position(d.os_ver IN d.os) > 0 THEN d.os
		           ELSE d.os || ' ' || d.os_ver
		       END, ''), 'Unknown') AS os,
		       COALESCE(NULLIF(concat_ws(' ', NULLIF(d.maker, ''), NULLIF(d.model, '')), ''), 'Unknown') AS device,
		       COALESCE(NULLIF(e.release_version, ''), NULLIF(d.app_ver, ''), 'Unknown') AS app
		FROM issue_events e
		CROSS JOIN LATERAL (SELECT
		    NULLIF(e.device_info->>'os', '') AS os,
		    COALESCE(e.device_info->>'os_version', e.device_info->>'osVersion') AS os_ver,
		    COALESCE(e.device_info->>'manufacturer', e.device_info->>'brand') AS maker,
		    COALESCE(e.device_info->>'model', e.device_info->>'device_model',
		             CASE WHEN jsonb_typeof(e.device_info->'device') = 'string' THEN e.device_info->>'device' END) AS model,
		    COALESCE(e.device_info->>'app_version', e.device_info->>'appVersion') AS app_ver
		) d
		WHERE e.issue_id = $1
	)`

const issueBreakdownLimit = 10

// loadIssueSummary aggregates all stored occurrences of an issue: totals, the
// OS / device / app-version breakdowns, the most affected users and a 14-day trend.
func (h *Handler) loadIssueSummary(ctx context.Context, issueID int64) (IssueSummary, error) {
	s := IssueSummary{
		OSVersions:  []IssueBucket{},
		Devices:     []IssueBucket{},
		AppVersions: []IssueBucket{},
		Users:       []IssueAffectedUser{},
		Daily:       []IssueDailyCount{},
	}

	if err := h.db.QueryRow(ctx, issueEventContext+`
		SELECT count(*), count(DISTINCT NULLIF(user_id, '')) FROM ev`, issueID,
	).Scan(&s.TotalEvents, &s.AffectedUsers); err != nil {
		return s, err
	}

	for _, b := range []struct {
		column string
		dst    *[]IssueBucket
	}{{"os", &s.OSVersions}, {"device", &s.Devices}, {"app", &s.AppVersions}} {
		rows, err := h.db.Query(ctx, issueEventContext+`
			SELECT `+b.column+`, count(*) FROM ev
			GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT $2`, issueID, issueBreakdownLimit)
		if err != nil {
			return s, err
		}
		buckets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (IssueBucket, error) {
			var bk IssueBucket
			return bk, r.Scan(&bk.Value, &bk.Count)
		})
		if err != nil {
			return s, err
		}
		*b.dst = buckets
	}

	rows, err := h.db.Query(ctx, issueEventContext+`
		SELECT user_id, count(*), max(created_at),
		       (array_agg(session_id ORDER BY created_at DESC))[1],
		       (array_agg(device ORDER BY created_at DESC))[1],
		       (array_agg(os ORDER BY created_at DESC))[1],
		       (array_agg(app ORDER BY created_at DESC))[1]
		FROM ev WHERE user_id <> ''
		GROUP BY user_id ORDER BY 2 DESC, 3 DESC LIMIT 50`, issueID)
	if err != nil {
		return s, err
	}
	s.Users, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (IssueAffectedUser, error) {
		var u IssueAffectedUser
		return u, r.Scan(&u.UserID, &u.Count, &u.LastSeen, &u.SessionID, &u.Device, &u.OS, &u.AppVersion)
	})
	if err != nil {
		return s, err
	}

	rows, err = h.db.Query(ctx, `
		SELECT to_char(day_local, 'YYYY-MM-DD'),
		       (SELECT count(*) FROM issue_events e
		        WHERE e.issue_id = $1
		          AND e.created_at >= day AND e.created_at < day + interval '1 day')
		FROM generate_series(
		    date_trunc('day', now() AT TIME ZONE 'UTC') - interval '13 days',
		    date_trunc('day', now() AT TIME ZONE 'UTC'),
		    interval '1 day') AS g(day_local)
		CROSS JOIN LATERAL (SELECT day_local AT TIME ZONE 'UTC' AS day) d
		ORDER BY day`, issueID)
	if err != nil {
		return s, err
	}
	s.Daily, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (IssueDailyCount, error) {
		var d IssueDailyCount
		return d, r.Scan(&d.Day, &d.Count)
	})
	return s, err
}

func (h *Handler) loadProguardRetracer(ctx context.Context, projectID int64, releaseVersion string) *symbolication.ProguardRetracer {
	if releaseVersion == "" {
		return nil
	}
	var content []byte
	err := h.db.QueryRow(ctx, `
		SELECT content FROM project_release_artifacts
		WHERE project_id = $1 AND version = $2 AND artifact_type = 'proguard_mapping'
		ORDER BY created_at DESC LIMIT 1`, projectID, releaseVersion).Scan(&content)
	if err != nil || len(content) == 0 {
		return nil
	}
	retracer, err := symbolication.ParseProguardMapping(content)
	if err != nil {
		slog.Warn("skipping invalid proguard mapping", "err", err, "version", releaseVersion)
		return nil
	}
	return retracer
}

func (h *Handler) loadIOSSymbolicator(ctx context.Context, projectID int64, releaseVersion string) *symbolication.IOSSymbolicator {
	if releaseVersion == "" {
		return nil
	}
	var content []byte
	err := h.db.QueryRow(ctx, `
		SELECT content FROM project_release_artifacts
		WHERE project_id = $1 AND version = $2 AND artifact_type = 'dsym'
		ORDER BY created_at DESC LIMIT 1`, projectID, releaseVersion).Scan(&content)
	if err != nil || len(content) == 0 {
		return nil
	}
	sym, err := symbolication.NewIOSSymbolicator(content)
	if err != nil {
		slog.Warn("skipping invalid dsym macho binary", "err", err, "version", releaseVersion)
		return nil
	}
	return sym
}

func (h *Handler) loadSourceMaps(ctx context.Context, projectID int64, releaseVersion string) []*issues.SourceMap {
	if releaseVersion == "" {
		return nil
	}
	rows, err := h.db.Query(ctx, `SELECT content FROM source_maps WHERE project_id = $1 AND release_version = $2`, projectID, releaseVersion)
	if err != nil {
		slog.Warn("load source maps failed", "err", err)
		return nil
	}
	defer rows.Close()

	var maps []*issues.SourceMap
	for rows.Next() {
		var content []byte
		if err := rows.Scan(&content); err != nil {
			continue
		}
		sm, err := issues.ParseSourceMap(content)
		if err != nil {
			slog.Warn("skipping invalid source map", "err", err)
			continue
		}
		maps = append(maps, sm)
	}
	return maps
}

// UpdateIssueStatus resolves, ignores, or reopens an issue.
func (h *Handler) UpdateIssueStatus(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	issueID, err := strconv.ParseInt(c.Params("issueID"), 10, 64)
	if err != nil {
		return errIssueNotFound
	}

	var in struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	it, err := h.setIssueStatus(ctx, projectID, issueID, in.Status)
	if err != nil {
		return err
	}
	return c.JSON(it)
}

// setIssueStatus validates and applies a status change to an issue in
// projectID, closing its firing incidents when resolved or ignored, in one
// transaction. It returns errIssueNotFound when the issue is not in that project.
func (h *Handler) setIssueStatus(ctx context.Context, projectID, issueID int64, status string) (Issue, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if !validIssueStatus(status) {
		return Issue{}, fiber.NewError(fiber.StatusBadRequest, "status must be open, resolved or ignored")
	}

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return Issue{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	it, err := scanIssue(tx.QueryRow(ctx, `
		UPDATE issues SET status = $1
		WHERE id = $2 AND project_id = $3
		RETURNING id, project_id, fingerprint, title, type, status, first_seen, last_seen, count,
		          COALESCE((SELECT u.id FROM users u WHERE u.id = issues.assigned_to), 0),
		          COALESCE((SELECT u.name FROM users u WHERE u.id = issues.assigned_to), ''),
		          COALESCE((SELECT u.email FROM users u WHERE u.id = issues.assigned_to), ''),
		          COALESCE((SELECT e.message FROM issue_events e WHERE e.issue_id = issues.id ORDER BY e.created_at DESC, e.id DESC LIMIT 1), '')`,
		status, issueID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, errIssueNotFound
	}
	if err != nil {
		return Issue{}, err
	}
	if status == "resolved" || status == "ignored" {
		if err := closeIssueIncidents(ctx, tx, projectID, issueID); err != nil {
			return Issue{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Issue{}, err
	}
	return it, nil
}

// closeIssueIncidents silently closes the issue's firing new_issue and
// regression incidents, so the next regression opens a fresh incident instead
// of being absorbed by a stale one. Spike incidents are left to the evaluator.
func closeIssueIncidents(ctx context.Context, tx pgx.Tx, projectID, issueID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE alert_incidents
		   SET status = 'resolved', resolved_at = now(),
		       notify_state = CASE WHEN notify_state = 'pending' THEN 'skipped' ELSE notify_state END
		 WHERE project_id = $1 AND issue_id = $2 AND status = 'firing'
		   AND rule_kind IN ('new_issue', 'regression')`,
		projectID, issueID)
	return err
}

// AssignIssue assigns the issue to the current authenticated user (or a
// specific user id passed in the body).
func (h *Handler) AssignIssue(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	issueID, err := strconv.ParseInt(c.Params("issueID"), 10, 64)
	if err != nil {
		return errIssueNotFound
	}

	var in struct {
		UserID *int64 `json:"user_id"`
	}
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}

	assignee := userID(c)
	if in.UserID != nil && *in.UserID > 0 {
		assignee = *in.UserID
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	it, err := scanIssue(h.db.QueryRow(ctx, `
		UPDATE issues SET assigned_to = $1
		WHERE id = $2 AND project_id = $3
		RETURNING id, project_id, fingerprint, title, type, status, first_seen, last_seen, count,
		          COALESCE((SELECT u.id FROM users u WHERE u.id = issues.assigned_to), 0),
		          COALESCE((SELECT u.name FROM users u WHERE u.id = issues.assigned_to), ''),
		          COALESCE((SELECT u.email FROM users u WHERE u.id = issues.assigned_to), ''),
		          COALESCE((SELECT e.message FROM issue_events e WHERE e.issue_id = issues.id ORDER BY e.created_at DESC, e.id DESC LIMIT 1), '')`,
		assignee, issueID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return errIssueNotFound
	}
	if err != nil {
		return err
	}
	return c.JSON(it)
}

// UploadSourceMap stores a Source Map v3 document for a release + output file.
func (h *Handler) UploadSourceMap(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	version := strings.TrimSpace(c.Params("version"))
	if version == "" {
		return fiber.NewError(fiber.StatusBadRequest, "release version is required")
	}

	body := c.Body()
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "source map body is required")
	}

	sm, err := issues.ParseSourceMap(body)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid source map: "+err.Error())
	}

	fileName := strings.TrimSpace(c.Query("file"))
	if fileName == "" {
		fileName = strings.TrimSpace(sm.File)
	}
	if fileName == "" {
		return fiber.NewError(fiber.StatusBadRequest, "file name is required (source map has no 'file' field)")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if _, err := h.db.Exec(ctx, `
		INSERT INTO source_maps(project_id, release_version, file_name, content)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, release_version, file_name)
		DO UPDATE SET content = EXCLUDED.content, created_at = now()`,
		projectID, version, fileName, body); err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"status":          "stored",
		"release_version": version,
		"file_name":       fileName,
		"sources":         len(sm.Sources),
	})
}

// IssueService groups error telemetry rows into issues. It implements
// ingest.IssueProcessor and runs inside the ingest worker pool.
type IssueService struct {
	db     *pgxpool.Pool
	alerts IssueAlerter
}

// IssueAlerter receives a signal when an issue is new or has regressed.
type IssueAlerter interface {
	OnIssue(ctx context.Context, sig alerts.IssueSignal) error
}

// WithAlerter sets the alerter that is told about new and regressed issues.
func (s *IssueService) WithAlerter(a IssueAlerter) *IssueService {
	s.alerts = a
	return s
}

func (h *Handler) IssueService() *IssueService {
	if h.db == nil {
		return &IssueService{}
	}
	svc := &IssueService{db: h.db}
	if h.alertsSvc != nil { // never store a typed-nil pointer in the interface
		svc.WithAlerter(h.alertsSvc)
	}
	return svc
}

// ProcessRows upserts issues and records occurrences for error/fatal logs.
func (s *IssueService) ProcessRows(ctx context.Context, rows []ingest.TelemetryRow) error {
	if s.db == nil {
		return nil
	}
	for i := range rows {
		r := &rows[i]
		if r.Kind != "log" || (r.Level != "error" && r.Level != "fatal") {
			continue
		}
		if err := s.recordError(ctx, *r); err != nil {
			// A single bad row must not break grouping for the rest of the batch.
			slog.Error("record issue failed", "err", err, "project", r.ProjectID)
		}
	}
	return nil
}

func (s *IssueService) recordError(ctx context.Context, r ingest.TelemetryRow) error {
	message, stackTrace, breadcrumbs, deviceInfo := extractErrorInfo(r)

	errType := issues.ErrorType(message)
	fp := issues.Fingerprint(stackTrace, message, errType)
	title := issues.Title(message)

	var (
		issueID    int64
		inserted   bool
		prevStatus string
	)
	err := s.db.QueryRow(ctx, `
		WITH prev AS (SELECT status FROM issues WHERE project_id = $1 AND fingerprint = $2),
		up AS (
		  INSERT INTO issues(project_id, fingerprint, title, type, status, first_seen, last_seen, count)
		  VALUES ($1, $2, $3, $4, 'open', now(), now(), 1)
		  ON CONFLICT (project_id, fingerprint) DO UPDATE
		    SET last_seen = now(), count = issues.count + 1,
		        status = CASE WHEN issues.status = 'resolved' THEN 'open' ELSE issues.status END
		  RETURNING id, (xmax = 0) AS inserted)
		SELECT up.id, up.inserted, COALESCE((SELECT status FROM prev), '') FROM up`,
		r.ProjectID, fp, title, errType).Scan(&issueID, &inserted, &prevStatus)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO issue_events(issue_id, session_id, user_id, stack_trace, message, breadcrumbs, device_info, release_version, environment)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		issueID, r.SessionID, r.UserID, stackTrace, message, breadcrumbs, deviceInfo, r.ReleaseVersion, r.Environment)
	if err != nil {
		return err
	}

	if s.alerts != nil {
		var kind alerts.RuleKind
		switch {
		case inserted:
			kind = alerts.KindNewIssue
		case prevStatus == "resolved":
			kind = alerts.KindRegression
		}
		if kind != "" {
			sig := alerts.IssueSignal{
				ProjectID: r.ProjectID, IssueID: issueID, Kind: kind, Title: title,
				Environment: r.Environment, ReleaseVersion: r.ReleaseVersion, SessionID: r.SessionID,
			}
			if aerr := s.alerts.OnIssue(ctx, sig); aerr != nil {
				slog.Error("issue alert failed", "err", aerr, "project", r.ProjectID, "issue", issueID)
			}
		}
	}
	return nil
}

// extractErrorInfo pulls the crash payload out of a telemetry row's attributes,
// falling back to safe defaults when keys are absent.
func extractErrorInfo(r ingest.TelemetryRow) (message, stackTrace string, breadcrumbs, deviceInfo []byte) {
	message = r.Message

	var attrs map[string]json.RawMessage
	if len(r.Attributes) > 0 && json.Unmarshal(r.Attributes, &attrs) == nil {
		for _, key := range []string{"stack_trace", "stack", "stacktrace"} {
			if v, ok := attrs[key]; ok {
				if json.Unmarshal(v, &stackTrace) == nil && stackTrace != "" {
					break
				}
			}
		}
		if v, ok := attrs["breadcrumbs"]; ok {
			breadcrumbs = v
		}
		for _, key := range []string{"device_info", "device", "context"} {
			if v, ok := attrs[key]; ok {
				deviceInfo = v
				break
			}
		}
	}
	if len(breadcrumbs) == 0 {
		breadcrumbs = []byte("[]")
	}
	if len(deviceInfo) == 0 {
		deviceInfo = []byte("{}")
	}
	return message, stackTrace, breadcrumbs, deviceInfo
}
