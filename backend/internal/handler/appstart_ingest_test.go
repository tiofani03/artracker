package handler

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

const profileTestKey = "art_live_profile_test_key_0123456789abcdef0123"

// newProfileEnv builds a DB-backed app with one project and an ingest key seeded for it.
func newProfileEnv(t *testing.T) (*fiber.App, *pgxpool.Pool, int64) {
	t.Helper()
	pool := testDB(t)
	ctx := context.Background()
	var orgID, projectID int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Prof','prof-org') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'App','prof-app','android') RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	svc := ingest.NewService(ingest.Config{Workers: 1, QueueSize: 10, BatchSize: 10, FlushIntervalMs: 50,
		ProjectRPS: 100, ProjectEventsPS: 5000, KeyCacheTTLSec: 60}, nil, ingest.NewMemoryStorageSink())
	svc.KeyCache().(*ingest.MemoryKeyCache).SeedKey(profileTestKey, ingest.KeyInfo{ProjectID: projectID, KeyType: auth.KeyPublicClient}, time.Hour)
	h := New(pool, config.Config{Env: "development", JWTSecret: testSecret}, auth.NewIssuer(testSecret, time.Hour), map[string]OAuthProvider{}).WithIngest(svc)
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	h.Routes(app.Group("/api/v1"))
	return app, pool, projectID
}

func profileJSON(startID, context string) string {
	return `{"sdk":{"name":"artracker-android","version":"0.4"},"context":` + context + `,
"profile":{"v":1,"start_id":"` + startID + `","platform":"android","interval_ms":10,"t0":"process_start",
"frames":[{"c":"a.B","m":"c","l":1,"f":"B.kt"}],"stacks":[[0]],"samples":[{"t":10,"s":0}]}}`
}

const androidCtx = `{"app_version":"3.2.0","build":"1203","environment":"production"}`

func postProfile(t *testing.T, app *fiber.App, key string, body []byte, gz bool) int {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/app-start/profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if gz {
		req.Header.Set("Content-Encoding", "gzip")
	}
	if key != "" {
		req.Header.Set("X-Artracker-Key", key)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == fiber.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), `"status":"accepted"`) {
			t.Fatalf("body = %s", b)
		}
	}
	return resp.StatusCode
}

func TestIngestAppStartProfileStoresGzip(t *testing.T) {
	app, pool, projectID := newProfileEnv(t)
	plain := []byte(profileJSON("start-1", androidCtx))

	// plain JSON body is gzipped before it is stored
	if code := postProfile(t, app, profileTestKey, plain, false); code != 202 {
		t.Fatalf("plain status = %d, want 202", code)
	}
	// gzip body is stored as sent
	if code := postProfile(t, app, profileTestKey, gzipCompress(t, []byte(profileJSON("start-2", androidCtx))), true); code != 202 {
		t.Fatalf("gzip status = %d, want 202", code)
	}

	rows, err := pool.Query(context.Background(),
		`SELECT start_id, platform, environment, app_version, build, status, raw FROM app_start_profiles WHERE project_id=$1 ORDER BY start_id`, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var startID, platform, env, version, build, status string
		var raw []byte
		if err := rows.Scan(&startID, &platform, &env, &version, &build, &status, &raw); err != nil {
			t.Fatal(err)
		}
		n++
		if status != "pending" || platform != "android" || env != "production" || version != "3.2.0" || build != "1203" {
			t.Errorf("row %s = %s/%s/%s/%s/%s", startID, platform, env, version, build, status)
		}
		if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
			t.Errorf("row %s raw does not start with gzip magic: % x", startID, raw[:min(4, len(raw))])
		}
		if out, err := ingest.NewDecompressor().Gunzip(raw); err != nil || !strings.Contains(string(out), `"start_id":"`+startID+`"`) {
			t.Errorf("row %s raw does not round-trip: %v", startID, err)
		}
	}
	if n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
}

