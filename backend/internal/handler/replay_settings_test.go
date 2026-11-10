package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type replaySettingsFixture struct {
	e        *testEnv
	pid      int64
	adminHdr map[string]string
	viewHdr  map[string]string
}

func newReplaySettingsFixture(t *testing.T) replaySettingsFixture {
	t.Helper()
	e := newTestEnv(t)
	org := e.addOrg(t, "acme-replay-settings")
	adminID, adminHdr := e.addUser(t, "rsadmin")
	e.addMember(t, org, adminID, "admin")
	viewerID, viewHdr := e.addUser(t, "rsviewer")
	e.addMember(t, org, viewerID, "viewer")
	pid := e.addProject(t, org, "replay-settings-app")
	return replaySettingsFixture{e: e, pid: pid, adminHdr: adminHdr, viewHdr: viewHdr}
}

func (f replaySettingsFixture) getProject(t *testing.T) Project {
	t.Helper()
	resp := f.e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d", f.pid), nil, f.adminHdr)
	if resp.StatusCode != 200 {
		t.Fatalf("get project = %d", resp.StatusCode)
	}
	var p Project
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f replaySettingsFixture) patch(t *testing.T, hdr map[string]string, body string) (int, Project) {
	t.Helper()
	resp := f.e.do(t, "PATCH", fmt.Sprintf("/api/v1/projects/%d/replay-settings", f.pid), strings.NewReader(body), hdr)
	var p Project
	_ = json.NewDecoder(resp.Body).Decode(&p)
	return resp.StatusCode, p
}

func TestReplaySettingsDefaultOn(t *testing.T) {
	f := newReplaySettingsFixture(t)
	if !f.getProject(t).ReplayScreenshotEnabled {
		t.Fatal("new project should have replay_screenshot_enabled = true")
	}
}

func TestReplaySettingsPatchAdmin(t *testing.T) {
	f := newReplaySettingsFixture(t)
	status, p := f.patch(t, f.adminHdr, `{"screenshot_enabled":false}`)
	if status != 200 {
		t.Fatalf("patch status = %d", status)
	}
	if p.ReplayScreenshotEnabled {
		t.Fatal("patch response should report replay_screenshot_enabled = false")
	}
	if f.getProject(t).ReplayScreenshotEnabled {
		t.Fatal("GET after PATCH should report false")
	}
}

func TestReplaySettingsViewerForbidden(t *testing.T) {
	f := newReplaySettingsFixture(t)
	status, _ := f.patch(t, f.viewHdr, `{"screenshot_enabled":false}`)
	if status != 403 {
		t.Fatalf("viewer patch status = %d, want 403", status)
	}
	if !f.getProject(t).ReplayScreenshotEnabled {
		t.Fatal("value must be unchanged after a forbidden patch")
	}
}

func TestReplaySettingsValidation(t *testing.T) {
	f := newReplaySettingsFixture(t)
	if status, _ := f.patch(t, f.adminHdr, `{}`); status != 400 {
		t.Fatalf("empty body status = %d, want 400", status)
	}
}
