package heatmap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000, 1699999005000), true)
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
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000, 1699999005000), true)
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	f.addChunk(t, 1, `{"format":"artracker-screen-v1","platform":"android","frames":[],"touches":[{"ts":1699999010000,"x":100,"y":200,"kind":"down"},{"ts":1699999010050,"x":100,"y":200,"kind":"up"}]}`, true)
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
	f.addChunk(t, 1, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
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
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
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
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
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
	f.addChunk(t, 1, chunkBody("HomeActivity", 1699999100000, 1699999101000), true)
	if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET updated_at = now() - interval '11 minutes' WHERE project_id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 1 {
		t.Fatalf("grown session: n=%d err=%v", n, err)
	}
}

func TestProcessSessionStripsNulAndSkipsBadTimestamps(t *testing.T) {
	f := newWFix(t)
	body := strings.Replace(chunkBody("HomeXActivity", 1_700_000_000_000, 1_700_000_001_000), "X", `\u0000`, 1)
	// a frame and a touch with an absurd ts must be dropped, not fail the session
	body = body[:len(body)-2] + `,{"ts":1e300,"x":1,"y":1,"kind":"down"}]}`
	f.addChunk(t, 0, body, true)
	if err := f.w.ProcessSession(context.Background(), f.pid, sid); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM screen_taps WHERE project_id=$1 AND screen='HomeActivity'`, f.pid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("taps = %d err=%v", n, err)
	}
}

// hookGetter wraps an ObjectGetter; after (if set) runs once the object was fetched, err overrides it.
type hookGetter struct {
	inner ObjectGetter
	err   error
	after func()
}

func (h *hookGetter) GetObject(ctx context.Context, key string) (io.ReadCloser, int64, string, error) {
	if h.err != nil {
		return nil, 0, "", h.err
	}
	rc, n, ct, err := h.inner.GetObject(ctx, key)
	if err == nil && h.after != nil {
		h.after()
	}
	return rc, n, ct, err
}

func (f *wfix) idleNow(t *testing.T) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE replay_sessions SET updated_at = now() - interval '11 minutes' WHERE project_id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
}

func TestProcessSessionExclusionChangedAfterChunkLoad(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
	f.w.store = &hookGetter{inner: f.w.store, after: func() {
		if _, err := f.pool.Exec(ctx, `UPDATE projects SET heatmap_excluded_screens = '{HomeActivity}' WHERE id=$1`, f.pid); err != nil {
			t.Error(err)
		}
	}}
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	for _, tb := range []string{"screen_taps", "screen_visits", "screen_backgrounds"} {
		if n := f.count(t, tb); n != 0 {
			t.Fatalf("%s = %d, want 0", tb, n)
		}
	}
}

func TestFailingSessionBacksOffThenGivesUp(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
	f.idleNow(t)
	f.w.store = &hookGetter{inner: f.w.store, err: errors.New("r2 unavailable")}

	if n, err := f.w.ProcessPending(ctx); err != nil || n != 0 {
		t.Fatalf("first: n=%d err=%v", n, err)
	}
	var attempts int
	var retryAt *time.Time
	var processed bool
	q := `SELECT taps_attempts, taps_retry_at, taps_processed_at IS NOT NULL FROM replay_sessions WHERE project_id=$1 AND id=$2`
	if err := f.pool.QueryRow(ctx, q, f.pid, sid).Scan(&attempts, &retryAt, &processed); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || retryAt == nil || !retryAt.After(time.Now()) || processed {
		t.Fatalf("after failure: attempts=%d retry=%v processed=%v", attempts, retryAt, processed)
	}
	// not picked again before retry_at
	if _, err := f.w.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, q, f.pid, sid).Scan(&attempts, &retryAt, &processed); err != nil || attempts != 1 {
		t.Fatalf("retried early: attempts=%d err=%v", attempts, err)
	}
	// after the maximum number of attempts the session is marked processed
	for i := 0; i < maxAttempts-1; i++ {
		if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET taps_retry_at = now() - interval '1 second' WHERE project_id=$1`, f.pid); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.ProcessPending(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.pool.QueryRow(ctx, q, f.pid, sid).Scan(&attempts, &retryAt, &processed); err != nil {
		t.Fatal(err)
	}
	if attempts != maxAttempts || !processed {
		t.Fatalf("attempts=%d processed=%v, want %d/true", attempts, processed, maxAttempts)
	}
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 0 {
		t.Fatalf("given-up session picked again: n=%d err=%v", n, err)
	}
}

