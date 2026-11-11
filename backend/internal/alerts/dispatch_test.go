package alerts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dispatchNow is the fixed clock every dispatch test runs against.
var dispatchNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func seedChannel(t *testing.T, pool *pgxpool.Pool, projectID, chatID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO alert_channels(project_id, telegram_chat_id, title) VALUES ($1, $2, 'Ops') RETURNING id`,
		projectID, chatID).Scan(&id); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	return id
}

// seedIncident inserts a firing incident that is due at dispatchNow.
func seedIncident(t *testing.T, pool *pgxpool.Pool, projectID int64, kind RuleKind, title string) int64 {
	t.Helper()
	issueID := seedIssue(t, pool, projectID, "open")
	var id int64
	if err := pool.QueryRow(context.Background(), `
INSERT INTO alert_incidents(project_id, rule_kind, issue_id, title, environment, release_version, next_attempt_at)
VALUES ($1, $2, $3, $4, 'production', '1.0', $5) RETURNING id`,
		projectID, string(kind), issueID, title, dispatchNow.Add(-time.Minute)).Scan(&id); err != nil {
		t.Fatalf("seed incident: %v", err)
	}
	return id
}

type incState struct {
	Notify, Recovery string
	Attempts         int
	NextAttempt      time.Time
}

func incidentState(t *testing.T, pool *pgxpool.Pool, id int64) incState {
	t.Helper()
	var st incState
	if err := pool.QueryRow(context.Background(),
		`SELECT notify_state, recovery_notify_state, notify_attempts, next_attempt_at FROM alert_incidents WHERE id=$1`, id).
		Scan(&st.Notify, &st.Recovery, &st.Attempts, &st.NextAttempt); err != nil {
		t.Fatalf("incident state: %v", err)
	}
	return st
}

func dispatch(t *testing.T, s *Service, now time.Time) {
	t.Helper()
	if err := s.DispatchPending(context.Background(), now); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
}

func TestDispatchSendsAndMarksSent(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	chID := seedChannel(t, pool, pid, -42)
	inc := seedIncident(t, pool, pid, KindNewIssue, "TypeError: boom")
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app")

	dispatch(t, s, dispatchNow)

	sent := fn.Sent()
	if len(sent) != 1 || sent[0].ChatID != -42 || !strings.HasPrefix(sent[0].HTML, "🆕 New issue · Checkout Web") {
		t.Fatalf("sent = %+v", sent)
	}
	if st := incidentState(t, pool, inc); st.Notify != "sent" {
		t.Fatalf("notify_state = %s", st.Notify)
	}
	var lastSent *time.Time
	var lastErr string
	if err := pool.QueryRow(context.Background(), `SELECT last_sent_at, last_error FROM alert_channels WHERE id=$1`, chID).Scan(&lastSent, &lastErr); err != nil {
		t.Fatal(err)
	}
	if lastSent == nil || !lastSent.Equal(dispatchNow) || lastErr != "" {
		t.Fatalf("channel last_sent_at=%v last_error=%q", lastSent, lastErr)
	}

	dispatch(t, s, dispatchNow.Add(time.Hour))
	if n := len(fn.Sent()); n != 1 {
		t.Fatalf("a sent incident was delivered again: %d messages", n)
	}
}

func TestDispatchDigestsBursts(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	seedChannel(t, pool, pid, -2)
	var ids []int64
	for i := 0; i < 40; i++ {
		ids = append(ids, seedIncident(t, pool, pid, KindNewIssue, "boom"))
	}
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app")

	dispatch(t, s, dispatchNow)

	sent := fn.Sent()
	if len(sent) != 2 || sent[0].ChatID != -1 || sent[1].ChatID != -2 {
		t.Fatalf("want one message per channel, got %+v", sent)
	}
	for _, m := range sent {
		if !strings.HasPrefix(m.HTML, "🚨 40 new alerts") {
			t.Fatalf("not a digest: %q", m.HTML)
		}
	}
	for _, id := range ids {
		if st := incidentState(t, pool, id); st.Notify != "sent" {
			t.Fatalf("incident %d notify_state = %s", id, st.Notify)
		}
	}
}

func TestDispatchSendsThreeIndividually(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	for i := 0; i < 3; i++ {
		seedIncident(t, pool, pid, KindNewIssue, "boom")
	}
	fn := &fakeNotifier{}
	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)
	sent := fn.Sent()
	if len(sent) != 3 {
		t.Fatalf("want 3 individual messages, got %d", len(sent))
	}
	for _, m := range sent {
		if !strings.HasPrefix(m.HTML, "🆕") {
			t.Fatalf("unexpected message %q", m.HTML)
		}
	}
}

func TestDispatchForbiddenDisablesOnlyThatChannel(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	chA := seedChannel(t, pool, pid, -1)
	chB := seedChannel(t, pool, pid, -2)
	inc := seedIncident(t, pool, pid, KindNewIssue, "boom")
	fn := &fakeNotifier{errFor: map[int64]error{-1: ErrChatForbidden}}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	var enabledA, enabledB bool
	var reasonA string
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `SELECT enabled, disabled_reason FROM alert_channels WHERE id=$1`, chA).Scan(&enabledA, &reasonA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT enabled FROM alert_channels WHERE id=$1`, chB).Scan(&enabledB); err != nil {
		t.Fatal(err)
	}
	if enabledA || reasonA == "" || !enabledB {
		t.Fatalf("A enabled=%v reason=%q, B enabled=%v", enabledA, reasonA, enabledB)
	}
	sent := fn.Sent()
	if len(sent) != 1 || sent[0].ChatID != -2 {
		t.Fatalf("sent = %+v", sent)
	}
	if st := incidentState(t, pool, inc); st.Notify != "sent" {
		t.Fatalf("notify_state = %s", st.Notify)
	}
}

