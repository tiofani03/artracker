package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"testing"
	"time"
)

// addTelemetryAt inserts a telemetry row for a session at an absolute timestamp.
func (e *testEnv) addTelemetryAt(t *testing.T, pid int64, session, user, name, level string, ts time.Time) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `
		INSERT INTO telemetry_events(id, project_id, kind, name, level, session_id, user_id, event_timestamp)
		VALUES (gen_random_uuid(), $1, 'event', $2, $3, $4, $5, $6)`,
		pid, name, level, session, user, ts); err != nil {
		t.Fatal(err)
	}
}

type listedSession struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"`
	Environment    string `json:"environment"`
	EventCount     int64  `json:"event_count"`
	ChunkCount     int    `json:"chunk_count"`
	TotalSizeBytes int64  `json:"total_size_bytes"`
	HasError       bool   `json:"has_error"`
	HasRageClick   bool   `json:"has_rage_click"`
	HasDeadClick   bool   `json:"has_dead_click"`
	HasReplay      bool   `json:"has_replay"`
}

// sessionListEnv is a project plus helpers to list its sessions. telemetry_events has no FK to projects, so it is
// not wiped by the per-test TRUNCATE; clear it for the project ids used here (ids restart at 1 on every test).
type sessionListEnv struct {
	*testEnv
	pid, other int64
	hdr        map[string]string
}

func newSessionListEnv(t *testing.T, tag string) *sessionListEnv {
	t.Helper()
	e := newTestEnv(t)
	uid, hdr := e.addUser(t, "sl"+tag)
	orgID := e.addOrg(t, "acme-sl-"+tag)
	e.addMember(t, orgID, uid, "owner")
	pid := e.addProject(t, orgID, "sl-app")
	other := e.addProject(t, orgID, "sl-other")
	clean := func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM telemetry_events WHERE project_id = ANY($1)`, []int64{pid, other})
	}
	clean()
	t.Cleanup(clean)
	return &sessionListEnv{testEnv: e, pid: pid, other: other, hdr: hdr}
}

func (s *sessionListEnv) list(t *testing.T, query string) ([]listedSession, int64) {
	t.Helper()
	resp := s.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/sessions?%s", s.pid, query), nil, s.hdr)
	if resp.StatusCode != 200 {
		t.Fatalf("%s: status %d", query, resp.StatusCode)
	}
	var body struct {
		Items []listedSession `json:"items"`
		Total int64           `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Items, body.Total
}

