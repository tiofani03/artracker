package alerts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// evalNow is the fixed clock the evaluator tests run against. Every project
// uses the seeded defaults unless a test changes them: environment
// 'production', multiplier 5, min_count 20, window 60 min, cooldown 60 min.
var evalNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

// insertEvent writes one issue_events row at the given time.
func insertEvent(t *testing.T, pool *pgxpool.Pool, issueID int64, at time.Time, env, release, session string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO issue_events(issue_id, created_at, environment, release_version, session_id)
VALUES ($1, $2, $3, $4, $5)`, issueID, at, env, release, session); err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

// addEvents writes n events for the issue, all at the given time.
func addEvents(t *testing.T, pool *pgxpool.Pool, issueID int64, n int, at time.Time, env string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO issue_events(issue_id, created_at, environment)
SELECT $1, $2, $3 FROM generate_series(1, $4)`, issueID, at, env, n); err != nil {
		t.Fatalf("add events: %v", err)
	}
}

// addBaseline writes perHour events at the middle of each of the 24 hours of
// the baseline range (now − 60m − 24h, now − 60m] for a 60-minute window, so
// the baseline is 24·perHour × 60/1440 = perHour per window.
func addBaseline(t *testing.T, pool *pgxpool.Pool, issueID int64, perHour int, now time.Time, env string) {
	t.Helper()
	for h := 0; h < 24; h++ {
		addEvents(t, pool, issueID, perHour, now.Add(-90*time.Minute-time.Duration(h)*time.Hour), env)
	}
}

func evaluate(t *testing.T, s *Service, now time.Time) {
	t.Helper()
	if err := s.Evaluate(context.Background(), now); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
}

type spikeRow struct {
	ID                           int64
	Status, Recovery, Notify     string
	Title, Env, Release, Session string
	Detail                       SpikeDetail
	FiredAt                      time.Time
	ResolvedAt                   *time.Time
	NextAttemptAt                time.Time
}

// spikeIncidents returns the issue's spike incidents, oldest first.
func spikeIncidents(t *testing.T, pool *pgxpool.Pool, issueID int64) []spikeRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
SELECT id, status, recovery_notify_state, notify_state, title, environment, release_version, session_id,
       detail, fired_at, resolved_at, next_attempt_at
FROM alert_incidents WHERE issue_id = $1 AND rule_kind = 'issue_spike' ORDER BY id`, issueID)
	if err != nil {
		t.Fatalf("query incidents: %v", err)
	}
	defer rows.Close()
	var out []spikeRow
	for rows.Next() {
		var r spikeRow
		var detail []byte
		if err := rows.Scan(&r.ID, &r.Status, &r.Recovery, &r.Notify, &r.Title, &r.Env, &r.Release, &r.Session,
			&detail, &r.FiredAt, &r.ResolvedAt, &r.NextAttemptAt); err != nil {
			t.Fatalf("scan incident: %v", err)
		}
		if err := json.Unmarshal(detail, &r.Detail); err != nil {
			t.Fatalf("detail %s: %v", detail, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func incidentStatus(t *testing.T, pool *pgxpool.Pool, id int64) (status, recovery string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT status, recovery_notify_state FROM alert_incidents WHERE id = $1`, id).Scan(&status, &recovery); err != nil {
		t.Fatalf("incident status: %v", err)
	}
	return status, recovery
}

// seedFiringSpike inserts a firing spike incident for the issue fired at firedAt.
func seedFiringSpike(t *testing.T, pool *pgxpool.Pool, projectID, issueID int64, firedAt time.Time) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
INSERT INTO alert_incidents(project_id, rule_kind, issue_id, title, environment, fired_at, next_attempt_at, notify_state)
VALUES ($1, 'issue_spike', $2, 'TypeError: boom', 'production', $3, $3, 'sent') RETURNING id`,
		projectID, issueID, firedAt).Scan(&id); err != nil {
		t.Fatalf("seed spike: %v", err)
	}
	return id
}

func setSpikeRule(t *testing.T, pool *pgxpool.Pool, projectID int64, set string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE alert_rules SET `+set+` WHERE project_id = $1 AND kind = 'issue_spike'`, projectID); err != nil {
		t.Fatalf("update rule: %v", err)
	}
}

