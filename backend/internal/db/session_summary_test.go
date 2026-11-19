package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type summaryRow struct {
	events     int64
	rage, dead bool
	hasError   bool
	env        string
}

// 0020_session_summary.sql backfills session_contexts from telemetry_events and issue_events, and stays
// correct when it runs again.
func TestSessionSummaryBackfill(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	body, err := migrationsFS.ReadFile("migrations/0020_session_summary.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	var orgID, pid int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('bf','bf-summary-'||gen_random_uuid()) RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,platform) VALUES ($1,'bf','bf','web') RETURNING id`, orgID).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM telemetry_events WHERE project_id = $1`, pid)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	now := time.Now()
	tel := func(session, name, level, env string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO telemetry_events(id, project_id, kind, name, level, session_id, user_id, environment, event_timestamp)
			VALUES (gen_random_uuid(), $1, 'event', $2, $3, $4, 'u1', $5, $6)`,
			pid, name, level, session, env, now); err != nil {
			t.Fatal(err)
		}
	}
	tel("s1", "tap", "info", "staging")
	tel("s1", "rage_tap", "", "staging")
	tel("s1", "replay_chunk", "", "staging")
	tel("s2", "dead_click", "error", "production")
	var issueID int64
	if err := pool.QueryRow(ctx, `INSERT INTO issues(project_id, fingerprint, title) VALUES ($1,'fp','NPE') RETURNING id`, pid).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO issue_events(issue_id, session_id, user_id, environment) VALUES ($1,'s3','u3','qa')`, issueID); err != nil {
		t.Fatal(err)
	}

	want := map[string]summaryRow{
		"s1": {events: 2, rage: true, env: "staging"},
		"s2": {events: 1, dead: true, hasError: true, env: "production"},
		"s3": {events: 1, hasError: true, env: "qa"},
	}
	check := func(run string) {
		t.Helper()
		for sid, w := range want {
			var g summaryRow
			if err := pool.QueryRow(ctx, `
				SELECT event_count, has_rage_click, has_dead_click, has_error, environment
				FROM session_contexts WHERE project_id = $1 AND session_id = $2`, pid, sid).
				Scan(&g.events, &g.rage, &g.dead, &g.hasError, &g.env); err != nil {
				t.Fatalf("%s: %s: %v", run, sid, err)
			}
			if g != w {
				t.Errorf("%s: %s = %+v, want %+v", run, sid, g, w)
			}
		}
	}

	if _, err := pool.Exec(ctx, `DELETE FROM session_contexts WHERE project_id = $1`, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(body), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("first run: %v", err)
	}
	check("first run")
	if _, err := pool.Exec(ctx, string(body), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("second run: %v", err)
	}
	check("second run")
}
