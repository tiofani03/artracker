package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/tiofani03/artracker/backend/internal/people"
)

type peopleFixture struct {
	e    *testEnv
	pid  int64
	hdr  map[string]string
	st   *people.Store
	base time.Time
}

func newPeopleFixture(t *testing.T) *peopleFixture {
	t.Helper()
	e := newTestEnv(t)
	uid, hdr := e.addUser(t, "people-viewer")
	org := e.addOrg(t, "people-api-org")
	e.addMember(t, org, uid, "viewer")
	pid := e.addProject(t, org, "people-api-app")
	return &peopleFixture{e: e, pid: pid, hdr: hdr, st: people.NewStore(e.pool),
		base: time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)}
}

// session records one session of user at base+offset, optionally with an error, device model and traits.
func (f *peopleFixture) session(t *testing.T, user, session string, offset time.Duration, hasErr bool, traits map[string]any) {
	t.Helper()
	ts := f.base.Add(offset)
	b := people.Batch{Sessions: []people.SessionDelta{{ProjectID: f.pid, SessionID: session, UserID: user,
		FirstSeen: ts, LastSeen: ts, HasError: hasErr, Context: map[string]any{"model": "Pixel 8", "platform": "android"}}}}
	if user != "" {
		b.Profiles = []people.ProfileDelta{{ProjectID: f.pid, UserID: user, FirstSeen: ts, LastSeen: ts, LastSessionID: session,
			Traits: traits, LastContext: map[string]any{"model": "Pixel 8"}}}
	}
	if err := f.st.Apply(context.Background(), b); err != nil {
		t.Fatal(err)
	}
}

func (f *peopleFixture) get(t *testing.T, path string, out any) int {
	t.Helper()
	resp := f.e.do(t, "GET", path, nil, f.hdr)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK && out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func TestListPeople(t *testing.T) {
	f := newPeopleFixture(t)
	f.session(t, "u_old", "s1", 0, false, map[string]any{"plan": "free"})
	f.session(t, "u_mid", "s2", time.Hour, true, map[string]any{"plan": "pro"})
	f.session(t, "u_mid", "s3", 2*time.Hour, false, nil)
	f.session(t, "u_new", "s4", 3*time.Hour, false, nil)
	f.session(t, "", "s5", 4*time.Hour, false, nil) // anonymous: never listed

	var res PeopleListResponse
	if code := f.get(t, fmt.Sprintf("/api/v1/projects/%d/people", f.pid), &res); code != 200 {
		t.Fatalf("status %d", code)
	}
	if res.Total != 3 || len(res.Items) != 3 || res.Items[0].UserID != "u_new" || res.Items[2].UserID != "u_old" {
		t.Fatalf("bad list: %+v", res)
	}
	mid := res.Items[1]
	if mid.UserID != "u_mid" || mid.SessionCount != 2 || mid.ErrorSessionCount != 1 || mid.Traits["plan"] != "pro" {
		t.Fatalf("bad u_mid: %+v", mid)
	}

	if f.get(t, fmt.Sprintf("/api/v1/projects/%d/people?search=U_M", f.pid), &res); res.Total != 1 || res.Items[0].UserID != "u_mid" {
		t.Fatalf("prefix search: %+v", res)
	}
	if f.get(t, fmt.Sprintf("/api/v1/projects/%d/people?search=fre", f.pid), &res); res.Total != 1 || res.Items[0].UserID != "u_old" {
		t.Fatalf("trait search: %+v", res)
	}
	if f.get(t, fmt.Sprintf("/api/v1/projects/%d/people?search=%%25", f.pid), &res); res.Total != 0 {
		t.Fatalf("%% must match literally, got %+v", res)
	}
	if f.get(t, fmt.Sprintf("/api/v1/projects/%d/people?limit=1&offset=1", f.pid), &res); res.Total != 3 || len(res.Items) != 1 || res.Items[0].UserID != "u_mid" {
		t.Fatalf("paging: %+v", res)
	}
}

func TestListPeopleProjectIsolation(t *testing.T) {
	f := newPeopleFixture(t)
	f.session(t, "u_a", "s1", 0, false, nil)
	otherOrg := f.e.addOrg(t, "people-other-org")
	other := f.e.addProject(t, otherOrg, "people-other-app")
	if err := f.st.Apply(context.Background(), people.Batch{Profiles: []people.ProfileDelta{
		{ProjectID: other, UserID: "u_b", FirstSeen: f.base, LastSeen: f.base}}}); err != nil {
		t.Fatal(err)
	}
	var res PeopleListResponse
	f.get(t, fmt.Sprintf("/api/v1/projects/%d/people", f.pid), &res)
	if res.Total != 1 || res.Items[0].UserID != "u_a" {
		t.Fatalf("leaked other project: %+v", res)
	}
	if code := f.get(t, fmt.Sprintf("/api/v1/projects/%d/people", other), nil); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("other project must be denied, got %d", code)
	}
}

