package appstart

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/ingest"
	"github.com/tiofani03/artracker/backend/internal/symbolication"
)

func TestNextAttempt(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	received := now.Add(-time.Minute)
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 5 * time.Minute},
		{2, 10 * time.Minute},
		{3, 20 * time.Minute},
		{8, 6 * time.Hour}, // 5m * 2^7 = 640m, capped
		{40, 6 * time.Hour},
	}
	for _, c := range cases {
		got := NextAttempt(c.attempts, received, now)
		if got == nil || !got.Equal(now.Add(c.want)) {
			t.Errorf("NextAttempt(%d) = %v, want now+%s", c.attempts, got, c.want)
		}
	}
	if got := NextAttempt(1, now.Add(-23*time.Hour-55*time.Minute), now); got != nil {
		t.Errorf("NextAttempt past the 24h window = %v, want nil", *got)
	}
	if got := NextAttempt(1, now.Add(-24*time.Hour+6*time.Minute), now); got == nil {
		t.Error("NextAttempt landing just inside the 24h window = nil, want a retry")
	}
}

func testWorker(t *testing.T, pool *pgxpool.Pool) *Worker {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewWorker(pool, symbolication.NewLoader(pool, 4), log)
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func seedProject(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()
	var orgID, pid int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'P','p','android') RETURNING id`, orgID).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return pid
}

// androidProfileRaw is a gzipped upload whose single sample sits in obfuscated a.b.a (com.example.Shop.start
// once the mapping from insertMapping is applied).
func androidProfileRaw(t *testing.T, startID string) []byte {
	t.Helper()
	line := 15
	up := ingest.AppStartProfileUpload{Profile: ingest.AppStartProfile{
		V: 1, StartID: startID, Platform: "android", AppPackages: []string{"com.example"}, IntervalMs: 10,
		Frames:  []ingest.ProfileFrame{{C: "a.b", M: "a", L: &line, F: "SourceFile"}},
		Stacks:  [][]int{{0}},
		Samples: []ingest.ProfileSample{{T: 0, S: 0}, {T: 10, S: 0}},
	}}
	b, err := json.Marshal(up)
	if err != nil {
		t.Fatal(err)
	}
	return gzipBytes(t, b)
}

func iosProfileRaw(t *testing.T, startID string) []byte {
	t.Helper()
	idx := 0
	up := ingest.AppStartProfileUpload{Profile: ingest.AppStartProfile{
		V: 1, StartID: startID, Platform: "ios", IntervalMs: 10,
		Images:  []ingest.ProfileImage{{UUID: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", Name: "libsystem", Load: "0x180000000"}},
		Frames:  []ingest.ProfileFrame{{A: "0x180001000", I: &idx, Sym: "dyld4::start"}},
		Stacks:  [][]int{{0}},
		Samples: []ingest.ProfileSample{{T: 0, S: 0}},
	}}
	b, err := json.Marshal(up)
	if err != nil {
		t.Fatal(err)
	}
	return gzipBytes(t, b)
}

func insertProfile(t *testing.T, pool *pgxpool.Pool, pid int64, startID, platform, version string, uuids []string, raw []byte, receivedAt time.Time) {
	t.Helper()
	if uuids == nil {
		uuids = []string{}
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO app_start_profiles (project_id, start_id, platform, environment, app_version, build, image_uuids, raw, received_at)
		VALUES ($1, $2, $3, 'staging', $4, '', $5, $6, $7)`,
		pid, startID, platform, version, uuids, raw, receivedAt)
	if err != nil {
		t.Fatal(err)
	}
}

func insertMapping(t *testing.T, pool *pgxpool.Pool, pid int64, version string) {
	t.Helper()
	mapping := []byte("com.example.Shop -> a.b:\n    10:20:void start() -> a\n")
	_, err := pool.Exec(context.Background(), `
		INSERT INTO project_release_artifacts(project_id, version, build_number, artifact_type, file_name,
			storage_path, content, checksum_sha256, cpu_arch, debug_uuid, size_bytes)
		VALUES ($1, $2, '', 'proguard_mapping', 'mapping.txt', 'p', $3, 'x', '', '', $4)`,
		pid, version, mapping, int64(len(mapping)))
	if err != nil {
		t.Fatal(err)
	}
}