func TestSuccessResetsAttempts(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
	if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET taps_attempts=3, taps_retry_at = now() - interval '1 second' WHERE project_id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	f.idleNow(t)
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	var a int
	var r *time.Time
	if err := f.pool.QueryRow(ctx, `SELECT taps_attempts, taps_retry_at FROM replay_sessions WHERE project_id=$1`, f.pid).Scan(&a, &r); err != nil || a != 0 || r != nil {
		t.Fatalf("attempts=%d retry=%v err=%v", a, r, err)
	}
}

func TestTransientStorageErrorFailsSession(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
	f.w.store = &hookGetter{inner: f.w.store, err: errors.New("connection reset")}
	if err := f.w.ProcessSession(ctx, f.pid, sid); err == nil {
		t.Fatal("want error for a transient storage failure")
	}
	if n := f.count(t, "screen_taps"); n != 0 {
		t.Fatalf("taps = %d", n)
	}
	var processed bool
	if err := f.pool.QueryRow(ctx, `SELECT taps_processed_at IS NOT NULL FROM replay_sessions WHERE project_id=$1`, f.pid).Scan(&processed); err != nil || processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	// a not-found object is skipped, not an error
	f.w.store = &hookGetter{inner: f.w.store, err: fmt.Errorf("get: %w", storage.ErrNotFound)}
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatalf("not-found must be skipped: %v", err)
	}
}

func TestPendingSkipsWebAndStopsOnNonScreenChunk(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	f.addChunk(t, 0, `[{"type":4,"data":{}}]`, true)
	f.addChunk(t, 1, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, "screen_taps"); n != 0 {
		t.Fatalf("taps = %d, want 0 after a non-screen first chunk", n)
	}
	var processed bool
	if err := f.pool.QueryRow(ctx, `SELECT taps_processed_at IS NOT NULL FROM replay_sessions WHERE project_id=$1`, f.pid).Scan(&processed); err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET taps_processed_at=NULL WHERE project_id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	f.idleNow(t)
	if _, err := f.pool.Exec(ctx, `UPDATE projects SET platform='web' WHERE id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 0 {
		t.Fatalf("web project: n=%d err=%v", n, err)
	}
}

func TestProcessPendingNewestFirstAndDrains(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	f.addChunk(t, 0, chunkBody("HomeActivity", 1699999000000, 1699999001000), true)
	// two more idle sessions sharing the chunk object
	for _, id := range []string{"old", "mid"} {
		if _, err := f.pool.Exec(ctx, `INSERT INTO replay_sessions(id, project_id, chunk_count) VALUES ($1,$2,1)`, id, f.pid); err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("projects/%d/sessions/%s/chunk_0.gz", f.pid, sid)
		if _, err := f.pool.Exec(ctx, `INSERT INTO replay_chunks(project_id, session_id, chunk_seq, object_key) VALUES ($1,$2,0,$3)`, f.pid, id, key); err != nil {
			t.Fatal(err)
		}
	}
	for id, ago := range map[string]string{"old": "3 hours", sid: "1 hour", "mid": "2 hours"} {
		if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET updated_at = now() - $3::interval WHERE project_id=$1 AND id=$2`, f.pid, id, ago); err != nil {
			t.Fatal(err)
		}
	}
	f.w.batch = 1
	p, err := f.w.listPending(ctx)
	if err != nil || len(p) != 1 || p[0].sessionID != sid {
		t.Fatalf("newest first: %+v err=%v", p, err)
	}
	// one tick drains every batch
	if n, err := f.w.ProcessPending(ctx); err != nil || n != 3 {
		t.Fatalf("drain: n=%d err=%v", n, err)
	}
}

// flowBody is a screen chunk with one frame per "Screen@ts" entry and no touches.
func flowBody(frames ...string) string {
	var fr []string
	for _, e := range frames {
		name, tsStr, _ := strings.Cut(e, "@")
		fr = append(fr, fmt.Sprintf(`{"ts":%s,"screen":%q,"w":200,"h":400,"vw":400,"vh":800,"mime":"image/jpeg","masked":0,"data":"AAAA"}`, tsStr, name))
	}
	return `{"format":"artracker-screen-v1","platform":"android","frames":[` + strings.Join(fr, ",") + `],"touches":[]}`
}

