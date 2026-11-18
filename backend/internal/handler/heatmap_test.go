package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func (e *testEnv) addVisit(t *testing.T, pid int64, sid, screen, platform string, aspect float64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO screen_visits(project_id, session_id, screen, ts, platform, aspect_bucket)
		 VALUES ($1,$2,$3,now(),$4,$5)`, pid, sid, screen, platform, aspect); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) addTap(t *testing.T, pid int64, sid, screen, platform string, aspect, x, y float64, rage bool) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO screen_taps(project_id, session_id, screen, ts, x_norm, y_norm, vw, vh, aspect_bucket, platform, rage)
		 VALUES ($1,$2,$3,now(),$4,$5,400,880,$6,$7,$8)`, pid, sid, screen, x, y, aspect, platform, rage); err != nil {
		t.Fatal(err)
	}
}

func hmGet(t *testing.T, e *testEnv, hdr map[string]string, path string, out any) int {
	t.Helper()
	resp := e.do(t, "GET", path, nil, hdr)
	if out != nil && resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func TestHeatmapScreensList(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "hm-list")
	uid, hdr := e.addUser(t, "hmviewer")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "hm-app")
	for _, s := range []string{"s1", "s2", "s3"} {
		e.addReplaySession(t, pid, s, "1 hour", false, false, false)
	}
	e.addVisit(t, pid, "s1", "Home", "android", 2.2)
	e.addVisit(t, pid, "s2", "Home", "android", 2.2)
	e.addVisit(t, pid, "s3", "Cart", "ios", 2.2)
	e.addTap(t, pid, "s1", "Home", "android", 2.2, 0.5, 0.5, false)
	for sid, v := range map[string]string{"s1": "4.57.0", "s2": "4.58.0", "s3": "4570001"} {
		if _, err := e.pool.Exec(context.Background(), `UPDATE screen_visits SET app_version=$3 WHERE project_id=$1 AND session_id=$2`, pid, sid, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE replay_sessions SET taps_unrecognized = 4 WHERE project_id=$1 AND id='s1'`, pid); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Screens []struct {
			Screen   string `json:"screen"`
			Platform string `json:"platform"`
			Sessions int    `json:"sessions"`
			Taps     int    `json:"taps"`
		} `json:"screens"`
		Unrecognized int      `json:"unrecognized_taps"`
		AppVersions  []string `json:"app_versions"`
	}
	base := fmt.Sprintf("/api/v1/projects/%d/heatmap/screens", pid)
	if st := hmGet(t, e, hdr, base, &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	if len(got.Screens) != 2 || got.Screens[0].Screen != "Home" || got.Screens[0].Sessions != 2 || got.Screens[0].Taps != 1 || got.Screens[1].Screen != "Cart" {
		t.Fatalf("unexpected screens: %+v", got.Screens)
	}
	if got.Unrecognized != 4 {
		t.Errorf("unrecognized = %d, want 4", got.Unrecognized)
	}
	if want := []string{"4570001", "4.58.0", "4.57.0"}; !reflect.DeepEqual(got.AppVersions, want) {
		t.Errorf("app_versions = %v, want %v", got.AppVersions, want)
	}
	got.Screens, got.AppVersions = nil, nil
	hmGet(t, e, hdr, base+"?platform=ios", &got)
	if len(got.Screens) != 1 || got.Screens[0].Screen != "Cart" {
		t.Fatalf("platform filter: %+v", got.Screens)
	}
	if len(got.AppVersions) != 1 || got.AppVersions[0] != "4570001" {
		t.Errorf("platform-filtered app_versions = %v", got.AppVersions)
	}
	if st := hmGet(t, e, hdr, base+"?from=garbage", nil); st != 400 {
		t.Errorf("bad from = %d, want 400", st)
	}
}

type hmGrid struct {
	Cols  int `json:"cols"`
	Rows  int `json:"rows"`
	Cells []struct {
		C        int `json:"c"`
		R        int `json:"r"`
		Sessions int `json:"sessions"`
		Taps     int `json:"taps"`
	} `json:"cells"`
	Rage           []struct{ X, Y float64 } `json:"rage"`
	ScreenSessions int                      `json:"screen_sessions"`
	Aspect         float64                  `json:"aspect"`
	Aspects        []struct {
		Aspect   float64 `json:"aspect"`
		Sessions int     `json:"sessions"`
	} `json:"aspects"`
	Background *struct {
		URL string `json:"url"`
	} `json:"background"`
}

func TestHeatmapGrid(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "hm-grid")
	uid, hdr := e.addUser(t, "hmgridviewer")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "hm-grid-app")
	for _, s := range []string{"a", "b", "c", "d"} {
		e.addReplaySession(t, pid, s, "1 hour", false, false, false)
	}
	for _, s := range []string{"a", "b", "c"} {
		e.addVisit(t, pid, s, "Home", "android", 2.2)
	}
	e.addVisit(t, pid, "d", "Home", "android", 1.8)
	e.addTap(t, pid, "a", "Home", "android", 2.2, 0.5, 0.5, false)
	e.addTap(t, pid, "a", "Home", "android", 2.2, 0.5, 0.5, true)
	e.addTap(t, pid, "b", "Home", "android", 2.2, 0.5, 0.5, false)
	e.addTap(t, pid, "d", "Home", "android", 1.8, 0.1, 0.1, false)

	var g hmGrid
	path := fmt.Sprintf("/api/v1/projects/%d/heatmap?screen=Home&platform=android&orientation=portrait", pid)
	if st := hmGet(t, e, hdr, path, &g); st != 200 {
		t.Fatalf("status %d", st)
	}
	if g.Cols != 40 || g.Aspect != 2.2 || g.ScreenSessions != 3 {
		t.Fatalf("cols=%d aspect=%v screen_sessions=%d", g.Cols, g.Aspect, g.ScreenSessions)
	}
	if len(g.Aspects) != 2 || g.Aspects[0].Aspect != 2.2 || g.Aspects[0].Sessions != 3 {
		t.Fatalf("aspects: %+v", g.Aspects)
	}
	if len(g.Cells) != 1 || g.Cells[0].C != 20 || g.Cells[0].R != g.Rows/2 || g.Cells[0].Sessions != 2 || g.Cells[0].Taps != 3 {
		t.Fatalf("cells: %+v rows=%d", g.Cells, g.Rows)
	}
	if len(g.Rage) != 1 || g.Background != nil {
		t.Fatalf("rage=%v background=%v", g.Rage, g.Background)
	}
}

func TestHeatmapRequiresParams(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "hm-req")
	uid, hdr := e.addUser(t, "hmreqviewer")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "hm-req-app")
	for _, q := range []string{"screen=Home&platform=ios", "screen=Home&orientation=portrait", "platform=ios&orientation=portrait", "screen=Home&platform=ios&orientation=sideways"} {
		if st := hmGet(t, e, hdr, fmt.Sprintf("/api/v1/projects/%d/heatmap?%s", pid, q), nil); st != 400 {
			t.Errorf("%s: status %d, want 400", q, st)
		}
	}
}

func TestHeatmapSettingsExcludeDeletesRows(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "hm-set")
	aid, ahdr := e.addUser(t, "hmadmin")
	e.addMember(t, org, aid, "admin")
	vid, vhdr := e.addUser(t, "hmsetviewer")
	e.addMember(t, org, vid, "viewer")
	pid := e.addProject(t, org, "hm-set-app")
	e.addReplaySession(t, pid, "s1", "1 hour", false, false, false)
	e.addVisit(t, pid, "s1", "Home", "ios", 2.2)
	e.addVisit(t, pid, "s1", "Keep", "ios", 2.2)
	e.addTap(t, pid, "s1", "Home", "ios", 2.2, 0.5, 0.5, false)
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO screen_backgrounds(project_id, screen, platform, aspect_bucket, session_id, chunk_seq, frame_index, vw, vh, captured_at)
		 VALUES ($1,'Home','ios',2.2,'s1',0,0,400,880,now())`, pid); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/projects/%d/heatmap-settings", pid)

	if resp := e.do(t, "PATCH", path, strings.NewReader(`{"excluded_screens":["Home"]}`), vhdr); resp.StatusCode != 403 {
		t.Fatalf("viewer PATCH = %d, want 403", resp.StatusCode)
	}
	hdr := map[string]string{"Content-Type": "application/json"}
	for k, v := range ahdr {
		hdr[k] = v
	}
	resp := e.do(t, "PATCH", path, strings.NewReader(`{"excluded_screens":[" Home ","Home",""]}`), hdr)
	if resp.StatusCode != 200 {
		t.Fatalf("admin PATCH = %d", resp.StatusCode)
	}
	for _, tbl := range []string{"screen_taps", "screen_visits", "screen_backgrounds"} {
		if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM %s WHERE screen='Home'`, tbl)); n != 0 {
			t.Errorf("%s still has %d Home rows", tbl, n)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM screen_visits WHERE screen='Keep'`); n != 1 {
		t.Errorf("Keep visits = %d, want 1", n)
	}
	var s struct {
		Excluded []string `json:"excluded_screens"`
		Rule     string   `json:"sensitive_rule"`
	}
	if st := hmGet(t, e, vhdr, path, &s); st != 200 || len(s.Excluded) != 1 || s.Excluded[0] != "Home" || s.Rule == "" {
		t.Fatalf("GET settings: %d %+v", st, s)
	}
}

func TestHeatmapSettingsValidation(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "hm-val")
	aid, ahdr := e.addUser(t, "hmvaladmin")
	e.addMember(t, org, aid, "admin")
	pid := e.addProject(t, org, "hm-val-app")
	hdr := map[string]string{"Content-Type": "application/json"}
	for k, v := range ahdr {
		hdr[k] = v
	}
	path := fmt.Sprintf("/api/v1/projects/%d/heatmap-settings", pid)
	var many []string
	for i := 0; i < 201; i++ {
		many = append(many, fmt.Sprintf("S%d", i))
	}
	b, _ := json.Marshal(map[string]any{"excluded_screens": many})
	if resp := e.do(t, "PATCH", path, strings.NewReader(string(b)), hdr); resp.StatusCode != 400 {
		t.Errorf("201 entries = %d, want 400", resp.StatusCode)
	}
	b, _ = json.Marshal(map[string]any{"excluded_screens": []string{strings.Repeat("x", 201)}})
	if resp := e.do(t, "PATCH", path, strings.NewReader(string(b)), hdr); resp.StatusCode != 400 {
		t.Errorf("long entry = %d, want 400", resp.StatusCode)
	}
}
