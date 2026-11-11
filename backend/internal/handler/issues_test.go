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

	return &issueEnv{app: app, pool: pool, projectID: projectID, userID: userID}
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
