package handler

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type flowPathsResp struct {
	Screen    string `json:"screen"`
	Direction string `json:"direction"`
	Sessions  int    `json:"sessions"`
	Nodes     []struct {
		ID     string `json:"id"`
		Step   int    `json:"step"`
		Screen string `json:"screen"`
	} `json:"nodes"`
	Links []struct {
		Source   string `json:"source"`
		Target   string `json:"target"`
		Sessions int    `json:"sessions"`
	} `json:"links"`
}

func flowLinkMap(links []flowLink) map[string]int {
	m := map[string]int{}
	for _, l := range links {
		m[l.Source+">"+l.Target] = l.Sessions
	}
	return m
}

func TestBuildFlowPathsCountsAndExit(t *testing.T) {
	nodes, links := buildFlowPaths([][]string{
		{"Home", "Cart", "Exit"},
		{"Home", "Exit"},
		{"Home", "Cart", "Pay"},
	}, 6)
	want := map[string]int{"0:Home>1:Cart": 2, "0:Home>1:Exit": 1, "1:Cart>2:Exit": 1, "1:Cart>2:Pay": 1}
	if got := flowLinkMap(links); !reflect.DeepEqual(got, want) {
		t.Errorf("links = %v, want %v", got, want)
	}
	if len(links) != len(want) {
		t.Errorf("duplicate links: %+v", links)
	}
	for _, l := range links {
		if l.Source == "1:Exit" || l.Source == "2:Exit" {
			t.Errorf("link out of Exit: %+v", l)
		}
	}
	ids := []string{}
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	if wantIDs := []string{"0:Home", "1:Cart", "1:Exit", "2:Pay", "2:Exit"}; !reflect.DeepEqual(ids, wantIDs) {
		t.Errorf("node order = %v, want %v", ids, wantIDs)
	}
}

func TestBuildFlowPathsOther(t *testing.T) {
	var paths [][]string
	for i := 1; i <= 8; i++ {
		for j := 0; j < 9-i; j++ { // S1 x8 ... S8 x1
			paths = append(paths, []string{"Home", fmt.Sprintf("S%d", i)})
		}
	}
	paths = append(paths, []string{"Home", "Exit"})
	nodes, links := buildFlowPaths(paths, 6)
	m := flowLinkMap(links)
	for i := 1; i <= 6; i++ {
		if got, want := m[fmt.Sprintf("0:Home>1:S%d", i)], 9-i; got != want {
			t.Errorf("S%d = %d, want %d", i, got, want)
		}
	}
	if m["0:Home>1:Other"] != 3 { // S7 (2) + S8 (1)
		t.Errorf("Other = %d, want 3", m["0:Home>1:Other"])
	}
	if m["0:Home>1:Exit"] != 1 {
		t.Errorf("Exit = %d, want 1", m["0:Home>1:Exit"])
	}
	if _, ok := m["0:Home>1:S7"]; ok {
		t.Error("S7 should be folded into Other")
	}
	step1 := 0
	for _, n := range nodes {
		if n.Step == 1 {
			step1++
		}
	}
	if step1 != 8 { // 6 screens + Other + Exit
		t.Errorf("step 1 nodes = %d, want 8", step1)
	}
}

func TestBuildFlowPathsOtherContinues(t *testing.T) {
	var paths [][]string
	for i := 1; i <= 8; i++ {
		for j := 0; j < 9-i; j++ {
			next := "Done"
			if i >= 7 {
				next = "Late"
			}
			paths = append(paths, []string{"Home", fmt.Sprintf("S%d", i), next})
		}
	}
	_, links := buildFlowPaths(paths, 6)
	m := flowLinkMap(links)
	if m["1:Other>2:Late"] != 3 {
		t.Errorf("1:Other>2:Late = %d, want 3 (links: %v)", m["1:Other>2:Late"], m)
	}
	if m["1:S1>2:Done"] != 8 {
		t.Errorf("1:S1>2:Done = %d, want 8", m["1:S1>2:Done"])
	}
}

