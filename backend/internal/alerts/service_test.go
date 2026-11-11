package alerts

import (
	"context"
	"testing"
)

func countIncidents(t *testing.T, s *Service, projectID int64) (n int, status, notify string) {
	t.Helper()
	err := s.db.QueryRow(context.Background(),
		`SELECT count(*), coalesce(max(status),''), coalesce(max(notify_state),'') FROM alert_incidents WHERE project_id=$1`,
		projectID).Scan(&n, &status, &notify)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func setRule(t *testing.T, s *Service, projectID int64, kind RuleKind, enabled bool, env string) {
	t.Helper()
	if _, err := s.db.Exec(context.Background(),
		`UPDATE alert_rules SET enabled=$3, environment=$4 WHERE project_id=$1 AND kind=$2`,
		projectID, string(kind), enabled, env); err != nil {
		t.Fatal(err)
	}
}

func sig(projectID, issueID int64, kind RuleKind, env string) IssueSignal {
	return IssueSignal{ProjectID: projectID, IssueID: issueID, Kind: kind, Title: "T", Environment: env, ReleaseVersion: "1.0", SessionID: "s"}
}

func TestOnIssueCreatesPendingIncident(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	iid := seedIssue(t, pool, pid, "open")
	s := NewService(pool, nil, "http://app")
	setRule(t, s, pid, KindNewIssue, true, "production")
	if err := s.OnIssue(context.Background(), sig(pid, iid, KindNewIssue, "production")); err != nil {
		t.Fatal(err)
	}
	n, status, notify := countIncidents(t, s, pid)
	if n != 1 || status != "firing" || notify != "pending" {
		t.Fatalf("got n=%d status=%s notify=%s", n, status, notify)
	}
}

func TestOnIssueRespectsRule(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	s := NewService(pool, nil, "http://app")
	ctx := context.Background()

	i1 := seedIssue(t, pool, pid, "open")
	setRule(t, s, pid, KindNewIssue, false, "production")
	_ = s.OnIssue(ctx, sig(pid, i1, KindNewIssue, "production"))
	if n, _, _ := countIncidents(t, s, pid); n != 0 {
		t.Fatalf("disabled rule: got %d", n)
	}

	setRule(t, s, pid, KindNewIssue, true, "production")
	_ = s.OnIssue(ctx, sig(pid, i1, KindNewIssue, "staging"))
	if n, _, _ := countIncidents(t, s, pid); n != 0 {
		t.Fatalf("env mismatch: got %d", n)
	}

	setRule(t, s, pid, KindNewIssue, true, "")
	if err := s.OnIssue(ctx, sig(pid, i1, KindNewIssue, "staging")); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := countIncidents(t, s, pid); n != 1 {
		t.Fatalf("any-env rule: got %d", n)
	}
}

func TestOnIssueDedupsFiring(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	iid := seedIssue(t, pool, pid, "open")
	s := NewService(pool, nil, "http://app")
	for i := 0; i < 2; i++ {
		if err := s.OnIssue(context.Background(), sig(pid, iid, KindRegression, "production")); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if n, _, _ := countIncidents(t, s, pid); n != 1 {
		t.Fatalf("got %d incidents", n)
	}
}

func TestServiceTelegramConfigured(t *testing.T) {
	if NewService(nil, nil, "x/").WithBotUsername("b").TelegramConfigured() {
		t.Error("nil notifier should be unconfigured")
	}
	s := NewService(nil, &fakeNotifier{}, "http://app/")
	if s.TelegramConfigured() {
		t.Error("no bot username should be unconfigured")
	}
	if !s.WithBotUsername("bot").TelegramConfigured() || s.BotUsername() != "bot" || s.publicURL != "http://app" {
		t.Error("expected configured")
	}
}