func TestDispatchForbiddenOnlyChannelSkips(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	inc := seedIncident(t, pool, pid, KindNewIssue, "boom")
	fn := &fakeNotifier{errFor: map[int64]error{-1: ErrChatForbidden}}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	if st := incidentState(t, pool, inc); st.Notify != "skipped" {
		t.Fatalf("notify_state = %s, want skipped once no channel is left", st.Notify)
	}
}

func TestDispatchRetryAfterKeepsPending(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	inc := seedIncident(t, pool, pid, KindNewIssue, "boom")
	fn := &fakeNotifier{errFor: map[int64]error{-1: &RetryAfterError{Seconds: 7}}}
	s := NewService(pool, fn, "http://app")

	dispatch(t, s, dispatchNow)

	st := incidentState(t, pool, inc)
	if st.Notify != "pending" || st.Attempts != 0 || !st.NextAttempt.Equal(dispatchNow.Add(7*time.Second)) {
		t.Fatalf("state = %+v", st)
	}

	fn.mu.Lock()
	fn.errFor = nil
	fn.mu.Unlock()
	dispatch(t, s, dispatchNow.Add(time.Second))
	if n := len(fn.Sent()); n != 0 {
		t.Fatalf("sent %d messages before retry_after elapsed", n)
	}
	dispatch(t, s, dispatchNow.Add(7*time.Second))
	if n := len(fn.Sent()); n != 1 {
		t.Fatalf("sent %d messages after retry_after elapsed, want 1", n)
	}
}

func TestDispatchRetryAfterIsCapped(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	inc := seedIncident(t, pool, pid, KindNewIssue, "boom")
	fn := &fakeNotifier{errFor: map[int64]error{-1: &RetryAfterError{Seconds: 1_000_000}}}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	st := incidentState(t, pool, inc)
	if st.Notify != "pending" || !st.NextAttempt.Equal(dispatchNow.Add(time.Hour)) {
		t.Fatalf("state = %+v, want next attempt capped at now+1h", st)
	}
}

