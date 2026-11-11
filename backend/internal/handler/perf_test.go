package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// insertPerfBatch writes one session_perf_batches row with samples t=fromMs..toMs step 1000 and cpu=cpuAt(t).
func (e *testEnv) insertPerfBatch(t *testing.T, pid int64, sessionID string, fromMs, toMs int64, cpuAt func(int64) float64) {
	t.Helper()
	var samples []map[string]any
	for ts := fromMs; ts <= toMs; ts += 1000 {
		samples = append(samples, map[string]any{"t": ts, "cpu": cpuAt(ts)})
	}
	raw, _ := json.Marshal(samples)
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO session_perf_batches (project_id, session_id, platform, start_ts, end_ts, interval_ms, device, samples)
		VALUES ($1, $2, 'android', to_timestamp($3/1000.0), to_timestamp($4/1000.0), 1000, '{"cores":8,"refresh_hz":120}', $5)`,
		pid, sessionID, fromMs, toMs, raw)
	if err != nil {
		t.Fatalf("insert perf batch: %v", err)
	}
}

func TestGetSessionPerf(t *testing.T) {
	e := newTestEnv(t)
	uid, authHdr := e.addUser(t, "perfviewer")
	orgID := e.addOrg(t, "acme-perf")
	e.addMember(t, orgID, uid, "owner")
	pid := e.addProject(t, orgID, "perf-app")
	otherPid := e.addProject(t, orgID, "other-app")

	const sess = "ses_perf_0001"
	flat := func(int64) float64 { return 10 }
	e.insertPerfBatch(t, pid, sess, 1000, 3000, flat)
	e.insertPerfBatch(t, pid, sess, 3000, 5000, flat) // overlaps at t=3000 (SDK retry)
	e.insertPerfBatch(t, otherPid, sess, 100000, 102000, flat)

	get := func(path string) (*fiber.Map, int) {
		resp := e.do(t, "GET", path, nil, authHdr)
		var body fiber.Map
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return &body, resp.StatusCode
	}

	body, status := get(fmt.Sprintf("/api/v1/projects/%d/sessions/%s/perf", pid, sess))
	if status != fiber.StatusOK {
		t.Fatalf("status = %d", status)
	}
	samples, _ := (*body)["samples"].([]any)
	if len(samples) != 5 {
		t.Fatalf("want 5 deduped samples from this project only, got %d", len(samples))
	}
	if (*body)["interval_ms"].(float64) != 1000 {
		t.Errorf("interval_ms = %v", (*body)["interval_ms"])
	}
	if metrics, _ := (*body)["metrics"].([]any); len(metrics) != 1 || metrics[0] != "cpu" {
		t.Errorf("metrics = %v, want [cpu]", (*body)["metrics"])
	}
	if (*body)["downsampled"] != false {
		t.Errorf("downsampled = %v", (*body)["downsampled"])
	}
	if dev, _ := (*body)["device"].(map[string]any); dev["cores"] != float64(8) {
		t.Errorf("device = %v", (*body)["device"])
	}

	// A session with no data is not an error: empty, non-null arrays.
	body, status = get(fmt.Sprintf("/api/v1/projects/%d/sessions/%s/perf", pid, "ses_no_data_01"))
	if status != fiber.StatusOK {
		t.Fatalf("empty session status = %d", status)
	}
	if s, ok := (*body)["samples"].([]any); !ok || len(s) != 0 {
		t.Errorf("samples must be an empty array, got %#v", (*body)["samples"])
	}
	if m, ok := (*body)["metrics"].([]any); !ok || len(m) != 0 {
		t.Errorf("metrics must be an empty array, got %#v", (*body)["metrics"])
	}

	if _, status = get(fmt.Sprintf("/api/v1/projects/%d/sessions/%s/perf", pid, "bad!session1")); status != fiber.StatusBadRequest {
		t.Errorf("malformed session id: status = %d, want 400", status)
	}
}

func TestGetSessionPerfRequiresAuth(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, "GET", "/api/v1/projects/1/sessions/ses_perf_0001/perf", nil, nil)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGetSessionPerfReportsEffectiveIntervalWhenDownsampled(t *testing.T) {
	e := newTestEnv(t)
	uid, authHdr := e.addUser(t, "perflong")
	orgID := e.addOrg(t, "acme-perf-long")
	e.addMember(t, orgID, uid, "owner")
	pid := e.addProject(t, orgID, "perf-long")

	const sess = "ses_perf_long1"
	// 2400 one-second samples in one stored row (the table does not cap batch size; the SDK/ingest does).
	e.insertPerfBatch(t, pid, sess, 1000, 2400*1000, func(int64) float64 { return 50 })

	resp := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/sessions/%s/perf", pid, sess), nil, authHdr)
	var body struct {
		IntervalMs  int  `json:"interval_ms"`
		Downsampled bool `json:"downsampled"`
		Samples     []any
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if !body.Downsampled || len(body.Samples) != 1200 {
		t.Fatalf("want 1200 downsampled samples, got %d (downsampled=%v)", len(body.Samples), body.Downsampled)
	}
	if body.IntervalMs != 2000 {
		t.Fatalf("interval_ms must describe the spacing of the returned points: got %d want 2000", body.IntervalMs)
	}
}