func TestSpikeFires(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	iss := seedIssue(t, pool, pid, "open")
	// baseline: 24 events, one per hour of the previous day → 24 × 60/1440 = 1 per window.
	addBaseline(t, pool, iss, 1, evalNow, "production")
	// current: 24 + 1 = 25 events in (now−60m, now]; 25 ≥ 20 and 25 ≥ 5 × max(1, 1) = 5.
	addEvents(t, pool, iss, 24, evalNow.Add(-20*time.Minute), "production")
	insertEvent(t, pool, iss, evalNow.Add(-time.Minute), "production", "2.0.1", "sess-9")
	// noise outside both ranges must not count: just after now and before the baseline.
	addEvents(t, pool, iss, 50, evalNow.Add(time.Second), "production")
	addEvents(t, pool, iss, 50, evalNow.Add(-26*time.Hour), "production")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	incs := spikeIncidents(t, pool, iss)
	if len(incs) != 1 {
		t.Fatalf("incidents = %+v", incs)
	}
	got := incs[0]
	if got.Status != "firing" || got.Notify != "pending" || got.Recovery != "none" {
		t.Fatalf("states = %+v", got)
	}
	if got.Detail != (SpikeDetail{Count: 25, Baseline: 1, WindowMinutes: 60}) {
		t.Fatalf("detail = %+v", got.Detail)
	}
	if got.Title != "TypeError: boom" || got.Env != "production" || got.Release != "2.0.1" || got.Session != "sess-9" {
		t.Fatalf("latest-event fields = %+v", got)
	}
	if !got.FiredAt.Equal(evalNow) || !got.NextAttemptAt.Equal(evalNow) {
		t.Fatalf("fired_at = %v next_attempt_at = %v", got.FiredAt, got.NextAttemptAt)
	}

	// a second tick does not duplicate the firing incident.
	evaluate(t, NewService(pool, nil, ""), evalNow.Add(time.Minute))
	if incs := spikeIncidents(t, pool, iss); len(incs) != 1 {
		t.Fatalf("after second tick incidents = %+v", incs)
	}
}

func TestSpikeBelowMinCount(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	iss := seedIssue(t, pool, pid, "open")
	// baseline 0; current 19 < min_count 20.
	addEvents(t, pool, iss, 19, evalNow.Add(-10*time.Minute), "production")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	if incs := spikeIncidents(t, pool, iss); len(incs) != 0 {
		t.Fatalf("incidents = %+v", incs)
	}
}

func TestSpikeBelowMultiplier(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	iss := seedIssue(t, pool, pid, "open")
	// baseline: 240 events over the day → 240 × 60/1440 = 10 per window; threshold 5 × 10 = 50.
	addBaseline(t, pool, iss, 10, evalNow, "production")
	// current 40 ≥ 20 but 40 < 50.
	addEvents(t, pool, iss, 40, evalNow.Add(-10*time.Minute), "production")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	if incs := spikeIncidents(t, pool, iss); len(incs) != 0 {
		t.Fatalf("incidents = %+v", incs)
	}
}