func TestDispatchRetryAfterDefersRestOfProject(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	a := seedIncident(t, pool, pid, KindNewIssue, "a")
	b := seedIncident(t, pool, pid, KindNewIssue, "b")
	_, other := seedSecondProject(t, pool)
	seedChannel(t, pool, other, -9)
	c := seedIncident(t, pool, other, KindNewIssue, "c")
	fn := &fakeNotifier{errFor: map[int64]error{-1: &RetryAfterError{Seconds: 20}}}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	for _, id := range []int64{a, b} {
		st := incidentState(t, pool, id)
		if st.Notify != "pending" || !st.NextAttempt.Equal(dispatchNow.Add(20*time.Second)) {
			t.Fatalf("incident %d state = %+v", id, st)
		}
	}
	if st := incidentState(t, pool, c); st.Notify != "sent" {
		t.Fatalf("other project's incident notify_state = %s", st.Notify)
	}
}

func seedSecondProject(t *testing.T, pool *pgxpool.Pool) (orgID, projectID int64) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Beta','beta') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'Mobile','mobile','android') RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return orgID, projectID
}

func TestDispatchMigratedChatUpdatesID(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	ch := seedChannel(t, pool, pid, -1)
	inc := seedIncident(t, pool, pid, KindNewIssue, "boom")
	fn := &fakeNotifier{errFor: map[int64]error{-1: &MigratedError{NewChatID: -100999}}}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	var chatID int64
	if err := pool.QueryRow(context.Background(), `SELECT telegram_chat_id FROM alert_channels WHERE id=$1`, ch).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	if chatID != -100999 {
		t.Fatalf("chat id = %d", chatID)
	}
	sent := fn.Sent()
	if len(sent) != 1 || sent[0].ChatID != -100999 {
		t.Fatalf("sent = %+v", sent)
	}
	if st := incidentState(t, pool, inc); st.Notify != "sent" {
		t.Fatalf("notify_state = %s", st.Notify)
	}
}

