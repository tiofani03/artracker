package ingest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/db"
)

// appStartTestDB connects to TEST_DATABASE_URL, migrates and truncates the shared tables; it skips when unset.
func appStartTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE users, organizations, organization_members, projects, api_keys, refresh_tokens RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func TestAppStartSinkIgnoresDuplicateStartID(t *testing.T) {
	pool := appStartTestDB(t)
	ctx := context.Background()

	var orgID, pid int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'P','p','android') RETURNING id`, orgID).Scan(&pid); err != nil {
		t.Fatal(err)
	}

	ram := 3712
	row := AppStartRow{
		ProjectID: pid, StartID: "s1", SessionID: "ses_a", Platform: "android", Environment: "production",
		Type: "cold", DurationMs: 2412, Phases: []AppStartPhase{{Name: "first_draw", Ms: 2412}},
		FirstScreen: "MainActivity", AppVersion: "3.2.0", Build: "1203", OSVersion: "14", DeviceModel: "SM-A145F",
		RAMMB: &ram, Profiled: true, StartedAt: time.Now().UTC(), ReceivedAt: time.Now().UTC(),
	}
	sink := NewPgxAppStartSink(pool)
	for i := 0; i < 2; i++ {
		if err := sink.WriteAppStarts(ctx, []AppStartRow{row}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app_starts WHERE project_id=$1 AND start_id='s1'`, pid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}