func TestSpikeBoundaries(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	// min_count edge (baseline 0, threshold max(20, 5 × 1) = 20): 20 fires, 19 does not.
	atMin := seedIssue(t, pool, pid, "open")
	addEvents(t, pool, atMin, 20, evalNow.Add(-10*time.Minute), "production")
	belowMin := seedIssue(t, pool, pid, "open")
	addEvents(t, pool, belowMin, 19, evalNow.Add(-10*time.Minute), "production")
	// multiplier edge (baseline 240 × 60/1440 = 10, threshold 5 × 10 = 50): 50 fires, 49 does not.
	atMult := seedIssue(t, pool, pid, "open")
	addBaseline(t, pool, atMult, 10, evalNow, "production")
	addEvents(t, pool, atMult, 50, evalNow.Add(-10*time.Minute), "production")
	belowMult := seedIssue(t, pool, pid, "open")
	addBaseline(t, pool, belowMult, 10, evalNow, "production")
	addEvents(t, pool, belowMult, 49, evalNow.Add(-10*time.Minute), "production")
	// window edges: an event exactly at now counts as current, one exactly at
	// now − W does not (it is the last instant of the baseline range).
	edges := seedIssue(t, pool, pid, "open")
	addEvents(t, pool, edges, 19, evalNow.Add(-10*time.Minute), "production")
	addEvents(t, pool, edges, 1, evalNow, "production")
	addEvents(t, pool, edges, 5, evalNow.Add(-60*time.Minute), "production")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	for name, tc := range map[string]struct {
		issue int64
		fires bool
		count int
	}{
		"current == min_count":         {atMin, true, 20},
		"current == min_count - 1":     {belowMin, false, 0},
		"current == multiplier×base":   {atMult, true, 50},
		"current == multiplier×base-1": {belowMult, false, 0},
		"event at now counts":          {edges, true, 20},
	} {
		incs := spikeIncidents(t, pool, tc.issue)
		if !tc.fires {
			if len(incs) != 0 {
				t.Errorf("%s: incidents = %+v", name, incs)
			}
			continue
		}
		if len(incs) != 1 || incs[0].Detail.Count != tc.count {
			t.Errorf("%s: incidents = %+v", name, incs)
		}
	}
	if incs := spikeIncidents(t, pool, atMult); len(incs) == 1 && incs[0].Detail.Baseline != 10 {
		t.Errorf("baseline = %v, want 10", incs[0].Detail.Baseline)
	}
	// the 5 events at now − W fall in the baseline range: 5 × 60/1440.
	if incs := spikeIncidents(t, pool, edges); len(incs) == 1 && incs[0].Detail.Baseline != 5.0*60/1440 {
		t.Errorf("edge baseline = %v", incs[0].Detail.Baseline)
	}
}

func TestSpikeEnvironmentFilter(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool) // rule environment = 'production'
	// 25 staging events do not count for a production rule → none.
	staging := seedIssue(t, pool, pid, "open")
	addEvents(t, pool, staging, 25, evalNow.Add(-10*time.Minute), "staging")
	// 10 production + 10 legacy '' = 20 counted (the 30 staging are ignored) → fires with count 20.
	legacy := seedIssue(t, pool, pid, "open")
	addEvents(t, pool, legacy, 10, evalNow.Add(-20*time.Minute), "production")
	addEvents(t, pool, legacy, 10, evalNow.Add(-10*time.Minute), "")
	addEvents(t, pool, legacy, 30, evalNow.Add(-5*time.Minute), "staging")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	if incs := spikeIncidents(t, pool, staging); len(incs) != 0 {
		t.Fatalf("staging incidents = %+v", incs)
	}
	incs := spikeIncidents(t, pool, legacy)
	if len(incs) != 1 || incs[0].Detail.Count != 20 {
		t.Fatalf("legacy incidents = %+v", incs)
	}
	// the latest counted event is the legacy '' one, not the later staging one.
	if incs[0].Env != "" {
		t.Fatalf("environment = %q", incs[0].Env)
	}
}

func TestSpikeRuleWithoutEnvironmentCountsAll(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	setSpikeRule(t, pool, pid, `environment = ''`)
	iss := seedIssue(t, pool, pid, "open")
	// 10 staging + 10 production = 20 → fires.
	addEvents(t, pool, iss, 10, evalNow.Add(-20*time.Minute), "production")
	addEvents(t, pool, iss, 10, evalNow.Add(-10*time.Minute), "staging")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	incs := spikeIncidents(t, pool, iss)
	if len(incs) != 1 || incs[0].Detail.Count != 20 || incs[0].Env != "staging" {
		t.Fatalf("incidents = %+v", incs)
	}
}

func TestSpikeResolvesAfterWindow(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	iss := seedIssue(t, pool, pid, "open")
	// 25 events at now − 40m: current at now is 25 (fires); at now + 30m the
	// window (now − 30m, now + 30m] holds 0.
	addEvents(t, pool, iss, 25, evalNow.Add(-40*time.Minute), "production")
	s := NewService(pool, nil, "")

	evaluate(t, s, evalNow)
	incs := spikeIncidents(t, pool, iss)
	if len(incs) != 1 || incs[0].Status != "firing" {
		t.Fatalf("at now: %+v", incs)
	}

	// condition false, but fired 30m ago < one 60m window → stays firing.
	evaluate(t, s, evalNow.Add(30*time.Minute))
	if st, rec := incidentStatus(t, pool, incs[0].ID); st != "firing" || rec != "none" {
		t.Fatalf("at now+30m: %s %s", st, rec)
	}

	// fired 61m ago ≥ 60m and condition false → resolved with a recovery pending.
	at := evalNow.Add(61 * time.Minute)
	evaluate(t, s, at)
	incs = spikeIncidents(t, pool, iss)
	if len(incs) != 1 || incs[0].Status != "resolved" || incs[0].Recovery != "pending" ||
		incs[0].ResolvedAt == nil || !incs[0].ResolvedAt.Equal(at) {
		t.Fatalf("at now+61m: %+v", incs)
	}
}

