package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/alerts"
	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

type issueEnv struct {
	app       *fiber.App
	h         *Handler
	pool      *pgxpool.Pool
	projectID int64
	userID    int64
}

// newIssueEnv creates a project + owner and an app whose middleware injects the
// project/user locals (mirroring RequireProjectRole + RequireAuth).
func newIssueEnv(t *testing.T) *issueEnv {
	t.Helper()
	pool := testDB(t)
	ctx := context.Background()

	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users(email, sso_provider, sso_id) VALUES ('dev@example.com','google','g-issues') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var orgID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO organizations(name, slug) VALUES ('Acme','acme-issues') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(org_id, user_id, role) VALUES ($1,$2,'owner')`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	var projectID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'Checkout Web','checkout-web-issues','web') RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{Env: "development", JWTSecret: testSecret}
	h := New(pool, cfg, auth.NewIssuer(testSecret, time.Hour), map[string]OAuthProvider{})

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Use("/api/v1/projects/:projectID/*", func(c *fiber.Ctx) error {
		c.Locals("projectID", projectID)
		c.Locals("userID", userID)
		return c.Next()
	})

	api := app.Group("/api/v1")
	api.Get("/projects/:projectID/issues", h.ListIssues)
	api.Get("/projects/:projectID/issues/:issueID", h.GetIssue)
	api.Patch("/projects/:projectID/issues/:issueID", h.UpdateIssueStatus)
	api.Post("/projects/:projectID/issues/:issueID/assign", h.AssignIssue)
	api.Post("/projects/:projectID/releases/:version/sourcemaps", h.UploadSourceMap)

	return &issueEnv{app: app, h: h, pool: pool, projectID: projectID, userID: userID}
}

func errorRow(projectID int64, message, stack string) ingest.TelemetryRow {
	attrs, _ := json.Marshal(map[string]any{
		"stack_trace": stack,
		"breadcrumbs": []map[string]any{{"type": "navigation", "message": "/checkout"}},
		"device_info": map[string]any{"os": "macOS", "browser": "Chrome 131"},
	})
	return ingest.TelemetryRow{
		ProjectID:      projectID,
		Kind:           "log",
		Level:          "error",
		Message:        message,
		SessionID:      "sess_1",
		UserID:         "usr_1",
		ReleaseVersion: "1.2.3",
		EventTimestamp: time.Now(),
		Attributes:     attrs,
	}
}

func TestExtractErrorInfo(t *testing.T) {
	r := ingest.TelemetryRow{
		Message: "TypeError: Cannot read properties of undefined (reading 'charge_id')",
		Attributes: json.RawMessage(`{
			"stack_trace": "at handleSubmit (app.min.js:1:240)",
			"breadcrumbs": [{"type":"navigation","message":"/checkout"}],
			"device_info": {"os":"macOS","browser":"Chrome 131"}
		}`),
	}

	message, stackTrace, breadcrumbs, deviceInfo := extractErrorInfo(r)
	if message != r.Message {
		t.Fatalf("message = %q", message)
	}
	if stackTrace != "at handleSubmit (app.min.js:1:240)" {
		t.Fatalf("stackTrace = %q", stackTrace)
	}
	if !strings.Contains(string(breadcrumbs), "navigation") || !strings.Contains(string(deviceInfo), "macOS") {
		t.Fatalf("unexpected breadcrumbs/device: %s / %s", breadcrumbs, deviceInfo)
	}
}

func TestExtractErrorInfoDefaults(t *testing.T) {
	_, stackTrace, breadcrumbs, deviceInfo := extractErrorInfo(ingest.TelemetryRow{Message: "plain error"})
	if stackTrace != "" {
		t.Fatalf("stackTrace = %q, want empty", stackTrace)
	}
	if string(breadcrumbs) != "[]" || string(deviceInfo) != "{}" {
		t.Fatalf("defaults = %s / %s", breadcrumbs, deviceInfo)
	}
}

func TestIssueServiceGroupsRepeatedErrors(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	var userID int64
	_ = pool.QueryRow(ctx, `INSERT INTO users(email, sso_provider, sso_id) VALUES ('a@b.c','google','g-1') RETURNING id`).Scan(&userID)
	var orgID int64
	_ = pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('O','o') RETURNING id`).Scan(&orgID)
	_, _ = pool.Exec(ctx, `INSERT INTO organization_members(org_id, user_id, role) VALUES ($1,$2,'owner')`, orgID, userID)
	var projectID int64
	_ = pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'P','p','web') RETURNING id`, orgID).Scan(&projectID)

	svc := &IssueService{db: pool}
	stack := "TypeError: Cannot read properties of undefined (reading 'charge_id')\n    at handleSubmit (app.min.js:1:240)"
	rows := []ingest.TelemetryRow{
		errorRow(projectID, "TypeError: Cannot read properties of undefined (reading 'charge_id')", stack),
		errorRow(projectID, "TypeError: Cannot read properties of undefined (reading 'charge_id')", stack),
		errorRow(projectID, "ReferenceError: x is not defined", "at foo (app.min.js:2:100)"),
	}
	if err := svc.ProcessRows(ctx, rows); err != nil {
		t.Fatal(err)
	}

	var issueCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE project_id = $1`, projectID).Scan(&issueCount); err != nil {
		t.Fatal(err)
	}
	if issueCount != 2 {
		t.Fatalf("issues = %d, want 2 (repeated errors must group)", issueCount)
	}

	var repeatedCount int64
	if err := pool.QueryRow(ctx,
		`SELECT count FROM issues WHERE project_id = $1 AND title LIKE 'TypeError%'`, projectID).Scan(&repeatedCount); err != nil {
		t.Fatal(err)
	}
	if repeatedCount != 2 {
		t.Fatalf("repeated issue count = %d, want 2", repeatedCount)
	}

	var events int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM issue_events`).Scan(&events)
	if events != 3 {
		t.Fatalf("issue_events = %d, want 3", events)
	}
}

func TestListIssuesEndpoint(t *testing.T) {
	env := newIssueEnv(t)
	ctx := context.Background()

	svc := &IssueService{db: env.pool}
	stack := "TypeError: Cannot read properties of undefined (reading 'x')\n    at handleSubmit (app.min.js:1:1)"
	if err := svc.ProcessRows(ctx, []ingest.TelemetryRow{
		errorRow(env.projectID, "TypeError: Cannot read properties of undefined (reading 'x')", stack),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/v1/projects/999/issues?status=all", nil)
	resp, err := env.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Items []Issue `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Items))
	}
	if out.Items[0].Status != "open" || out.Items[0].Type != "TypeError" {
		t.Fatalf("unexpected issue: %+v", out.Items[0])
	}

	// Search filter should match by error message.
	req = httptest.NewRequest("GET", "/api/v1/projects/999/issues?status=all&q=reading+%27x%27", nil)
	resp, _ = env.app.Test(req, -1)
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Items) != 1 {
		t.Fatalf("search items = %d, want 1", len(out.Items))
	}
}

