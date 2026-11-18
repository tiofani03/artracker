package heatmap

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/db"
	"github.com/tiofani03/artracker/backend/internal/storage"
)

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

const sid = "sess_1"

type wfix struct {
	pool *pgxpool.Pool
	dir  string
	w    *Worker
	pid  int64
}

func newWFix(t *testing.T) *wfix {
	t.Helper()
	pool := testDB(t)
	ctx := context.Background()
	var orgID, pid int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'P','p','android') RETURNING id`, orgID).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO replay_sessions(id, project_id) VALUES ($1,$2)`, sid, pid); err != nil {
		t.Fatal(err)
	}
	for i, v := range []string{"4.57.0", "4.57.0", ""} {
		if _, err := pool.Exec(ctx, `INSERT INTO telemetry_events(id, project_id, kind, session_id, release_version, event_timestamp)
			VALUES (gen_random_uuid(), $1, 'event', $2, $3, now())`, pid, sid, v); err != nil {
			t.Fatalf("telemetry %d: %v", i, err)
		}
	}
	dir := t.TempDir()
	store, err := storage.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &wfix{pool: pool, dir: dir, w: NewWorker(pool, store, slog.Default()), pid: pid}
}

// addChunk registers a chunk row (bumping chunk_count) and optionally writes its file.
func (f *wfix) addChunk(t *testing.T, seq int, body string, write bool) {
	t.Helper()
	key := fmt.Sprintf("projects/%d/sessions/%s/chunk_%d.gz", f.pid, sid, seq)
	if write {
		p := filepath.Join(f.dir, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, gz(t, body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `INSERT INTO replay_chunks(project_id, session_id, chunk_seq, object_key) VALUES ($1,$2,$3,$4)`, f.pid, sid, seq, key); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET chunk_count = chunk_count + 1 WHERE project_id=$1 AND id=$2`, f.pid, sid); err != nil {
		t.Fatal(err)
	}
}

func (f *wfix) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE project_id=$1`, f.pid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// chunkBody has a frame for screen at t0 and one tap per entry of tapTS (down+up 50ms later).
func chunkBody(screen string, t0 int64, tapTS ...int64) string {
	s := fmt.Sprintf(`{"format":"artracker-screen-v1","platform":"android","frames":[{"ts":%d,"screen":%q,"w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","masked":0,"data":"AAAA"}],"touches":[`, t0, screen)
	var b bytes.Buffer
	for i, ts := range tapTS {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"ts":%d,"x":100,"y":200,"kind":"down"},{"ts":%d,"x":100,"y":200,"kind":"up"}`, ts, ts+50)
	}
	return s + b.String() + `]}`
}

func TestProcessSessionWritesRows(t *testing.T) {
	f := newWFix(t)
	f.addChunk(t, 0, chunkBody("HomeActivity", 1_000_000, 1_001_000, 1_005_000), true)
	if err := f.w.ProcessSession(context.Background(), f.pid, sid); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM screen_taps WHERE project_id=$1 AND app_version='4.57.0' AND platform='android' AND screen='HomeActivity'`, f.pid).Scan(&n); err != nil || n != 2 {
		t.Fatalf("taps = %d err=%v", n, err)
	}
	if v, bg := f.count(t, "screen_visits"), f.count(t, "screen_backgrounds"); v != 1 || bg != 1 {
		t.Fatalf("visits=%d backgrounds=%d", v, bg)
	}
	var pc int
	if err := f.pool.QueryRow(context.Background(), `SELECT taps_processed_chunks FROM replay_sessions WHERE project_id=$1 AND id=$2 AND taps_processed_at IS NOT NULL`, f.pid, sid).Scan(&pc); err != nil || pc != 1 {
		t.Fatalf("processed chunks = %d err=%v", pc, err)
	}
}

func TestProcessSessionIdempotent(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	f.addChunk(t, 0, chunkBody("HomeActivity", 1_000_000, 1_001_000, 1_005_000), true)
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	f.addChunk(t, 1, `{"format":"artracker-screen-v1","platform":"android","frames":[],"touches":[{"ts":1010000,"x":100,"y":200,"kind":"down"},{"ts":1010050,"x":100,"y":200,"kind":"up"}]}`, true)
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, "screen_taps"); n != 3 {
		t.Fatalf("taps = %d, want 3", n)
	}
	if n := f.count(t, "screen_visits"); n != 1 {
		t.Fatalf("visits = %d, want 1", n)
	}
}

func TestProcessSessionSkipsMissingChunk(t *testing.T) {
	f := newWFix(t)
	f.addChunk(t, 0, "", false)
	f.addChunk(t, 1, chunkBody("HomeActivity", 1_000_000, 1_001_000), true)
	if err := f.w.ProcessSession(context.Background(), f.pid, sid); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, "screen_taps"); n != 1 {
		t.Fatalf("taps = %d, want 1", n)
	}
}

func TestProcessSessionRespectsExclusion(t *testing.T) {
	f := newWFix(t)
	if _, err := f.pool.Exec(context.Background(), `UPDATE projects SET heatmap_excluded_screens = '{HomeActivity}' WHERE id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	f.addChunk(t, 0, chunkBody("HomeActivity", 1_000_000, 1_001_000), true)
	if err := f.w.ProcessSession(context.Background(), f.pid, sid); err != nil {
		t.Fatal(err)
	}
	for _, tb := range []string{"screen_taps", "screen_visits", "screen_backgrounds"} {
		if n := f.count(t, tb); n != 0 {
			t.Fatalf("%s = %d, want 0", tb, n)
		}
	}
}

func TestProcessPendingIdle(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	f.addChunk(t, 0, chunkBody("HomeActivity", 1_000_000, 1_001_000), true)
	if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET updated_at = now() WHERE project_id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 0 {
		t.Fatalf("fresh session: n=%d err=%v", n, err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET updated_at = now() - interval '11 minutes' WHERE project_id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 1 {
		t.Fatalf("idle session: n=%d err=%v", n, err)
	}
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 0 {
		t.Fatalf("already processed: n=%d err=%v", n, err)
	}
	f.addChunk(t, 1, chunkBody("HomeActivity", 1_100_000, 1_101_000), true)
	if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET updated_at = now() - interval '11 minutes' WHERE project_id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 1 {
		t.Fatalf("grown session: n=%d err=%v", n, err)
	}
}
