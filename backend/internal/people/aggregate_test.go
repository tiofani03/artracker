package people

import (
	"fmt"
	"testing"
	"time"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

func row(session, user, kind, name, level string, ts time.Time, attrs string) ingest.TelemetryRow {
	if attrs == "" {
		attrs = "{}"
	}
	return ingest.TelemetryRow{
		ProjectID: 1, Kind: kind, Name: name, Level: level,
		SessionID: session, UserID: user, EventTimestamp: ts, Attributes: []byte(attrs),
	}
}

func findSession(b Batch, id string) *SessionDelta {
	for i := range b.Sessions {
		if b.Sessions[i].SessionID == id {
			return &b.Sessions[i]
		}
	}
	return nil
}

func TestAggregate(t *testing.T) {
	t1 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	rows := []ingest.TelemetryRow{
		row("s1", "u1", "log", "x", "error", t2, ""),
		row("s1", "u1", "session", "session_start", "", t1, `{"model":"Pixel 8"}`),
		row("s1", "u1", "event", "$identify", "", t1, `{"plan":"pro"}`),
		row("s1", "u1", "event", "tap", "info", t1, ""),
		row("s2", "", "event", "tap", "info", t1, ""),
	}
	b := Aggregate(rows)
	if len(b.Sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(b.Sessions))
	}
	s1 := findSession(b, "s1")
	if s1 == nil || !s1.HasError || !s1.FirstSeen.Equal(t1) || !s1.LastSeen.Equal(t2) || s1.UserID != "u1" {
		t.Fatalf("bad s1: %+v", s1)
	}
	if s1.Context["model"] != "Pixel 8" {
		t.Fatalf("s1 context: %v", s1.Context)
	}
	s2 := findSession(b, "s2")
	if s2 == nil || s2.UserID != "" || s2.HasError || s2.Context != nil {
		t.Fatalf("bad s2: %+v", s2)
	}
	if len(b.Profiles) != 1 {
		t.Fatalf("want 1 profile, got %d", len(b.Profiles))
	}
	p := b.Profiles[0]
	if p.UserID != "u1" || p.Traits["plan"] != "pro" || p.LastContext["model"] != "Pixel 8" || p.LastSessionID != "s1" {
		t.Fatalf("bad profile: %+v", p)
	}
	if !p.FirstSeen.Equal(t1) || !p.LastSeen.Equal(t2) {
		t.Fatalf("profile bounds: %v %v", p.FirstSeen, p.LastSeen)
	}
}

func TestAggregateSessionDotStart(t *testing.T) {
	ts := time.Now().UTC()
	b := Aggregate([]ingest.TelemetryRow{row("s1", "", "session", "session.start", "", ts, `{"model":"iPhone15,2"}`)})
	if s := findSession(b, "s1"); s == nil || s.Context["model"] != "iPhone15,2" {
		t.Fatalf("session.start must set context: %+v", s)
	}
	if len(b.Profiles) != 0 {
		t.Fatal("anonymous session must not create a profile")
	}
}

func TestAggregateDeterministicOrder(t *testing.T) {
	t1 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Second)
	later := row("s1", "u1", "event", "$identify", "", t2, `{"plan":"team"}`)
	earlier := row("s1", "u1", "event", "$identify", "", t1, `{"plan":"pro","country":"ID"}`)
	for _, rows := range [][]ingest.TelemetryRow{{later, earlier}, {earlier, later}} {
		b := Aggregate(rows)
		tr := b.Profiles[0].Traits
		if tr["plan"] != "team" || tr["country"] != "ID" {
			t.Fatalf("want later-wins merge, got %v", tr)
		}
	}
}

func TestAggregateSkipsEmptySession(t *testing.T) {
	b := Aggregate([]ingest.TelemetryRow{row("", "u1", "event", "tap", "", time.Now(), "")})
	if len(b.Sessions) != 0 {
		t.Fatal("empty session id must not produce a session delta")
	}
	if len(b.Profiles) != 1 || b.Profiles[0].LastSessionID != "" {
		t.Fatalf("profile still touched: %+v", b.Profiles)
	}
}

func TestAggregateCapsMergedTraitsAndRecordsTime(t *testing.T) {
	t1 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	first, second := map[string]any{}, map[string]any{}
	for i := 0; i < 40; i++ {
		first[fmt.Sprintf("a%02d", i)] = i
		second[fmt.Sprintf("b%02d", i)] = i
	}
	second["a00"] = "updated"
	b := Aggregate([]ingest.TelemetryRow{
		row("s1", "u1", "event", "$identify", "", t1, string(mustJSON(t, first))),
		row("s1", "u1", "event", "$identify", "", t1.Add(time.Second), string(mustJSON(t, second))),
	})
	p := b.Profiles[0]
	if len(p.Traits) != MaxTraitKeys {
		t.Fatalf("merged traits must stop at %d keys, got %d", MaxTraitKeys, len(p.Traits))
	}
	if p.Traits["a00"] != "updated" {
		t.Fatal("existing keys keep updating past the cap")
	}
	if !p.TraitsAt.Equal(t1.Add(time.Second)) {
		t.Fatalf("TraitsAt = %v", p.TraitsAt)
	}
}