func TestSpikeStaysFiringWhileSpiking(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	iss := seedIssue(t, pool, pid, "open")
	// fired 2h ago, but 25 events in the last 60 min (baseline 0) → still spiking.
	inc := seedFiringSpike(t, pool, pid, iss, evalNow.Add(-2*time.Hour))
	addEvents(t, pool, iss, 25, evalNow.Add(-10*time.Minute), "production")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	if st, rec := incidentStatus(t, pool, inc); st != "firing" || rec != "none" {
		t.Fatalf("state = %s %s", st, rec)
	}
	if incs := spikeIncidents(t, pool, iss); len(incs) != 1 {
		t.Fatalf("incidents = %+v", incs)
	}
}

func TestSpikeQuietIssueResolves(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	// an issue with no events at all in the last day still has its firing spike resolved.
	iss := seedIssue(t, pool, pid, "open")
	inc := seedFiringSpike(t, pool, pid, iss, evalNow.Add(-3*time.Hour))
	addEvents(t, pool, iss, 100, evalNow.Add(-30*time.Hour), "production")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	if st, rec := incidentStatus(t, pool, inc); st != "resolved" || rec != "pending" {
		t.Fatalf("state = %s %s", st, rec)
	}
}

func TestSpikeCooldown(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool) // cooldown 60 min
	iss := seedIssue(t, pool, pid, "open")
	T := evalNow
	prev := seedFiringSpike(t, pool, pid, iss, T.Add(-3*time.Hour))
	if _, err := pool.Exec(context.Background(),
		`UPDATE alert_incidents SET status='resolved', resolved_at=$2 WHERE id=$1`, prev, T); err != nil {
		t.Fatal(err)
	}
	// the spike recurs: 25 events at T + 20m, baseline 0.
	addEvents(t, pool, iss, 25, T.Add(20*time.Minute), "production")
	s := NewService(pool, nil, "")

	// T + 30m: resolved 30m ago, within the 60m cooldown → no new incident.
	evaluate(t, s, T.Add(30*time.Minute))
	if incs := spikeIncidents(t, pool, iss); len(incs) != 1 {
		t.Fatalf("at T+30m: %+v", incs)
	}

	// T + 61m: cooldown over; the 25 events are still in (T + 1m, T + 61m] → new incident.
	evaluate(t, s, T.Add(61*time.Minute))
	incs := spikeIncidents(t, pool, iss)
	if len(incs) != 2 || incs[1].Status != "firing" || incs[1].Detail.Count != 25 {
		t.Fatalf("at T+61m: %+v", incs)
	}
}

func TestSpikeIgnoresIgnoredIssues(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	ignored := seedIssue(t, pool, pid, "ignored")
	addEvents(t, pool, ignored, 100, evalNow.Add(-10*time.Minute), "production")
	resolved := seedIssue(t, pool, pid, "resolved")
	addEvents(t, pool, resolved, 100, evalNow.Add(-10*time.Minute), "production")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	if incs := spikeIncidents(t, pool, ignored); len(incs) != 0 {
		t.Fatalf("ignored incidents = %+v", incs)
	}
	if incs := spikeIncidents(t, pool, resolved); len(incs) != 0 {
		t.Fatalf("resolved incidents = %+v", incs)
	}
}

func TestSpikeDisabledRuleDoesNotFire(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	setSpikeRule(t, pool, pid, `enabled = false`)
	iss := seedIssue(t, pool, pid, "open")
	addEvents(t, pool, iss, 100, evalNow.Add(-10*time.Minute), "production")

	evaluate(t, NewService(pool, nil, ""), evalNow)

	if incs := spikeIncidents(t, pool, iss); len(incs) != 0 {
		t.Fatalf("incidents = %+v", incs)
	}
}

