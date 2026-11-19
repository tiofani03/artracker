package appstart

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/db"
)

// testDB connects to TEST_DATABASE_URL, migrates, and truncates the shared tables.
// It skips the test when TEST_DATABASE_URL is unset.
func testDB(t *testing.T) *pgxpool.Pool {
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

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestMigration0019Tables(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	var orgID, pid int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'P','p','android') RETURNING id`, orgID).Scan(&pid); err != nil {
		t.Fatal(err)
	}

	const insertStart = `INSERT INTO app_starts(project_id, start_id, platform, type, duration_ms, started_at)
		VALUES ($1, $2, 'android', $3, 1200, now())`
	if _, err := pool.Exec(ctx, insertStart, pid, "s1", "cold"); err != nil {
		t.Fatalf("insert app_starts: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app_start_profiles(project_id, start_id, platform, raw)
		VALUES ($1, 's1', 'android', '\x00')`, pid); err != nil {
		t.Fatalf("insert app_start_profiles: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app_start_fn_stats(project_id, start_id, platform, environment, app_version, function, is_app, self_ms, total_ms, attributed_ms, started_at)
		VALUES ($1, 's1', 'android', 'production', '1.0.0', 'com.acme.App.onCreate', true, 10, 20, 15, now())`, pid); err != nil {
		t.Fatalf("insert app_start_fn_stats: %v", err)
	}

	if _, err := pool.Exec(ctx, insertStart, pid, "s1", "cold"); pgCode(err) != "23505" {
		t.Errorf("duplicate (project_id,start_id): want 23505, got %v", err)
	}
	if _, err := pool.Exec(ctx, insertStart, pid, "s2", "boot"); pgCode(err) != "23514" {
		t.Errorf("type='boot': want 23514, got %v", err)
	}
}