func TestProcessSessionWritesScreenViews(t *testing.T) {
	f := newWFix(t)
	const t0 = 1699999000000
	f.addChunk(t, 0, flowBody(fmt.Sprintf("Home@%d", t0), fmt.Sprintf("Cart@%d", t0+5000)), true)
	if err := f.w.ProcessSession(context.Background(), f.pid, sid); err != nil {
		t.Fatal(err)
	}
	rows, err := f.pool.Query(context.Background(), `SELECT seq, screen, duration_ms, is_entry, is_exit, platform, app_version, source
		FROM screen_views WHERE project_id=$1 AND session_id=$2 ORDER BY seq`, f.pid, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		seq             int
		screen          string
		dur             int
		entry, exit     bool
		plat, ver, srcs string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.seq, &r.screen, &r.dur, &r.entry, &r.exit, &r.plat, &r.ver, &r.srcs); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(got), got)
	}
	if got[0].seq != 0 || got[0].screen != "Home" || got[0].dur != 5000 || !got[0].entry || got[0].exit {
		t.Fatalf("row 0 = %+v", got[0])
	}
	if got[1].seq != 1 || got[1].screen != "Cart" || got[1].entry || !got[1].exit {
		t.Fatalf("row 1 = %+v", got[1])
	}
	for _, r := range got {
		if r.plat != "android" || r.ver != "4.57.0" || r.srcs != "replay" {
			t.Fatalf("row = %+v", r)
		}
	}
}

func TestProcessSessionScreenViewsIdempotent(t *testing.T) {
	f := newWFix(t)
	const t0 = 1699999000000
	f.addChunk(t, 0, flowBody(fmt.Sprintf("Home@%d", t0), fmt.Sprintf("Cart@%d", t0+5000)), true)
	for i := 0; i < 2; i++ {
		if err := f.w.ProcessSession(context.Background(), f.pid, sid); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count(t, "screen_views"); n != 2 {
		t.Fatalf("screen_views = %d, want 2", n)
	}
}

func TestProcessSessionMissingChunkKeepsHeatmapRows(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	const t0 = 1699999000000
	f.addChunk(t, 0, chunkBody("Home", t0, t0+1000), true)
	f.addChunk(t, 1, chunkBody("Cart", t0+5000, t0+6000), true)
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, "screen_taps"); n != 2 {
		t.Fatalf("taps = %d, want 2", n)
	}
	visits := f.count(t, "screen_visits")
	if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(fmt.Sprintf("projects/%d/sessions/%s/chunk_0.gz", f.pid, sid)))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE replay_sessions SET taps_processed_at = NULL WHERE project_id=$1 AND id=$2`, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, "screen_taps"); n != 2 {
		t.Fatalf("taps after missing chunk = %d, want 2", n)
	}
	if n := f.count(t, "screen_visits"); n != visits {
		t.Fatalf("visits = %d, want %d", n, visits)
	}
	var views int
	var screen string
	if err := f.pool.QueryRow(ctx, `SELECT count(*), COALESCE(min(screen),'') FROM screen_views WHERE project_id=$1`, f.pid).Scan(&views, &screen); err != nil {
		t.Fatal(err)
	}
	if views != 1 || screen != "Cart" {
		t.Fatalf("views = %d screen = %q, want 1 Cart", views, screen)
	}
	var processed bool
	if err := f.pool.QueryRow(ctx, `SELECT taps_processed_at IS NOT NULL FROM replay_sessions WHERE project_id=$1 AND id=$2`, f.pid, sid).Scan(&processed); err != nil || !processed {
		t.Fatalf("processed = %v err=%v", processed, err)
	}
}

func TestProcessSessionExcludedScreenStillInFlow(t *testing.T) {
	f := newWFix(t)
	ctx := context.Background()
	const t0 = 1699999000000
	if _, err := f.pool.Exec(ctx, `UPDATE projects SET heatmap_excluded_screens = '{Cart}' WHERE id=$1`, f.pid); err != nil {
		t.Fatal(err)
	}
	f.addChunk(t, 0, chunkBody("Home", t0), true)
	f.addChunk(t, 1, chunkBody("Cart", t0+5000, t0+6000), true)
	if err := f.w.ProcessSession(ctx, f.pid, sid); err != nil {
		t.Fatal(err)
	}
	var taps, cartViews int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM screen_taps WHERE project_id=$1 AND screen='Cart'`, f.pid).Scan(&taps); err != nil || taps != 0 {
		t.Fatalf("cart taps = %d err=%v", taps, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM screen_views WHERE project_id=$1 AND screen='Cart'`, f.pid).Scan(&cartViews); err != nil || cartViews != 1 {
		t.Fatalf("cart views = %d err=%v", cartViews, err)
	}
}