type profileRow struct {
	status      string
	attempts    int
	nextAttempt *time.Time
	hasSymbols  bool
}

func readProfile(t *testing.T, pool *pgxpool.Pool, pid int64, startID string) profileRow {
	t.Helper()
	var r profileRow
	err := pool.QueryRow(context.Background(), `
		SELECT status, attempts, next_attempt_at, symbolicated IS NOT NULL
		FROM app_start_profiles WHERE project_id = $1 AND start_id = $2`, pid, startID).
		Scan(&r.status, &r.attempts, &r.nextAttempt, &r.hasSymbols)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWorkerSymbolicatesAndroid(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	insertMapping(t, pool, pid, "1.0.0")
	insertProfile(t, pool, pid, "s1", "android", "1.0.0", nil, androidProfileRaw(t, "s1"), time.Now())
	startedAt := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if _, err := pool.Exec(ctx, `
		INSERT INTO app_starts(project_id, start_id, platform, type, duration_ms, started_at)
		VALUES ($1, 's1', 'android', 'cold', 900, $2)`, pid, startedAt); err != nil {
		t.Fatal(err)
	}

	n, err := testWorker(t, pool).ProcessPending(ctx)
	if err != nil || n != 1 {
		t.Fatalf("ProcessPending = %d, %v; want 1, nil", n, err)
	}

	row := readProfile(t, pool, pid, "s1")
	if row.status != "done" || row.attempts != 0 || !row.hasSymbols {
		t.Fatalf("profile = %+v, want done with symbols and 0 attempts", row)
	}

	var isApp bool
	var selfMs, totalMs int
	var env, version string
	var gotStarted time.Time
	err = pool.QueryRow(ctx, `
		SELECT is_app, self_ms, total_ms, environment, app_version, started_at
		FROM app_start_fn_stats WHERE project_id = $1 AND start_id = 's1' AND function = 'com.example.Shop.start'`, pid).
		Scan(&isApp, &selfMs, &totalMs, &env, &version, &gotStarted)
	if err != nil {
		t.Fatalf("fn stat com.example.Shop.start: %v", err)
	}
	if !isApp || selfMs != 20 || totalMs != 20 {
		t.Errorf("stat = app %v self %d total %d, want app 20 20", isApp, selfMs, totalMs)
	}
	if env != "staging" || version != "1.0.0" {
		t.Errorf("dims = %q %q, want staging 1.0.0 (from the profile row)", env, version)
	}
	if !gotStarted.Equal(startedAt) {
		t.Errorf("started_at = %v, want %v (from app_starts)", gotStarted, startedAt)
	}

	// Reprocessing replaces the stats rather than duplicating them.
	if _, err := pool.Exec(ctx, `UPDATE app_start_profiles SET status = 'pending', next_attempt_at = now()`); err != nil {
		t.Fatal(err)
	}
	if _, err := testWorker(t, pool).ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app_start_fn_stats WHERE start_id = 's1'`).Scan(&count); err != nil || count != 1 {
		t.Errorf("fn stat rows after reprocessing = %d, %v; want 1", count, err)
	}
}

func TestWorkerProcessesProfileWithoutStartRow(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	insertMapping(t, pool, pid, "1.0.0")
	received := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	insertProfile(t, pool, pid, "orphan", "android", "1.0.0", nil, androidProfileRaw(t, "orphan"), received)

	if _, err := testWorker(t, pool).ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}
	if row := readProfile(t, pool, pid, "orphan"); row.status != "done" {
		t.Fatalf("status = %q, want done", row.status)
	}
	var startedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT started_at FROM app_start_fn_stats WHERE start_id = 'orphan' LIMIT 1`).Scan(&startedAt); err != nil {
		t.Fatal(err)
	}
	if !startedAt.Equal(received) {
		t.Errorf("started_at = %v, want received_at %v", startedAt, received)
	}
}