func TestIngestAppStartProfileDuplicateIs202(t *testing.T) {
	app, pool, projectID := newProfileEnv(t)
	body := []byte(profileJSON("dup-1", androidCtx))
	for i := 0; i < 2; i++ {
		if code := postProfile(t, app, profileTestKey, body, false); code != 202 {
			t.Fatalf("post %d status = %d, want 202", i, code)
		}
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM app_start_profiles WHERE project_id=$1`, projectID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}

func TestIngestAppStartProfileTooLarge(t *testing.T) {
	app, _, _ := newProfileEnv(t)

	// gzip bomb: tiny on the wire, 5 MB uncompressed
	if code := postProfile(t, app, profileTestKey, gzipCompress(t, make([]byte, 5<<20)), true); code != 413 {
		t.Fatalf("bomb status = %d, want 413", code)
	}
	// compressed body above 1 MB (incompressible data)
	noise := make([]byte, 1<<20+1024)
	seed := uint32(1)
	for i := range noise {
		seed = seed*1664525 + 1013904223
		noise[i] = byte(seed >> 24)
	}
	if code := postProfile(t, app, profileTestKey, gzipCompress(t, noise), true); code != 413 {
		t.Fatalf("large compressed status = %d, want 413", code)
	}
}

func TestIngestAppStartProfileBadKey(t *testing.T) {
	app, _, _ := newProfileEnv(t)
	body := []byte(profileJSON("k-1", androidCtx))
	if code := postProfile(t, app, "", body, false); code != 401 {
		t.Fatalf("missing key = %d, want 401", code)
	}
	if code := postProfile(t, app, "art_live_unknown_key_value", body, false); code != 401 {
		t.Fatalf("unknown key = %d, want 401", code)
	}
}

func TestIngestAppStartProfileRejectsInvalid(t *testing.T) {
	app, pool, projectID := newProfileEnv(t)
	for name, body := range map[string]string{
		"empty":        ``,
		"not json":     `nope`,
		"bad version":  strings.Replace(profileJSON("v-1", androidCtx), `"v":1`, `"v":9`, 1),
		"bad stack":    strings.Replace(profileJSON("v-2", androidCtx), `"stacks":[[0]]`, `"stacks":[[7]]`, 1),
		"nul start_id": strings.Replace(profileJSON("v-3", androidCtx), `"v-3"`, `"\u0000"`, 1),
	} {
		if code := postProfile(t, app, profileTestKey, []byte(body), false); code != 400 {
			t.Errorf("%s status = %d, want 400", name, code)
		}
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM app_start_profiles WHERE project_id=$1`, projectID).Scan(&n)
	if n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}

func TestIngestAppStartProfileIOSVersion(t *testing.T) {
	app, pool, projectID := newProfileEnv(t)
	body := `{"sdk":{"name":"artracker-ios","version":"0.4"},"context":{"app_version":"1.4.0 (217)","environment":"staging"},
"profile":{"v":1,"start_id":"ios-1","platform":"ios","interval_ms":10,"t0":"process_start",
"frames":[{"a":"0x1045f3a20","i":0}],"images":[{"uuid":"e2a1b3c4d5e6f7089a0b1c2d3e4f5a6b","name":"MyApp","load":"0x104000000","app":true}],
"stacks":[[0]],"samples":[{"t":10,"s":0}]}}`
	if code := postProfile(t, app, profileTestKey, []byte(body), false); code != 202 {
		t.Fatalf("status = %d, want 202", code)
	}
	var version, build, env string
	var uuids []string
	if err := pool.QueryRow(context.Background(),
		`SELECT app_version, build, environment, image_uuids FROM app_start_profiles WHERE project_id=$1 AND start_id='ios-1'`, projectID).
		Scan(&version, &build, &env, &uuids); err != nil {
		t.Fatal(err)
	}
	if version != "1.4.0" || build != "217" || env != "staging" {
		t.Fatalf("version/build/env = %q/%q/%q", version, build, env)
	}
	if len(uuids) != 1 || uuids[0] != "E2A1B3C4-D5E6-F708-9A0B-1C2D3E4F5A6B" {
		t.Fatalf("image_uuids = %v", uuids)
	}
}

func TestIngestAppStartProfileSanitizesStoredStrings(t *testing.T) {
	app, pool, projectID := newProfileEnv(t)
	ctxJSON := `{"app_version":"3.\u00002.0","build":"12\u000003","environment":"pro\u0000d"}`
	if code := postProfile(t, app, profileTestKey, []byte(profileJSON("san\\u0000-1", ctxJSON)), false); code != 202 {
		t.Fatalf("status = %d, want 202", code)
	}
	var startID, version, build, env string
	if err := pool.QueryRow(context.Background(),
		`SELECT start_id, app_version, build, environment FROM app_start_profiles WHERE project_id=$1`, projectID).
		Scan(&startID, &version, &build, &env); err != nil {
		t.Fatal(err)
	}
	if startID != "san-1" || version != "3.2.0" || build != "1203" || env != "prod" {
		t.Fatalf("stored %q/%q/%q/%q", startID, version, build, env)
	}
}
