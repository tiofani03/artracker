package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tiofani03/artracker/backend/internal/people"
)

func newPeopleEnv(t *testing.T) (*testEnv, int64, *people.Store) {
	t.Helper()
	e := newTestEnv(t)
	org := e.addOrg(t, "people-org")
	pid := e.addProject(t, org, "people-app")
	return e, pid, people.NewStore(e.pool)
}

func (e *testEnv) profileTraits(t *testing.T, pid int64, user string) map[string]any {
	t.Helper()
	var raw []byte
	if err := e.pool.QueryRow(context.Background(),
		`SELECT traits FROM user_profiles WHERE project_id=$1 AND user_id=$2`, pid, user).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestApplyUpsertsProfileAndSession(t *testing.T) {
	e, pid, st := newPeopleEnv(t)
	ts := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	err := st.Apply(context.Background(), people.Batch{
		Sessions: []people.SessionDelta{{ProjectID: pid, SessionID: "s1", UserID: "u1", FirstSeen: ts, LastSeen: ts, HasError: true,
			Context: map[string]any{"model": "Pixel 8"}}},
		Profiles: []people.ProfileDelta{{ProjectID: pid, UserID: "u1", FirstSeen: ts, LastSeen: ts, LastSessionID: "s1",
			Traits: map[string]any{"plan": "pro"}, LastContext: map[string]any{"model": "Pixel 8"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tr := e.profileTraits(t, pid, "u1"); len(tr) != 1 || tr["plan"] != "pro" {
		t.Fatalf("traits: %v", tr)
	}
	var model string
	var hasErr bool
	if err := e.pool.QueryRow(context.Background(),
		`SELECT p.last_context->>'model', s.has_error FROM user_profiles p JOIN session_contexts s USING (project_id)
         WHERE p.project_id=$1 AND s.session_id='s1'`, pid).Scan(&model, &hasErr); err != nil {
		t.Fatal(err)
	}
	if model != "Pixel 8" || !hasErr {
		t.Fatalf("model=%q hasErr=%v", model, hasErr)
	}
}

func TestApplyMergesTraits(t *testing.T) {
	e, pid, st := newPeopleEnv(t)
	ts := time.Now().UTC()
	apply := func(tr map[string]any) {
		t.Helper()
		if err := st.Apply(context.Background(), people.Batch{Profiles: []people.ProfileDelta{
			{ProjectID: pid, UserID: "u1", FirstSeen: ts, LastSeen: ts, Traits: tr}}}); err != nil {
			t.Fatal(err)
		}
	}
	apply(map[string]any{"plan": "pro", "country": "ID"})
	apply(map[string]any{"plan": "team", "country": nil})
	apply(nil) // a batch without $identify leaves traits alone
	if tr := e.profileTraits(t, pid, "u1"); len(tr) != 1 || tr["plan"] != "team" {
		t.Fatalf("want {plan:team}, got %v", tr)
	}
}

func TestApplyOutOfOrderKeepsBounds(t *testing.T) {
	e, pid, st := newPeopleEnv(t)
	t0 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	t1, t3 := t0.Add(time.Hour), t0.Add(3*time.Hour)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.Apply(ctx, people.Batch{Profiles: []people.ProfileDelta{{ProjectID: pid, UserID: "u1", FirstSeen: t3, LastSeen: t3,
		LastSessionID: "new", LastContext: map[string]any{"model": "new"}}}}))
	must(st.Apply(ctx, people.Batch{Profiles: []people.ProfileDelta{{ProjectID: pid, UserID: "u1", FirstSeen: t0, LastSeen: t1,
		LastSessionID: "old", LastContext: map[string]any{"model": "old"}}}}))
	var first, last time.Time
	var sid, model string
	must(e.pool.QueryRow(ctx, `SELECT first_seen, last_seen, last_session_id, last_context->>'model' FROM user_profiles
        WHERE project_id=$1 AND user_id='u1'`, pid).Scan(&first, &last, &sid, &model))
	if !first.Equal(t0) || !last.Equal(t3) || sid != "new" || model != "new" {
		t.Fatalf("first=%v last=%v sid=%s model=%s", first, last, sid, model)
	}
}

func TestApplyFillsEmptyUserOnly(t *testing.T) {
	e, pid, st := newPeopleEnv(t)
	ts := time.Now().UTC()
	ctx := context.Background()
	for i, d := range []people.SessionDelta{
		{UserID: "", HasError: true, Context: map[string]any{"model": "Pixel 8"}},
		{UserID: "u1"},
		{UserID: "u2"},
	} {
		d.ProjectID, d.SessionID, d.FirstSeen, d.LastSeen = pid, "s1", ts, ts.Add(time.Duration(i)*time.Second)
		if err := st.Apply(ctx, people.Batch{Sessions: []people.SessionDelta{d}}); err != nil {
			t.Fatal(err)
		}
	}
	var user, model string
	var hasErr bool
	if err := e.pool.QueryRow(ctx, `SELECT user_id, has_error, context->>'model' FROM session_contexts
        WHERE project_id=$1 AND session_id='s1'`, pid).Scan(&user, &hasErr, &model); err != nil {
		t.Fatal(err)
	}
	if user != "u1" || !hasErr || model != "Pixel 8" {
		t.Fatalf("user=%s hasErr=%v model=%s", user, hasErr, model)
	}
}

func TestTouchFromReplay(t *testing.T) {
	e, pid, st := newPeopleEnv(t)
	ts := time.Now().UTC()
	for i := 0; i < 2; i++ {
		if err := st.TouchFromReplay(context.Background(), pid, "u9", "s9", ts); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM user_profiles WHERE project_id=%d AND user_id='u9'`, pid)); n != 1 {
		t.Fatalf("profiles: %d", n)
	}
	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM session_contexts WHERE project_id=%d AND session_id='s9' AND user_id='u9'`, pid)); n != 1 {
		t.Fatalf("sessions: %d", n)
	}
}

func TestPeopleMigrationBackfill(t *testing.T) {
	e, pid, _ := newPeopleEnv(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := e.pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO replay_sessions(id, project_id, user_id, has_error) VALUES ('r1', $1, 'u1', true), ('r2', $1, '', false)`, pid)
	exec(`INSERT INTO telemetry_events(id, project_id, kind, name, session_id, user_id, event_timestamp, attributes)
          VALUES (gen_random_uuid(), $1, 'session', 'session.start', 'r1', 'u1', now(), '{"model":"iPhone15,2"}'),
                 (gen_random_uuid(), $1, 'session', 'session_start', 't1', 'u2', now(), '{"model":"Pixel 8"}'),
                 (gen_random_uuid(), 999999, 'session', 'session_start', 'x1', 'ux', now(), '{}')`, pid)
	t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM telemetry_events WHERE project_id IN ($1, 999999)`, pid)
	})

	raw, err := os.ReadFile("../db/migrations/0016_people.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, backfill, ok := strings.Cut(string(raw), "-- Backfill")
	if !ok {
		t.Fatal("backfill section not found")
	}
	exec("-- Backfill" + backfill)

	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM session_contexts WHERE project_id=%d`, pid)); n != 3 {
		t.Fatalf("want 3 session contexts (r1, r2, t1), got %d", n)
	}
	var model string
	var hasErr bool
	if err := e.pool.QueryRow(ctx, `SELECT context->>'model', has_error FROM session_contexts WHERE project_id=$1 AND session_id='r1'`, pid).
		Scan(&model, &hasErr); err != nil {
		t.Fatal(err)
	}
	if model != "iPhone15,2" || !hasErr {
		t.Fatalf("r1 model=%q hasErr=%v", model, hasErr)
	}
	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM user_profiles WHERE project_id=%d`, pid)); n != 2 {
		t.Fatalf("want 2 profiles (u1, u2), got %d", n)
	}
}