func TestIssueDetailStatusAssignAndDemangle(t *testing.T) {
	env := newIssueEnv(t)
	ctx := context.Background()

	svc := &IssueService{db: env.pool}
	stack := "TypeError: boom\n    at handleSubmit (app.min.js:1:1)"
	if err := svc.ProcessRows(ctx, []ingest.TelemetryRow{
		errorRow(env.projectID, "TypeError: boom", stack),
	}); err != nil {
		t.Fatal(err)
	}

	var issueID int64
	if err := env.pool.QueryRow(ctx, `SELECT id FROM issues WHERE project_id = $1`, env.projectID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}

	// Without a source map, the resolved trace matches the raw one.
	var detail IssueDetail
	resp, err := env.app.Test(httptest.NewRequest("GET", "/api/v1/projects/999/issues/"+strconv.FormatInt(issueID, 10), nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 1 || detail.Events[0].StackTraceResolved != stack {
		t.Fatalf("unexpected detail: %+v", detail.Events)
	}

	// Upload a source map for release 1.2.3, then the trace must demangle.
	smBody := `{"version":3,"file":"app.min.js","sources":["../src/PaymentForm.tsx"],"names":["handleSubmit"],"mappings":"AAAAA"}`
	resp, err = env.app.Test(httptest.NewRequest("POST", "/api/v1/projects/999/releases/1.2.3/sourcemaps", strings.NewReader(smBody)), -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("sourcemap upload status = %d, want 201", resp.StatusCode)
	}

	resp, _ = env.app.Test(httptest.NewRequest("GET", "/api/v1/projects/999/issues/"+strconv.FormatInt(issueID, 10), nil), -1)
	detail = IssueDetail{}
	_ = json.NewDecoder(resp.Body).Decode(&detail)
	if !strings.Contains(detail.Events[0].StackTraceResolved, "../src/PaymentForm.tsx:1:1") {
		t.Fatalf("stack trace not demangled: %q", detail.Events[0].StackTraceResolved)
	}

	// Mark as resolved.
	req := httptest.NewRequest("PATCH", "/api/v1/projects/999/issues/"+strconv.FormatInt(issueID, 10), strings.NewReader(`{"status":"resolved"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = env.app.Test(req, -1)
	var updated Issue
	_ = json.NewDecoder(resp.Body).Decode(&updated)
	if updated.Status != "resolved" {
		t.Fatalf("status = %q, want resolved", updated.Status)
	}

	// Assign to the current user.
	req = httptest.NewRequest("POST", "/api/v1/projects/999/issues/"+strconv.FormatInt(issueID, 10)+"/assign", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = env.app.Test(req, -1)
	var assigned Issue
	_ = json.NewDecoder(resp.Body).Decode(&assigned)
	if assigned.AssignedTo == nil || assigned.AssignedTo.ID != env.userID {
		t.Fatalf("assigned_to = %+v, want user %d", assigned.AssignedTo, env.userID)
	}
}

type fakeAlerter struct{ sigs []alerts.IssueSignal }

func (f *fakeAlerter) OnIssue(_ context.Context, sig alerts.IssueSignal) error {
	f.sigs = append(f.sigs, sig)
	return nil
}

func signalRow(projectID int64) ingest.TelemetryRow {
	r := errorRow(projectID, "TypeError: boom", "at foo (app.min.js:1:1)")
	r.Environment = "production"
	return r
}

func TestIssueSignalsNewOnce(t *testing.T) {
	env := newIssueEnv(t)
	fa := &fakeAlerter{}
	svc := (&IssueService{db: env.pool}).WithAlerter(fa)
	rows := []ingest.TelemetryRow{signalRow(env.projectID), signalRow(env.projectID)}
	if err := svc.ProcessRows(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	if len(fa.sigs) != 1 {
		t.Fatalf("signals = %d, want 1", len(fa.sigs))
	}
	s := fa.sigs[0]
	if s.Kind != alerts.KindNewIssue || s.Title != "TypeError: boom" || s.Environment != "production" ||
		s.ReleaseVersion != "1.2.3" || s.SessionID != "sess_1" || s.ProjectID != env.projectID || s.IssueID == 0 {
		t.Fatalf("signal = %+v", s)
	}
}

func TestIssueRegressionReopens(t *testing.T) {
	env := newIssueEnv(t)
	ctx := context.Background()
	fa := &fakeAlerter{}
	svc := (&IssueService{db: env.pool}).WithAlerter(fa)
	if err := svc.ProcessRows(ctx, []ingest.TelemetryRow{signalRow(env.projectID)}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE issues SET status='resolved' WHERE project_id=$1`, env.projectID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessRows(ctx, []ingest.TelemetryRow{signalRow(env.projectID)}); err != nil {
		t.Fatal(err)
	}
	if len(fa.sigs) != 2 || fa.sigs[1].Kind != alerts.KindRegression {
		t.Fatalf("signals = %+v", fa.sigs)
	}
	var status string
	_ = env.pool.QueryRow(ctx, `SELECT status FROM issues WHERE project_id=$1`, env.projectID).Scan(&status)
	if status != "open" {
		t.Fatalf("status = %s, want open", status)
	}
}

func TestIgnoredIssueStaysSilent(t *testing.T) {
	env := newIssueEnv(t)
	ctx := context.Background()
	fa := &fakeAlerter{}
	svc := (&IssueService{db: env.pool}).WithAlerter(fa)
	if err := svc.ProcessRows(ctx, []ingest.TelemetryRow{signalRow(env.projectID)}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE issues SET status='ignored' WHERE project_id=$1`, env.projectID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessRows(ctx, []ingest.TelemetryRow{signalRow(env.projectID)}); err != nil {
		t.Fatal(err)
	}
	if len(fa.sigs) != 1 {
		t.Fatalf("signals = %d, want only the initial new_issue", len(fa.sigs))
	}
	var status string
	_ = env.pool.QueryRow(ctx, `SELECT status FROM issues WHERE project_id=$1`, env.projectID).Scan(&status)
	if status != "ignored" {
		t.Fatalf("status = %s, want ignored", status)
	}
}

func TestIssueServiceWithoutAlerterStillGroups(t *testing.T) {
	env := newIssueEnv(t)
	cfg := config.Config{Env: "development", JWTSecret: testSecret}
	svc := New(env.pool, cfg, auth.NewIssuer(testSecret, time.Hour), map[string]OAuthProvider{}).IssueService()
	rows := []ingest.TelemetryRow{signalRow(env.projectID), signalRow(env.projectID)}
	if err := svc.ProcessRows(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := env.pool.QueryRow(context.Background(), `SELECT count FROM issues WHERE project_id=$1`, env.projectID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestIssueEventEnvironmentStored(t *testing.T) {
	env := newIssueEnv(t)
	ctx := context.Background()
	svc := &IssueService{db: env.pool}
	r := signalRow(env.projectID)
	r.Environment = "staging"
	if err := svc.ProcessRows(ctx, []ingest.TelemetryRow{r}); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := env.pool.QueryRow(ctx, `SELECT e.environment FROM issue_events e JOIN issues i ON i.id=e.issue_id WHERE i.project_id=$1`, env.projectID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "staging" {
		t.Fatalf("environment = %q", got)
	}
}

func TestIssueServiceWiresAlerterOnlyWhenConfigured(t *testing.T) {
	env := newIssueEnv(t)
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, alerts.SeedRulesSQL, env.projectID); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Env: "development", JWTSecret: testSecret}
	h := New(env.pool, cfg, auth.NewIssuer(testSecret, time.Hour), map[string]OAuthProvider{})

	plain := h.IssueService()
	if plain.alerts != nil {
		t.Fatalf("alerter = %#v, want nil interface without WithAlerts", plain.alerts)
	}
	if err := plain.ProcessRows(ctx, []ingest.TelemetryRow{signalRow(env.projectID)}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM alert_incidents WHERE project_id=$1`, env.projectID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("incidents without alerter = %d (err %v)", n, err)
	}

	if got := h.WithAlerts(alerts.NewService(env.pool, nil, "http://app")); got != h {
		t.Fatal("WithAlerts must return the receiver")
	}
	wired := h.IssueService()
	if wired.alerts == nil {
		t.Fatal("alerter not wired after WithAlerts")
	}
	row := errorRow(env.projectID, "RangeError: other", "at bar (app.min.js:2:2)")
	row.Environment = "production"
	if err := wired.ProcessRows(ctx, []ingest.TelemetryRow{row}); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM alert_incidents WHERE project_id=$1 AND status='firing'`, env.projectID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("firing incidents = %d (err %v), want 1", n, err)
	}
}

// newAlertingIssueEnv wires a real alerts.Service (no notifier) into the
// issue env and seeds the project's default rules, so ingestion really opens
// incidents through OnIssue.
func newAlertingIssueEnv(t *testing.T) *issueEnv {
	t.Helper()
	env := newIssueEnv(t)
	if _, err := env.pool.Exec(context.Background(), alerts.SeedRulesSQL, env.projectID); err != nil {
		t.Fatal(err)
	}
	env.h.WithAlerts(alerts.NewService(env.pool, nil, "http://app"))
	return env
}

func (e *issueEnv) ingestSignal(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	if err := e.h.IssueService().ProcessRows(ctx, []ingest.TelemetryRow{signalRow(e.projectID)}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := e.pool.QueryRow(ctx, `SELECT id FROM issues WHERE project_id = $1`, e.projectID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *issueEnv) patchStatus(t *testing.T, issueID int64, status string) {
	t.Helper()
	req := httptest.NewRequest("PATCH", "/api/v1/projects/999/issues/"+strconv.FormatInt(issueID, 10),
		strings.NewReader(`{"status":"`+status+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("PATCH %s status = %d, want 200", status, resp.StatusCode)
	}
	var out Issue
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ID != issueID || out.Status != status {
		t.Fatalf("PATCH response = %+v, want id %d status %s", out, issueID, status)
	}
}

type incidentRow struct {
	kind, status, notify, recovery string
	resolvedAt                     *time.Time
}

func incidentsOf(t *testing.T, pool *pgxpool.Pool, projectID, issueID int64) []incidentRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
SELECT rule_kind, status, notify_state, recovery_notify_state, resolved_at
FROM alert_incidents WHERE project_id = $1 AND issue_id = $2 ORDER BY id`, projectID, issueID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []incidentRow
	for rows.Next() {
		var r incidentRow
		if err := rows.Scan(&r.kind, &r.status, &r.notify, &r.recovery, &r.resolvedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestResolveIssueClosesNewIssueIncident(t *testing.T) {
	env := newAlertingIssueEnv(t)
	issueID := env.ingestSignal(t)
	got := incidentsOf(t, env.pool, env.projectID, issueID)
	if len(got) != 1 || got[0].kind != "new_issue" || got[0].status != "firing" || got[0].notify != "pending" {
		t.Fatalf("before resolve = %+v, want one pending firing new_issue", got)
	}

	env.patchStatus(t, issueID, "resolved")

	got = incidentsOf(t, env.pool, env.projectID, issueID)
	if len(got) != 1 || got[0].status != "resolved" || got[0].resolvedAt == nil ||
		got[0].notify != "skipped" || got[0].recovery != "none" {
		t.Fatalf("after resolve = %+v, want resolved, notify skipped, recovery none", got)
	}
}

func TestResolveIssueKeepsSentNotifyState(t *testing.T) {
	env := newAlertingIssueEnv(t)
	issueID := env.ingestSignal(t)
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE alert_incidents SET notify_state = 'sent' WHERE issue_id = $1`, issueID); err != nil {
		t.Fatal(err)
	}
	env.patchStatus(t, issueID, "resolved")
	got := incidentsOf(t, env.pool, env.projectID, issueID)
	if len(got) != 1 || got[0].status != "resolved" || got[0].notify != "sent" {
		t.Fatalf("after resolve = %+v, want resolved with notify sent kept", got)
	}
}

func TestRepeatedRegressionAfterResolveIsReported(t *testing.T) {
	env := newAlertingIssueEnv(t)
	issueID := env.ingestSignal(t)
	env.patchStatus(t, issueID, "resolved")
	env.ingestSignal(t) // regression R1
	env.patchStatus(t, issueID, "resolved")
	env.ingestSignal(t) // regression R2 must not be absorbed by a stale R1

	var firing, resolved int
	for _, r := range incidentsOf(t, env.pool, env.projectID, issueID) {
		if r.kind != "regression" {
			continue
		}
		switch r.status {
		case "firing":
			firing++
		case "resolved":
			resolved++
		}
	}
	if firing != 1 || resolved != 1 {
		t.Fatalf("regression incidents firing=%d resolved=%d, want 1 and 1", firing, resolved)
	}
}

func TestIgnoreIssueClosesFiringIncidents(t *testing.T) {
	env := newAlertingIssueEnv(t)
	issueID := env.ingestSignal(t)
	env.patchStatus(t, issueID, "ignored")
	got := incidentsOf(t, env.pool, env.projectID, issueID)
	if len(got) != 1 || got[0].status != "resolved" || got[0].notify != "skipped" {
		t.Fatalf("after ignore = %+v, want resolved and skipped", got)
	}
}

func TestReopenIssueLeavesIncidents(t *testing.T) {
	env := newAlertingIssueEnv(t)
	issueID := env.ingestSignal(t)
	env.patchStatus(t, issueID, "open")
	got := incidentsOf(t, env.pool, env.projectID, issueID)
	if len(got) != 1 || got[0].status != "firing" || got[0].notify != "pending" {
		t.Fatalf("after open = %+v, want untouched firing pending", got)
	}
}

func TestResolveIssueLeavesOtherProjectAndSpikeIncidents(t *testing.T) {
	env := newAlertingIssueEnv(t)
	ctx := context.Background()
	issueID := env.ingestSignal(t)

	var otherProject int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO projects(org_id, name, slug, platform)
SELECT org_id, 'Other', 'other-issues', 'web' FROM projects WHERE id = $1 RETURNING id`, env.projectID).Scan(&otherProject); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `
INSERT INTO alert_incidents(project_id, rule_kind, issue_id) VALUES ($1, 'new_issue', $3), ($2, 'issue_spike', $3)`,
		otherProject, env.projectID, issueID); err != nil {
		t.Fatal(err)
	}

	env.patchStatus(t, issueID, "resolved")

	other := incidentsOf(t, env.pool, otherProject, issueID)
	if len(other) != 1 || other[0].status != "firing" || other[0].notify != "pending" {
		t.Fatalf("other project incident = %+v, want untouched", other)
	}
	for _, r := range incidentsOf(t, env.pool, env.projectID, issueID) {
		if r.kind == "issue_spike" && (r.status != "firing" || r.notify != "pending") {
			t.Fatalf("spike incident = %+v, want untouched", r)
		}
		if r.kind == "new_issue" && r.status != "resolved" {
			t.Fatalf("new_issue incident = %+v, want resolved", r)
		}
	}
}

// insertIssueWithEvents creates an issue and one event per device spec.
func insertIssueWithEvents(t *testing.T, env *issueEnv, events []struct {
	user, release string
	device        map[string]any
}) int64 {
	t.Helper()
	ctx := context.Background()
	var issueID int64
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO issues(project_id, fingerprint, title, type, status, count)
		VALUES ($1, 'fp-summary', 'java.lang.NullPointerException', 'NullPointerException', 'open', $2)
		RETURNING id`, env.projectID, len(events)).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	for i, ev := range events {
		dev, _ := json.Marshal(ev.device)
		if _, err := env.pool.Exec(ctx, `
			INSERT INTO issue_events(issue_id, user_id, device_info, release_version, created_at)
			VALUES ($1, $2, $3, $4, now() - make_interval(mins => $5))`,
			issueID, ev.user, dev, ev.release, i); err != nil {
			t.Fatal(err)
		}
	}
	return issueID
}

func getIssueDetail(t *testing.T, env *issueEnv, issueID int64) IssueDetail {
	t.Helper()
	resp, err := env.app.Test(httptest.NewRequest("GET", "/api/v1/projects/999/issues/"+strconv.FormatInt(issueID, 10), nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	var detail IssueDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	return detail
}

func bucketCount(bs []IssueBucket, value string) int64 {
	for _, b := range bs {
		if b.Value == value {
			return b.Count
		}
	}
	return -1
}

func TestIssueDetailSummaryBreakdowns(t *testing.T) {
	env := newIssueEnv(t)
	android14 := map[string]any{"os": "Android", "os_version": "14", "manufacturer": "samsung", "model": "SM-A546E"}
	android13 := map[string]any{"os": "Android", "os_version": "13", "manufacturer": "Xiaomi", "model": "Redmi Note 12"}
	issueID := insertIssueWithEvents(t, env, []struct {
		user, release string
		device        map[string]any
	}{
		{"u_1", "2.5.1", android14},
		{"u_1", "2.5.1", android14},
		{"u_2", "2.5.0", android13},
		{"", "", map[string]any{"app_version": "2.4.0"}},
	})

	s := getIssueDetail(t, env, issueID).Summary

	if s.TotalEvents != 4 {
		t.Errorf("total_events = %d, want 4", s.TotalEvents)
	}
	if s.AffectedUsers != 2 {
		t.Errorf("affected_users = %d, want 2 (anonymous events excluded)", s.AffectedUsers)
	}
	if got := bucketCount(s.OSVersions, "Android 14"); got != 2 {
		t.Errorf("Android 14 = %d, want 2 (buckets %+v)", got, s.OSVersions)
	}
	if got := bucketCount(s.OSVersions, "Unknown"); got != 1 {
		t.Errorf("Unknown OS = %d, want 1 (buckets %+v)", got, s.OSVersions)
	}
	if got := bucketCount(s.Devices, "samsung SM-A546E"); got != 2 {
		t.Errorf("samsung device = %d, want 2 (buckets %+v)", got, s.Devices)
	}
	if got := bucketCount(s.AppVersions, "2.4.0"); got != 1 {
		t.Errorf("app_version fallback 2.4.0 = %d, want 1 (buckets %+v)", got, s.AppVersions)
	}
	if s.OSVersions[0].Value != "Android 14" {
		t.Errorf("buckets not sorted by count desc: %+v", s.OSVersions)
	}
}

func TestIssueDetailSummaryAffectedUsers(t *testing.T) {
	env := newIssueEnv(t)
	issueID := insertIssueWithEvents(t, env, []struct {
		user, release string
		device        map[string]any
	}{
		{"u_1", "2.5.1", map[string]any{"os": "Android", "os_version": "14", "model": "Pixel 8"}},
		{"u_2", "2.5.0", map[string]any{"os": "Android", "os_version": "13", "model": "Pixel 6"}},
		{"u_1", "2.5.0", map[string]any{"os": "Android", "os_version": "13", "model": "Pixel 7"}},
	})

	users := getIssueDetail(t, env, issueID).Summary.Users
	if len(users) != 2 {
		t.Fatalf("users = %+v, want 2 entries", users)
	}
	top := users[0]
	if top.UserID != "u_1" || top.Count != 2 {
		t.Fatalf("top user = %+v, want u_1 with 2 crashes", top)
	}
	// Device/OS/app version come from that user's most recent event.
	if top.Device != "Pixel 8" || top.OS != "Android 14" || top.AppVersion != "2.5.1" {
		t.Fatalf("top user context = %+v, want Pixel 8 / Android 14 / 2.5.1", top)
	}
}

func TestIssueDetailSummaryDailyTrend(t *testing.T) {
	env := newIssueEnv(t)
	issueID := insertIssueWithEvents(t, env, []struct {
		user, release string
		device        map[string]any
	}{
		{"u_1", "1.0", map[string]any{}},
		{"u_2", "1.0", map[string]any{}},
	})
	daily := getIssueDetail(t, env, issueID).Summary.Daily
	if len(daily) != 14 {
		t.Fatalf("daily = %d points, want 14", len(daily))
	}
	var sum int64
	for _, d := range daily {
		sum += d.Count
	}
	if sum != 2 {
		t.Fatalf("daily sum = %d, want 2", sum)
	}
}

func TestLoadIssueDetailScopedToProject(t *testing.T) {
	env := newIssueEnv(t)
	ctx := context.Background()
	dev := map[string]any{"os": "Android", "os_version": "14", "model": "Pixel 8"}
	issueID := insertIssueWithEvents(t, env, []struct {
		user, release string
		device        map[string]any
	}{{"u_1", "2.5.1", dev}, {"u_2", "2.5.1", dev}, {"u_3", "2.5.1", dev}})

	var orgID, otherProject int64
	if err := env.pool.QueryRow(ctx, `SELECT org_id FROM projects WHERE id = $1`, env.projectID).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(ctx,
		`INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'Other','other-issues','web') RETURNING id`, orgID).Scan(&otherProject); err != nil {
		t.Fatal(err)
	}

	if _, err := env.h.loadIssueDetail(ctx, otherProject, issueID, 20); err != errIssueNotFound {
		t.Fatalf("other project err = %v, want errIssueNotFound", err)
	}
	if _, err := env.h.setIssueStatus(ctx, otherProject, issueID, "resolved"); err != errIssueNotFound {
		t.Fatalf("setIssueStatus other project err = %v, want errIssueNotFound", err)
	}

	d, err := env.h.loadIssueDetail(ctx, env.projectID, issueID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Events) != 2 {
		t.Errorf("events = %d, want limit 2", len(d.Events))
	}
	if d.Summary.TotalEvents != 3 {
		t.Errorf("summary total = %d, want 3", d.Summary.TotalEvents)
	}

	it, err := env.h.setIssueStatus(ctx, env.projectID, issueID, "resolved")
	if err != nil || it.Status != "resolved" {
		t.Fatalf("setIssueStatus = %+v, %v", it, err)
	}
	if _, err := env.h.setIssueStatus(ctx, env.projectID, issueID, "bogus"); err == nil {
		t.Fatal("expected error for invalid status")
	}
}
