package ingest

import (
	"context"
	"fmt"
	"os"
	"strings"
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

// A sanitised hostile item must not make the batch fail for the other rows (including other projects).
func TestAppStartSinkBatchWithSanitisedItemInsertsAllRows(t *testing.T) {
	pool := appStartTestDB(t)
	ctx := context.Background()

	var orgID, p1, p2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'P1','p1','android') RETURNING id`, orgID).Scan(&p1); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'P2','p2','android') RETURNING id`, orgID).Scan(&p2); err != nil {
		t.Fatal(err)
	}

	boundary := strings.Repeat("a", 63) + "€€"
	hostile := fmt.Sprintf(`{"v":1,"platform":"android","start_id":"h\u0000x","type":"cold","duration_ms":500,
		"first_screen":"A\u0000B","build":%q,"device":{"model":%q,"os_version":"1\u00004","ram_mb":2000000000}}`, boundary, boundary)
	now := time.Now().UTC()
	item := appStartItem(hostile)
	item.SessionID = "s\x00es"

	var rows []AppStartRow
	for i, pid := range []int64{p1, p2} {
		good, err := ParseAppStartItem(pid, now, "production", "1.0", appStartItem(fmt.Sprintf(
			`{"v":1,"platform":"android","start_id":"good%d","type":"cold","duration_ms":500}`, i)))
		if err != nil {
			t.Fatal(err)
		}
		bad, err := ParseAppStartItem(pid, now, "\x00", boundary, item)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, good, bad)
	}
	if err := NewPgxAppStartSink(pool).WriteAppStarts(ctx, rows); err != nil {
		t.Fatalf("write: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app_starts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("rows = %d, want 4", n)
	}
	var env string
	if err := pool.QueryRow(ctx, `SELECT environment FROM app_starts WHERE start_id='hx' AND project_id=$1`, p1).Scan(&env); err != nil || env != "production" {
		t.Fatalf("hostile row env = %q err=%v", env, err)
	}
}
