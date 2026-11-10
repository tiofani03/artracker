package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

type navCounts struct {
	FrustratedReplays24h int `json:"frustrated_replays_24h"`
	OpenIssues           int `json:"open_issues"`
}

func (e *testEnv) addReplaySession(t *testing.T, pid int64, id, ageSQL string, errFlag, rage, dead bool) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO replay_sessions(id, project_id, start_time, has_error, has_rage_click, has_dead_click)
		 VALUES ($1,$2, now() - $3::interval, $4,$5,$6)`, id, pid, ageSQL, errFlag, rage, dead)
	if err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) addIssue(t *testing.T, pid int64, fp, status string) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO issues(project_id, fingerprint, title, status) VALUES ($1,$2,$2,$3)`, pid, fp, status)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNavCountsReflectRealData(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme-nav")
	uid, hdr := e.addUser(t, "navviewer")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "nav-app")
	other := e.addProject(t, org, "nav-other")

	e.addReplaySession(t, pid, "s-err", "1 hour", true, false, false)
	e.addReplaySession(t, pid, "s-rage", "2 hours", false, true, false)
	e.addReplaySession(t, pid, "s-dead", "3 hours", false, false, true)
	e.addReplaySession(t, pid, "s-clean", "1 hour", false, false, false)
	e.addReplaySession(t, pid, "s-old", "25 hours", true, true, true)
	e.addReplaySession(t, other, "s-other", "1 hour", true, false, false)

	e.addIssue(t, pid, "a", "open")
	e.addIssue(t, pid, "b", "open")
	e.addIssue(t, pid, "c", "resolved")
	e.addIssue(t, pid, "d", "ignored")
	e.addIssue(t, other, "e", "open")

	resp := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/nav-counts", pid), nil, hdr)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got navCounts
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.FrustratedReplays24h != 3 {
		t.Errorf("frustrated_replays_24h = %d, want 3 (error, rage, dead within 24h only)", got.FrustratedReplays24h)
	}
	if got.OpenIssues != 2 {
		t.Errorf("open_issues = %d, want 2 (open only, this project only)", got.OpenIssues)
	}
}

func TestNavCountsForbiddenForNonMember(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme-nav2")
	pid := e.addProject(t, org, "nav-app2")
	_, hdr := e.addUser(t, "navoutsider")
	resp := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/nav-counts", pid), nil, hdr)
	if resp.StatusCode != 403 && resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 403/404", resp.StatusCode)
	}
}