func TestWorkerMissingSymbolsBacksOff(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	insertProfile(t, pool, pid, "s1", "android", "1.0.0", nil, androidProfileRaw(t, "s1"), time.Now())

	if _, err := testWorker(t, pool).ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}
	row := readProfile(t, pool, pid, "s1")
	if row.status != "missing_symbols" || row.attempts != 1 || !row.hasSymbols {
		t.Fatalf("profile = %+v, want missing_symbols, attempts 1, raw frames stored", row)
	}
	if row.nextAttempt == nil {
		t.Fatal("next_attempt_at = NULL, want now+5m")
	}
	if d := time.Until(*row.nextAttempt); d < 4*time.Minute || d > 6*time.Minute {
		t.Errorf("next attempt in %s, want about 5m", d)
	}

	// Not due yet: a second pass leaves it alone.
	n, err := testWorker(t, pool).ProcessPending(ctx)
	if err != nil || n != 0 {
		t.Errorf("second ProcessPending = %d, %v; want 0, nil", n, err)
	}

	// Raw frames are kept and still produce stats.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app_start_fn_stats WHERE function = 'a.b.a'`).Scan(&count); err != nil || count != 1 {
		t.Errorf("unresolved fn stat rows = %d, %v; want 1", count, err)
	}
}

func TestWorkerGivesUpAfterRetryWindow(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	insertProfile(t, pool, pid, "old", "android", "1.0.0", nil, androidProfileRaw(t, "old"), time.Now().Add(-23*time.Hour-55*time.Minute))

	if _, err := testWorker(t, pool).ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}
	row := readProfile(t, pool, pid, "old")
	if row.status != "missing_symbols" || row.nextAttempt != nil || !row.hasSymbols {
		t.Errorf("profile = %+v, want missing_symbols with next_attempt_at NULL and frames kept", row)
	}
}

func TestRequeueOnArtifactUpload(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	insertProfile(t, pool, pid, "s1", "android", "1.0.0", nil, androidProfileRaw(t, "s1"), time.Now())
	w := testWorker(t, pool)

	if _, err := w.ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}
	if row := readProfile(t, pool, pid, "s1"); row.status != "missing_symbols" {
		t.Fatalf("status = %q, want missing_symbols", row.status)
	}

	insertMapping(t, pool, pid, "1.0.0")
	if err := Requeue(ctx, pool, pid, "1.0.0", ""); err != nil {
		t.Fatal(err)
	}
	if row := readProfile(t, pool, pid, "s1"); row.status != "pending" || row.attempts != 0 || row.nextAttempt == nil {
		t.Fatalf("after Requeue = %+v, want pending, attempts 0, next_attempt_at set", row)
	}
	if n, err := w.ProcessPending(ctx); err != nil || n != 1 {
		t.Fatalf("ProcessPending = %d, %v; want 1, nil", n, err)
	}
	if row := readProfile(t, pool, pid, "s1"); row.status != "done" {
		t.Errorf("status = %q, want done", row.status)
	}
}