func TestDispatchMigratedToExistingChannel(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	stale := seedChannel(t, pool, pid, -1)
	seedChannel(t, pool, pid, -100999)
	inc := seedIncident(t, pool, pid, KindNewIssue, "boom")
	fn := &fakeNotifier{errFor: map[int64]error{-1: &MigratedError{NewChatID: -100999}}}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM alert_channels WHERE id=$1 AND enabled`, stale).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the stale channel is still active")
	}
	sent := fn.Sent()
	if len(sent) != 1 || sent[0].ChatID != -100999 {
		t.Fatalf("want exactly one message to the existing chat, got %+v", sent)
	}
	if st := incidentState(t, pool, inc); st.Notify != "sent" {
		t.Fatalf("notify_state = %s", st.Notify)
	}
}

func TestDispatchNoChannelsSkips(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	inc := seedIncident(t, pool, pid, KindNewIssue, "boom")
	fn := &fakeNotifier{}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	if st := incidentState(t, pool, inc); st.Notify != "skipped" {
		t.Fatalf("notify_state = %s", st.Notify)
	}
	if n := len(fn.Sent()); n != 0 {
		t.Fatalf("sent %d messages", n)
	}
}

func TestDispatchWithoutNotifierSkips(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	inc := seedIncident(t, pool, pid, KindIssueSpike, "boom")
	if _, err := pool.Exec(context.Background(),
		`UPDATE alert_incidents SET status='resolved', recovery_notify_state='pending' WHERE id=$1`, inc); err != nil {
		t.Fatal(err)
	}

	dispatch(t, NewService(pool, nil, "http://app"), dispatchNow)

	if st := incidentState(t, pool, inc); st.Notify != "skipped" || st.Recovery != "skipped" {
		t.Fatalf("state = %+v", st)
	}
}

func TestDispatchGivesUpAfterFiveAttempts(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	chID := seedChannel(t, pool, pid, -1)
	inc := seedIncident(t, pool, pid, KindNewIssue, "boom")
	fn := &fakeNotifier{errFor: map[int64]error{-1: errors.New("network down")}}
	s := NewService(pool, fn, "http://app")

	dispatch(t, s, dispatchNow)
	st := incidentState(t, pool, inc)
	if st.Notify != "pending" || st.Attempts != 1 || !st.NextAttempt.Equal(dispatchNow.Add(30*time.Second)) {
		t.Fatalf("after first failure: %+v", st)
	}
	var lastErr string
	if err := pool.QueryRow(context.Background(), `SELECT last_error FROM alert_channels WHERE id=$1`, chID).Scan(&lastErr); err != nil {
		t.Fatal(err)
	}
	if lastErr == "" {
		t.Fatal("last_error was not recorded")
	}

	for i := 1; i < 5; i++ {
		dispatch(t, s, dispatchNow.Add(time.Duration(i)*time.Hour))
	}
	if st := incidentState(t, pool, inc); st.Notify != "failed" || st.Attempts != 5 {
		t.Fatalf("after five failures: %+v", st)
	}
}

func TestDispatchSendsRecovery(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	inc := seedIncident(t, pool, pid, KindIssueSpike, "boom")
	if _, err := pool.Exec(context.Background(),
		`UPDATE alert_incidents SET status='resolved', notify_state='sent', recovery_notify_state='pending' WHERE id=$1`, inc); err != nil {
		t.Fatal(err)
	}
	fn := &fakeNotifier{}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	sent := fn.Sent()
	if len(sent) != 1 || !strings.HasPrefix(sent[0].HTML, "✅") {
		t.Fatalf("sent = %+v", sent)
	}
	if st := incidentState(t, pool, inc); st.Recovery != "sent" || st.Notify != "sent" {
		t.Fatalf("state = %+v", st)
	}
}

func TestDispatchSendsFireBeforeRecovery(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	inc := seedIncident(t, pool, pid, KindIssueSpike, "boom")
	if _, err := pool.Exec(context.Background(),
		`UPDATE alert_incidents SET status='resolved', recovery_notify_state='pending',
		 detail='{"count":25,"baseline":1,"window_minutes":60}' WHERE id=$1`, inc); err != nil {
		t.Fatal(err)
	}
	fn := &fakeNotifier{}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	sent := fn.Sent()
	if len(sent) != 2 || !strings.HasPrefix(sent[0].HTML, "📈 Spike") || !strings.HasPrefix(sent[1].HTML, "✅") {
		t.Fatalf("want fire then recovery, got %+v", sent)
	}
	if !strings.Contains(sent[0].HTML, "25 events in 60 min") {
		t.Fatalf("spike detail missing: %q", sent[0].HTML)
	}
	if st := incidentState(t, pool, inc); st.Notify != "sent" || st.Recovery != "sent" {
		t.Fatalf("state = %+v", st)
	}
}

func TestDispatchAddsReplayLinkWhenSessionExists(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	withReplay := seedIncident(t, pool, pid, KindNewIssue, "a")
	noReplay := seedIncident(t, pool, pid, KindNewIssue, "b")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE alert_incidents SET session_id = CASE WHEN id=$1 THEN 'sess_1' ELSE 'sess_missing' END WHERE id IN ($1,$2)`, withReplay, noReplay); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO replay_sessions(id, project_id) VALUES ('sess_1', $1)`, pid); err != nil {
		t.Fatal(err)
	}
	fn := &fakeNotifier{}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	sent := fn.Sent()
	if len(sent) != 2 {
		t.Fatalf("sent = %+v", sent)
	}
	if !strings.Contains(sent[0].HTML, "Watch replay") || strings.Contains(sent[1].HTML, "Watch replay") {
		t.Fatalf("replay links wrong: %q / %q", sent[0].HTML, sent[1].HTML)
	}
}

func TestDispatchDigestFailureRetriesAll(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	var ids []int64
	for i := 0; i < 4; i++ {
		ids = append(ids, seedIncident(t, pool, pid, KindNewIssue, "boom"))
	}
	fn := &fakeNotifier{errFor: map[int64]error{-1: errors.New("network down")}}

	dispatch(t, NewService(pool, fn, "http://app"), dispatchNow)

	for _, id := range ids {
		st := incidentState(t, pool, id)
		if st.Notify != "pending" || st.Attempts != 1 || !st.NextAttempt.Equal(dispatchNow.Add(30*time.Second)) {
			t.Fatalf("incident %d state = %+v", id, st)
		}
	}
}