func (s *sessionListEnv) ids(t *testing.T, query string) []string {
	t.Helper()
	items, total := s.list(t, query)
	out := []string{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	sort.Strings(out)
	if total != int64(len(out)) {
		t.Fatalf("%s: total %d != %d items", query, total, len(out))
	}
	return out
}

func wantIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestListSessionsIncludesTelemetryOnly(t *testing.T) {
	s := newSessionListEnv(t, "tel")
	now := time.Now()
	s.addTelemetryAt(t, s.pid, "ses_tel_0001", "budi", "screen_view", "", now.Add(-3*time.Minute))
	s.addTelemetryAt(t, s.pid, "ses_tel_0001", "", "tap", "error", now.Add(-2*time.Minute))
	s.addTelemetryAt(t, s.pid, "ses_tel_0001", "", "tap", "", now.Add(-1*time.Minute))

	items, total := s.list(t, "")
	if total != 1 || len(items) != 1 {
		t.Fatalf("total %d items %d, want 1/1: %+v", total, len(items), items)
	}
	it := items[0]
	if it.ID != "ses_tel_0001" || it.HasReplay || it.UserID != "budi" || it.EventCount != 3 ||
		!it.HasError || it.ChunkCount != 0 || it.TotalSizeBytes != 0 || it.Environment != "production" {
		t.Fatalf("unexpected item: %+v", it)
	}
}

func TestListSessionsNoDuplicateWhenReplayExists(t *testing.T) {
	s := newSessionListEnv(t, "dup")
	s.addSessionForUser(t, s.pid, "ses_rep_0001", "alice", false)
	if _, err := s.pool.Exec(context.Background(), `UPDATE replay_sessions SET chunk_count = 2 WHERE id = 'ses_rep_0001'`); err != nil {
		t.Fatal(err)
	}
	s.addTelemetryAt(t, s.pid, "ses_rep_0001", "bob", "tap", "", time.Now().Add(-time.Minute))

	items, total := s.list(t, "")
	if total != 1 || len(items) != 1 {
		t.Fatalf("total %d items %d, want exactly one: %+v", total, len(items), items)
	}
	if !items[0].HasReplay || items[0].UserID != "alice" || items[0].ChunkCount != 2 {
		t.Fatalf("replay row must win: %+v", items[0])
	}
}

func TestListSessionsHasReplayFalseWhenNoChunks(t *testing.T) {
	s := newSessionListEnv(t, "nochunk")
	s.addSessionForUser(t, s.pid, "ses_rep_0002", "alice", false) // chunk_count defaults to 0

	items, _ := s.list(t, "")
	if len(items) != 1 || items[0].HasReplay {
		t.Fatalf("want one item with has_replay=false: %+v", items)
	}
}

func TestListSessionsCrashOnlySession(t *testing.T) {
	s := newSessionListEnv(t, "crash")
	issue := s.mcpAddIssue(t, s.pid, "NPE", "crash", "open", 1)
	s.mcpAddEvent(t, issue, mcpTestEvent{user: "carol", session: "ses_crash_001", ageMins: 5})

	items, _ := s.list(t, "")
	if len(items) != 1 || items[0].ID != "ses_crash_001" {
		t.Fatalf("want the crash session: %+v", items)
	}
	if !items[0].HasError || items[0].HasReplay || items[0].UserID != "carol" || items[0].EventCount != 1 || items[0].Environment != "production" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
	wantIDs(t, s.ids(t, "has_error=true"), "ses_crash_001")
}

func TestListSessionsTelemetryFilters(t *testing.T) {
	s := newSessionListEnv(t, "filt")
	now := time.Now()
	s.addTelemetryAt(t, s.pid, "ses_rage_001", "budi", "rage_tap", "", now.Add(-time.Minute))
	s.addTelemetryAt(t, s.pid, "ses_dead_001", "Citra", "dead_click", "", now.Add(-time.Minute))
	s.addTelemetryAt(t, s.pid, "ses_plain_01", "andi", "tap", "", now.Add(-time.Minute))
	s.addSessionForUser(t, s.pid, "ses_rep_0003", "budi2", false)

	wantIDs(t, s.ids(t, "has_rage_click=true"), "ses_rage_001")
	wantIDs(t, s.ids(t, "has_dead_click=true"), "ses_dead_001")
	wantIDs(t, s.ids(t, "user_prefix=bu"), "ses_rage_001", "ses_rep_0003")
	wantIDs(t, s.ids(t, "user_prefix=citr"), "ses_dead_001") // case-insensitive
	wantIDs(t, s.ids(t, "user_prefix="+url.QueryEscape("%")))
	wantIDs(t, s.ids(t, "user_id=budi"), "ses_rage_001")
	wantIDs(t, s.ids(t, "has_error=true"))
	wantIDs(t, s.ids(t, "environment=staging"))
	wantIDs(t, s.ids(t, "environment=production"), "ses_dead_001", "ses_plain_01", "ses_rage_001", "ses_rep_0003")

	// to= applies to start_time of the merged set: the replay row starts "now", the telemetry sessions a minute ago
	to := url.QueryEscape(now.Add(-30 * time.Second).UTC().Format(time.RFC3339))
	wantIDs(t, s.ids(t, "to="+to), "ses_dead_001", "ses_plain_01", "ses_rage_001")
}

func TestListSessionsTelemetryWindow(t *testing.T) {
	s := newSessionListEnv(t, "win")
	s.addTelemetryAt(t, s.pid, "ses_old_0001", "budi", "tap", "", time.Now().Add(-8*24*time.Hour))

	wantIDs(t, s.ids(t, ""))
	from := time.Now().Add(-9 * 24 * time.Hour).UTC().Format(time.RFC3339)
	wantIDs(t, s.ids(t, "from="+url.QueryEscape(from)), "ses_old_0001")
}

func TestListSessionsTelemetryOtherProject(t *testing.T) {
	s := newSessionListEnv(t, "otherp")
	s.addTelemetryAt(t, s.other, "ses_oth_0001", "budi", "tap", "", time.Now().Add(-time.Minute))
	issue := s.mcpAddIssue(t, s.other, "NPE", "crash", "open", 1)
	s.mcpAddEvent(t, issue, mcpTestEvent{session: "ses_oth_0002", ageMins: 5})

	wantIDs(t, s.ids(t, ""))
}

func TestListSessionsIgnoresReplayChunkEvents(t *testing.T) {
	s := newSessionListEnv(t, "chunkev")
	s.addTelemetryAt(t, s.pid, "ses_chk_0001", "budi", "replay_chunk", "", time.Now().Add(-time.Minute))

	wantIDs(t, s.ids(t, ""))
}