func TestFlowPathsNext(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "flow-paths")
	uid, hdr := e.addUser(t, "flowpathsviewer")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "flow-paths-app")
	for _, s := range []string{"s1", "s2"} {
		e.addReplaySession(t, pid, s, "1 hour", false, false, false)
	}
	e.addView(t, pid, "s1", 0, "Home", "android", 1000, true, false)
	e.addView(t, pid, "s1", 1, "Cart", "android", 1000, false, true)
	e.addView(t, pid, "s2", 0, "Login", "android", 1000, true, false)
	e.addView(t, pid, "s2", 1, "Home", "android", 1000, false, false)
	e.addView(t, pid, "s2", 2, "Cart", "android", 1000, false, false)
	e.addView(t, pid, "s2", 3, "Home", "android", 1000, false, true)

	var got flowPathsResp
	base := fmt.Sprintf("/api/v1/projects/%d/flow/paths?screen=Home&platform=android", pid)
	if st := hmGet(t, e, hdr, base, &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	if got.Screen != "Home" || got.Direction != "next" || got.Sessions != 2 {
		t.Errorf("header = %+v", got)
	}
	m := map[string]int{}
	for _, l := range got.Links {
		m[l.Source+">"+l.Target] = l.Sessions
	}
	want := map[string]int{"0:Home>1:Cart": 2, "1:Cart>2:Exit": 1, "1:Cart>2:Home": 1, "2:Home>3:Exit": 1}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("links = %v, want %v", m, want)
	}
	if len(got.Nodes) == 0 || got.Nodes[0].ID != "0:Home" || got.Nodes[0].Step != 0 || got.Nodes[0].Screen != "Home" {
		t.Errorf("nodes = %+v", got.Nodes)
	}

	// steps=1 truncates: only the first hop.
	got = flowPathsResp{}
	hmGet(t, e, hdr, base+"&steps=1", &got)
	if len(got.Links) != 1 || got.Links[0].Source != "0:Home" || got.Links[0].Target != "1:Cart" || got.Links[0].Sessions != 2 {
		t.Errorf("steps=1 links = %+v", got.Links)
	}

	// Other platform has no such screen.
	got = flowPathsResp{}
	hmGet(t, e, hdr, fmt.Sprintf("/api/v1/projects/%d/flow/paths?screen=Home&platform=ios", pid), &got)
	if got.Sessions != 0 || got.Nodes == nil || got.Links == nil || len(got.Nodes) != 0 || len(got.Links) != 0 {
		t.Errorf("empty result = %+v", got)
	}
}

func TestFlowPathsPrev(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "flow-prev")
	uid, hdr := e.addUser(t, "flowprevviewer")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "flow-prev-app")
	e.addReplaySession(t, pid, "s1", "1 hour", false, false, false)
	e.addView(t, pid, "s1", 0, "Home", "android", 1000, true, false)
	e.addView(t, pid, "s1", 1, "Cart", "android", 1000, false, true)

	var got flowPathsResp
	base := fmt.Sprintf("/api/v1/projects/%d/flow/paths?screen=Cart&platform=android&direction=prev", pid)
	if st := hmGet(t, e, hdr, base, &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	if got.Direction != "prev" || got.Sessions != 1 {
		t.Errorf("header = %+v", got)
	}
	m := map[string]int{}
	for _, l := range got.Links {
		m[l.Source+">"+l.Target] = l.Sessions
	}
	want := map[string]int{"0:Cart>1:Home": 1, "1:Home>2:Start": 1}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("links = %v, want %v", m, want)
	}
}

func TestFlowPathsValidation(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "flow-paths-val")
	uid, hdr := e.addUser(t, "flowpathsval")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "flow-paths-val-app")
	base := fmt.Sprintf("/api/v1/projects/%d/flow/paths", pid)
	for _, q := range []string{
		"?platform=android",
		"?screen=Home",
		"?screen=Home&platform=android&steps=0",
		"?screen=Home&platform=android&steps=6",
		"?screen=Home&platform=android&steps=x",
		"?screen=Home&platform=android&direction=up",
		"?screen=Home&platform=android&from=garbage",
	} {
		if st := hmGet(t, e, hdr, base+q, nil); st != 400 {
			t.Errorf("%s = %d, want 400", q, st)
		}
	}
}