func TestRequeueSelection(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	var otherPID int64
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) SELECT org_id, 'Q', 'q', 'ios' FROM projects WHERE id = $1 RETURNING id`, pid).Scan(&otherPID); err != nil {
		t.Fatal(err)
	}
	const uuid = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"
	raw := iosProfileRaw(t, "x")
	insertProfile(t, pool, pid, "by-uuid", "ios", "2.0", []string{uuid}, raw, time.Now())
	insertProfile(t, pool, pid, "by-version", "ios", "1.0", nil, raw, time.Now())
	insertProfile(t, pool, pid, "other-version", "ios", "3.0", nil, raw, time.Now())
	insertProfile(t, pool, pid, "done", "ios", "1.0", nil, raw, time.Now())
	insertProfile(t, pool, otherPID, "other-project", "ios", "1.0", []string{uuid}, raw, time.Now())
	if _, err := pool.Exec(ctx, `UPDATE app_start_profiles SET status = 'missing_symbols', attempts = 4, next_attempt_at = NULL WHERE start_id <> 'done'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app_start_profiles SET status = 'done', next_attempt_at = NULL WHERE start_id = 'done'`); err != nil {
		t.Fatal(err)
	}

	// Lower-case UUID matches the upper-case stored one.
	if err := Requeue(ctx, pool, pid, "", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"); err != nil {
		t.Fatal(err)
	}
	if err := Requeue(ctx, pool, pid, "1.0", ""); err != nil {
		t.Fatal(err)
	}
	if err := Requeue(ctx, pool, pid, "", ""); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"by-uuid": "pending", "by-version": "pending", "other-version": "missing_symbols", "done": "done",
	}
	for id, status := range want {
		if row := readProfile(t, pool, pid, id); row.status != status {
			t.Errorf("%s status = %q, want %q", id, row.status, status)
		}
	}
	if row := readProfile(t, pool, pid, "by-uuid"); row.attempts != 0 {
		t.Errorf("by-uuid attempts = %d, want 0", row.attempts)
	}
	if row := readProfile(t, pool, otherPID, "other-project"); row.status != "missing_symbols" {
		t.Errorf("other project's profile status = %q, want untouched", row.status)
	}
}

func TestWorkerMarksInvalidRawFailed(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	insertProfile(t, pool, pid, "not-gzip", "android", "1.0.0", nil, []byte("definitely not gzip"), time.Now())
	insertProfile(t, pool, pid, "bad-json", "android", "1.0.0", nil, gzipBytes(t, []byte(`{"profile":`)), time.Now())
	insertProfile(t, pool, pid, "invalid", "android", "1.0.0", nil, gzipBytes(t, []byte(`{"profile":{"v":9}}`)), time.Now())

	if _, err := testWorker(t, pool).ProcessPending(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"not-gzip", "bad-json", "invalid"} {
		row := readProfile(t, pool, pid, id)
		if row.status != "failed" || row.nextAttempt != nil || row.hasSymbols {
			t.Errorf("%s = %+v, want failed with next_attempt_at NULL", id, row)
		}
	}
	// A failed profile is never picked up again.
	if n, err := testWorker(t, pool).ProcessPending(ctx); err != nil || n != 0 {
		t.Errorf("second ProcessPending = %d, %v; want 0, nil", n, err)
	}
}

func TestWorkerSurvivesPanickingProfile(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	now := time.Now()
	// The Android profile needs the loader, which this worker does not have, so it panics.
	insertProfile(t, pool, pid, "boom", "android", "1.0.0", nil, androidProfileRaw(t, "boom"), now.Add(-time.Minute))
	insertProfile(t, pool, pid, "fine", "ios", "1.0.0", nil, iosProfileRaw(t, "fine"), now)

	w := testWorker(t, pool)
	w.loader = nil
	n, err := w.ProcessPending(ctx)
	if err != nil || n != 2 {
		t.Fatalf("ProcessPending = %d, %v; want 2, nil", n, err)
	}
	if row := readProfile(t, pool, pid, "boom"); row.status != "failed" || row.nextAttempt != nil {
		t.Errorf("boom = %+v, want failed", row)
	}
	if row := readProfile(t, pool, pid, "fine"); row.status != "done" {
		t.Errorf("fine status = %q, want done (the batch continues after a panic)", row.status)
	}
}

func TestPurgeExpired(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	pid := seedProject(t, pool)
	raw := androidProfileRaw(t, "x")
	day := 24 * time.Hour
	insertProfile(t, pool, pid, "old", "android", "1.0.0", nil, raw, time.Now().Add(-31*day))
	insertProfile(t, pool, pid, "recent", "android", "1.0.0", nil, raw, time.Now().Add(-29*day))
	for _, s := range []struct {
		id  string
		age time.Duration
	}{{"old", 31 * day}, {"recent", 29 * day}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO app_start_fn_stats (project_id, start_id, platform, environment, app_version, function, is_app, self_ms, total_ms, attributed_ms, started_at)
			VALUES ($1, $2, 'android', 'production', '1.0.0', 'f', true, 1, 1, 1, $3)`,
			pid, s.id, time.Now().Add(-s.age)); err != nil {
			t.Fatal(err)
		}
	}

	n, err := testWorker(t, pool).PurgeExpired(ctx)
	if err != nil || n != 2 {
		t.Fatalf("PurgeExpired = %d, %v; want 2 (one profile, one fn stat), nil", n, err)
	}
	for table, col := range map[string]string{"app_start_profiles": "received_at", "app_start_fn_stats": "started_at"} {
		var ids []string
		rows, err := pool.Query(ctx, `SELECT start_id FROM `+table+` ORDER BY `+col)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if len(ids) != 1 || ids[0] != "recent" {
			t.Errorf("%s keeps %v, want [recent]", table, ids)
		}
	}
}
