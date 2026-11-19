package handler

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

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