// addView inserts one screen_views row for an existing replay session. entered_at is shortly before now
// so default date ranges include it (later seq = more recent).
func (e *testEnv) addView(t *testing.T, pid int64, sid string, seq int, screen, platform string, durMS int, entry, exit bool) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO screen_views(project_id, session_id, seq, screen, entered_at, duration_ms, is_entry, is_exit, platform)
		 VALUES ($1,$2,$3,$4, now() - ($5 * interval '1 second'), $6,$7,$8,$9)`,
		pid, sid, seq, screen, 60-seq, durMS, entry, exit, platform)
	if err != nil {
		t.Fatal(err)
	}
}

type flowScreensResp struct {
	Screens []struct {
		Screen        string `json:"screen"`
		Platform      string `json:"platform"`
		Sessions      int    `json:"sessions"`
		Views         int    `json:"views"`
		MedianMS      int    `json:"median_ms"`
		P75MS         int    `json:"p75_ms"`
		EntrySessions int    `json:"entry_sessions"`
		ExitSessions  int    `json:"exit_sessions"`
	} `json:"screens"`
	AppVersions  []string `json:"app_versions"`
	Unrecognized int      `json:"unrecognized_sessions"`
}

func TestFlowScreensList(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "flow-list")
	uid, hdr := e.addUser(t, "flowviewer")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "flow-app")
	for _, s := range []string{"s1", "s2", "s3"} {
		e.addReplaySession(t, pid, s, "1 hour", false, false, false)
	}
	e.addView(t, pid, "s1", 0, "Home", "android", 1000, true, false)
	e.addView(t, pid, "s1", 1, "Cart", "android", 3000, false, true)
	e.addView(t, pid, "s2", 0, "Home", "android", 2000, true, false)
	e.addView(t, pid, "s2", 1, "Cart", "android", 5000, false, false)
	e.addView(t, pid, "s2", 2, "Home", "android", 10000, false, true)
	if _, err := e.pool.Exec(context.Background(), `UPDATE replay_sessions SET taps_processed_at = now() WHERE project_id=$1 AND id='s3'`, pid); err != nil {
		t.Fatal(err)
	}

	var got flowScreensResp
	base := fmt.Sprintf("/api/v1/projects/%d/flow/screens", pid)
	if st := hmGet(t, e, hdr, base, &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	if len(got.Screens) != 2 || got.Screens[0].Screen != "Home" || got.Screens[1].Screen != "Cart" {
		t.Fatalf("unexpected screens: %+v", got.Screens)
	}
	home, cart := got.Screens[0], got.Screens[1]
	if home.Sessions != 2 || home.Views != 3 || home.EntrySessions != 2 || home.ExitSessions != 1 || home.Platform != "android" {
		t.Errorf("home = %+v", home)
	}
	// percentile_cont over {1000,2000,10000}: median 2000, p75 = 2000 + 0.5*8000 = 6000
	if home.MedianMS != 2000 || home.P75MS != 6000 {
		t.Errorf("home median/p75 = %d/%d, want 2000/6000", home.MedianMS, home.P75MS)
	}
	// {3000,5000}: median 4000, p75 = 3000 + 0.75*2000 = 4500
	if cart.Sessions != 2 || cart.Views != 2 || cart.ExitSessions != 1 || cart.EntrySessions != 0 || cart.MedianMS != 4000 || cart.P75MS != 4500 {
		t.Errorf("cart = %+v", cart)
	}
	if got.Unrecognized != 1 {
		t.Errorf("unrecognized_sessions = %d, want 1", got.Unrecognized)
	}
	if got.AppVersions == nil || len(got.AppVersions) != 0 {
		t.Errorf("app_versions = %#v, want empty list", got.AppVersions)
	}
}

func TestFlowScreensFilters(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "flow-filter")
	uid, hdr := e.addUser(t, "flowfilterviewer")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "flow-filter-app")
	for _, s := range []string{"s1", "s2"} {
		e.addReplaySession(t, pid, s, "1 hour", false, false, false)
	}
	e.addView(t, pid, "s1", 0, "Home", "android", 1000, true, true)
	e.addView(t, pid, "s2", 0, "Cart", "ios", 1000, true, true)
	for sid, v := range map[string]string{"s1": "4.57.0", "s2": "4.58.0"} {
		if _, err := e.pool.Exec(context.Background(), `UPDATE screen_views SET app_version=$3 WHERE project_id=$1 AND session_id=$2`, pid, sid, v); err != nil {
			t.Fatal(err)
		}
	}

	var got flowScreensResp
	base := fmt.Sprintf("/api/v1/projects/%d/flow/screens", pid)
	hmGet(t, e, hdr, base, &got)
	if want := []string{"4.58.0", "4.57.0"}; !reflect.DeepEqual(got.AppVersions, want) {
		t.Errorf("app_versions = %v, want %v", got.AppVersions, want)
	}
	got = flowScreensResp{}
	hmGet(t, e, hdr, base+"?platform=ios", &got)
	if len(got.Screens) != 1 || got.Screens[0].Screen != "Cart" {
		t.Fatalf("platform filter: %+v", got.Screens)
	}
	if want := []string{"4.58.0"}; !reflect.DeepEqual(got.AppVersions, want) {
		t.Errorf("platform-filtered app_versions = %v, want %v", got.AppVersions, want)
	}
	got = flowScreensResp{}
	hmGet(t, e, hdr, base+"?app_version=4.57.0", &got)
	if len(got.Screens) != 1 || got.Screens[0].Screen != "Home" {
		t.Fatalf("app_version filter: %+v", got.Screens)
	}
	if st := hmGet(t, e, hdr, base+"?from=garbage", nil); st != 400 {
		t.Errorf("bad from = %d, want 400", st)
	}

	// A user from another org must not see this project.
	otherOrg := e.addOrg(t, "flow-other")
	ouid, ohdr := e.addUser(t, "flowoutsider")
	e.addMember(t, otherOrg, ouid, "viewer")
	if st := hmGet(t, e, ohdr, base, nil); st != 403 && st != 404 {
		t.Errorf("cross-org status = %d, want 403/404", st)
	}
}