func TestSpikeDisabledOrMissingRuleResolvesSilently(t *testing.T) {
	pool := testDB(t)
	_, disabled := seedProject(t, pool)
	setSpikeRule(t, pool, disabled, `enabled = false`)
	_, missing := seedSecondProject(t, pool)
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM alert_rules WHERE project_id = $1 AND kind = 'issue_spike'`, missing); err != nil {
		t.Fatal(err)
	}
	// both incidents fired 10m ago with the issues still spiking: without an
	// enabled rule they close anyway, and without a recovery message.
	var incs []int64
	for _, pid := range []int64{disabled, missing} {
		iss := seedIssue(t, pool, pid, "open")
		addEvents(t, pool, iss, 100, evalNow.Add(-5*time.Minute), "production")
		incs = append(incs, seedFiringSpike(t, pool, pid, iss, evalNow.Add(-10*time.Minute)))
	}

	evaluate(t, NewService(pool, nil, ""), evalNow)

	for _, id := range incs {
		if st, rec := incidentStatus(t, pool, id); st != "resolved" || rec != "none" {
			t.Fatalf("incident %d: %s %s", id, st, rec)
		}
	}
}

func TestEvaluateClosesResolvedIssueIncidents(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	ctx := context.Background()
	// seedIncident creates an open issue per incident; then the issue status changes.
	newIssue := seedIncident(t, pool, pid, KindNewIssue, "boom")
	regression := seedIncident(t, pool, pid, KindRegression, "boom")
	stillOpen := seedIncident(t, pool, pid, KindNewIssue, "boom")
	for id, status := range map[int64]string{newIssue: "resolved", regression: "ignored"} {
		if _, err := pool.Exec(ctx,
			`UPDATE issues SET status = $2 WHERE id = (SELECT issue_id FROM alert_incidents WHERE id = $1)`, id, status); err != nil {
			t.Fatal(err)
		}
	}

	evaluate(t, NewService(pool, nil, ""), evalNow)

	for _, id := range []int64{newIssue, regression} {
		if st, rec := incidentStatus(t, pool, id); st != "resolved" || rec != "none" {
			t.Fatalf("incident %d: %s %s", id, st, rec)
		}
		var resolvedAt time.Time
		if err := pool.QueryRow(ctx, `SELECT resolved_at FROM alert_incidents WHERE id=$1`, id).Scan(&resolvedAt); err != nil || !resolvedAt.Equal(evalNow) {
			t.Fatalf("resolved_at = %v, %v", resolvedAt, err)
		}
	}
	if st, _ := incidentStatus(t, pool, stillOpen); st != "firing" {
		t.Fatalf("open issue incident = %s", st)
	}
}

// TestEvaluateRecoveryOnlyForSpikes runs Evaluate and then the dispatcher:
// only the resolved spike produces a recovery message.
func TestEvaluateRecoveryOnlyForSpikes(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -1)
	ctx := context.Background()
	newIssue := seedIncident(t, pool, pid, KindNewIssue, "new one")
	regression := seedIncident(t, pool, pid, KindRegression, "regressed one")
	spike := seedIncident(t, pool, pid, KindIssueSpike, "spiky one")
	// fire messages already delivered; spike fired 2h ago and is now quiet.
	if _, err := pool.Exec(ctx, `UPDATE alert_incidents SET notify_state='sent', fired_at=$1`, evalNow.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issues SET status='resolved'
WHERE id IN (SELECT issue_id FROM alert_incidents WHERE id = ANY($1))`, []int64{newIssue, regression}); err != nil {
		t.Fatal(err)
	}
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app")

	evaluate(t, s, evalNow)
	for id, want := range map[int64]string{newIssue: "none", regression: "none", spike: "pending"} {
		if st, rec := incidentStatus(t, pool, id); st != "resolved" || rec != want {
			t.Fatalf("incident %d: %s %s, want resolved %s", id, st, rec, want)
		}
	}

	if err := s.DispatchPending(ctx, evalNow); err != nil {
		t.Fatal(err)
	}
	sent := fn.Sent()
	if len(sent) != 1 || !strings.HasPrefix(sent[0].HTML, "✅") || !strings.Contains(sent[0].HTML, "spiky one") {
		t.Fatalf("sent = %+v", sent)
	}
}