func TestGetPerson(t *testing.T) {
	f := newPeopleFixture(t)
	f.session(t, "u1", "s_replay", 0, false, map[string]any{"plan": "pro"})
	f.session(t, "u1", "s_plain", time.Hour, false, nil)
	if _, err := f.e.pool.Exec(context.Background(),
		`INSERT INTO replay_sessions(id, project_id, user_id, chunk_count, has_error) VALUES ('s_replay', $1, 'u1', 2, true)`, f.pid); err != nil {
		t.Fatal(err)
	}
	var res PersonDetailResponse
	if code := f.get(t, fmt.Sprintf("/api/v1/projects/%d/people/u1", f.pid), &res); code != 200 {
		t.Fatalf("status %d", code)
	}
	if res.Person.UserID != "u1" || res.Person.SessionCount != 2 || res.Person.ErrorSessionCount != 1 || len(res.Sessions) != 2 {
		t.Fatalf("bad detail: %+v", res)
	}
	if res.Sessions[0].SessionID != "s_plain" || res.Sessions[0].HasReplay || !res.Sessions[1].HasReplay || !res.Sessions[1].HasError {
		t.Fatalf("bad sessions: %+v", res.Sessions)
	}
	if code := f.get(t, fmt.Sprintf("/api/v1/projects/%d/people/nobody", f.pid), nil); code != http.StatusNotFound {
		t.Fatalf("unknown user: %d", code)
	}
}

func TestGetPersonEncodedID(t *testing.T) {
	f := newPeopleFixture(t)
	id := "a/b c@x.id"
	f.session(t, id, "s1", 0, false, nil)
	var res PersonDetailResponse
	if code := f.get(t, fmt.Sprintf("/api/v1/projects/%d/people/%s", f.pid, url.PathEscape(id)), &res); code != 200 {
		t.Fatalf("status %d", code)
	}
	if res.Person.UserID != id {
		t.Fatalf("round-trip: %q", res.Person.UserID)
	}
}

func TestSessionContext(t *testing.T) {
	f := newPeopleFixture(t)
	for i := 0; i < 12; i++ {
		f.session(t, "u1", fmt.Sprintf("s%02d", i), time.Duration(i)*time.Minute, false, map[string]any{"plan": "pro"})
	}
	var res SessionContextResponse
	if code := f.get(t, fmt.Sprintf("/api/v1/projects/%d/sessions/s05/context", f.pid), &res); code != 200 {
		t.Fatalf("status %d", code)
	}
	if res.UserID != "u1" || res.Context["model"] != "Pixel 8" || res.Person == nil || res.Person.Traits["plan"] != "pro" {
		t.Fatalf("bad context: %+v", res)
	}
	if len(res.OtherSessions) != 10 || res.OtherSessions[0].SessionID != "s11" {
		t.Fatalf("other sessions: %+v", res.OtherSessions)
	}
	for _, s := range res.OtherSessions {
		if s.SessionID == "s05" {
			t.Fatal("current session must be excluded")
		}
	}
}

func TestSessionContextAnonymous(t *testing.T) {
	f := newPeopleFixture(t)
	f.session(t, "", "anon", 0, false, nil)
	path := fmt.Sprintf("/api/v1/projects/%d/sessions/anon/context", f.pid)
	resp := f.e.do(t, "GET", path, nil, f.hdr)
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["person"]) != "null" || string(raw["other_sessions"]) != "[]" || string(raw["user_id"]) != `""` {
		t.Fatalf("anonymous shape: %s / %s / %s", raw["person"], raw["other_sessions"], raw["user_id"])
	}
	var ctx map[string]any
	_ = json.Unmarshal(raw["context"], &ctx)
	if ctx["model"] != "Pixel 8" {
		t.Fatalf("context missing: %s", raw["context"])
	}

	var unknown SessionContextResponse
	if code := f.get(t, fmt.Sprintf("/api/v1/projects/%d/sessions/nope/context", f.pid), &unknown); code != 200 ||
		unknown.Context == nil || len(unknown.Context) != 0 || unknown.Person != nil {
		t.Fatalf("unknown session: %d %+v", code, unknown)
	}
}

func TestSessionContextFallsBackToReplayUser(t *testing.T) {
	f := newPeopleFixture(t)
	if _, err := f.e.pool.Exec(context.Background(),
		`INSERT INTO replay_sessions(id, project_id, user_id) VALUES ('legacy', $1, 'u_legacy')`, f.pid); err != nil {
		t.Fatal(err)
	}
	var res SessionContextResponse
	f.get(t, fmt.Sprintf("/api/v1/projects/%d/sessions/legacy/context", f.pid), &res)
	if res.UserID != "u_legacy" || res.Person != nil {
		t.Fatalf("fallback: %+v", res)
	}
}
