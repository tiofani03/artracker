package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Spike terms for a rule window W (spec §2), all relative to $now:
//   current  = events in (now − W, now]
//   baseline = events in (now − W − 24h, now − W] × W / 1440
//   fire iff current ≥ min_count AND current ≥ multiplier × max(baseline, 1)
// Counted events match the rule environment; legacy rows with '' match every rule.

// spikeRuleCTE selects project $1's enabled spike rule; $2 is now.
const spikeRuleCTE = `
r AS (
  SELECT environment AS env, spike_multiplier AS mult, spike_min_count AS min_count,
         spike_window_minutes AS wmin, make_interval(mins => spike_window_minutes) AS w,
         make_interval(mins => cooldown_minutes) AS cooldown
  FROM alert_rules
  WHERE project_id = $1 AND kind = 'issue_spike' AND enabled
)`

// envMatch filters issue_events e by the rule environment column ruleEnv.
func envMatch(ruleEnv string) string {
	return "(" + ruleEnv + " = '' OR e.environment = " + ruleEnv + " OR e.environment = '')"
}

// fireSpikesSQL takes as candidates the open issues seen within the window
// (bounded through issues_project_status_idx), counts both windows per
// candidate with one range scan of issue_events_issue_idx over the last
// W + 24h, and opens an incident for each issue over the threshold. The
// LATERAL keeps the counting per candidate, so the planner cannot fall back to
// hashing the whole issue_events table. The partial unique index absorbs an
// issue that is already firing.
var fireSpikesSQL = `
WITH ` + spikeRuleCTE + `,
c AS (
  SELECT i.id AS issue_id, i.title, r.mult, r.min_count, r.wmin, r.w, r.env, r.cooldown, k.cur, k.baseline
  FROM r
  JOIN issues i ON i.project_id = $1 AND i.status = 'open' AND i.last_seen > $2::timestamptz - r.w
  CROSS JOIN LATERAL (
    SELECT count(*) FILTER (WHERE e.created_at > $2::timestamptz - r.w) AS cur,
           count(*) FILTER (WHERE e.created_at <= $2::timestamptz - r.w)::float8 * r.wmin / 1440 AS baseline
    FROM issue_events e
    WHERE e.issue_id = i.id
      AND e.created_at > $2::timestamptz - r.w - interval '24 hours'
      AND e.created_at <= $2::timestamptz
      AND ` + envMatch("r.env") + `
  ) k
)
INSERT INTO alert_incidents(project_id, rule_kind, issue_id, title, environment, release_version, session_id,
                            detail, fired_at, next_attempt_at)
SELECT $1, 'issue_spike', c.issue_id, c.title, l.environment, l.release_version, l.session_id,
       jsonb_build_object('count', c.cur, 'baseline', c.baseline, 'window_minutes', c.wmin),
       $2::timestamptz, $2::timestamptz
FROM c
CROSS JOIN LATERAL (
  SELECT e.environment, e.release_version, e.session_id
  FROM issue_events e
  WHERE e.issue_id = c.issue_id AND e.created_at > $2::timestamptz - c.w AND e.created_at <= $2::timestamptz
    AND ` + envMatch("c.env") + `
  ORDER BY e.created_at DESC, e.id DESC
  LIMIT 1
) l
WHERE c.cur >= c.min_count AND c.cur >= c.mult * greatest(c.baseline, 1)
  AND NOT EXISTS (
    SELECT 1 FROM alert_incidents p
    WHERE p.project_id = $1 AND p.rule_kind = 'issue_spike' AND p.issue_id = c.issue_id
      AND p.status = 'resolved' AND p.resolved_at > $2::timestamptz - c.cooldown)
ON CONFLICT (project_id, rule_kind, issue_id) WHERE status = 'firing' DO NOTHING`

// resolveSpikesSQL recomputes the counts for every firing spike incident of
// the project that has fired for at least one window, whether or not its
// issue saw events recently, and resolves those no longer over the threshold.
var resolveSpikesSQL = `
WITH ` + spikeRuleCTE + `,
f AS (
  SELECT a.id, r.mult, r.min_count,
    (SELECT count(*) FROM issue_events e
     WHERE e.issue_id = a.issue_id AND e.created_at > $2::timestamptz - r.w AND e.created_at <= $2::timestamptz
       AND ` + envMatch("r.env") + `) AS cur,
    (SELECT count(*) FROM issue_events e
     WHERE e.issue_id = a.issue_id AND e.created_at > $2::timestamptz - r.w - interval '24 hours'
       AND e.created_at <= $2::timestamptz - r.w
       AND ` + envMatch("r.env") + `)::float8 * r.wmin / 1440 AS baseline
  FROM alert_incidents a CROSS JOIN r
  WHERE a.project_id = $1 AND a.rule_kind = 'issue_spike' AND a.status = 'firing'
    AND a.fired_at <= $2::timestamptz - r.w
)
UPDATE alert_incidents a
SET status = 'resolved', resolved_at = $2::timestamptz, recovery_notify_state = 'pending'
FROM f
WHERE a.id = f.id AND NOT (f.cur >= f.min_count AND f.cur >= f.mult * greatest(f.baseline, 1))`

// silentClose resolves an incident without a recovery message and drops a fire
// message that has not gone out yet.
const silentClose = `status = 'resolved', resolved_at = $2::timestamptz,
    notify_state = CASE WHEN notify_state = 'pending' THEN 'skipped' ELSE notify_state END`

// closeOrphanSpikesSQL silently resolves firing spike incidents whose rule is
// missing or disabled, so they do not stay firing forever.
const closeOrphanSpikesSQL = `
UPDATE alert_incidents a
SET ` + silentClose + `
WHERE a.project_id = $1 AND a.rule_kind = 'issue_spike' AND a.status = 'firing'
  AND NOT EXISTS (SELECT 1 FROM alert_rules r
                  WHERE r.project_id = $1 AND r.kind = 'issue_spike' AND r.enabled)`

// closeIssueIncidentsSQL silently resolves firing incidents of every kind once
// their issue is no longer open: whoever resolved or ignored it already knows
// (spec §3), and spikes are defined only for open issues (spec §2).
const closeIssueIncidentsSQL = `
UPDATE alert_incidents a
SET ` + silentClose + `
FROM issues i
WHERE a.project_id = $1 AND a.status = 'firing'
  AND i.id = a.issue_id AND i.status <> 'open'`

// Evaluate runs one evaluator tick at now: it closes incidents whose condition
// has ended and opens spike incidents. Projects are handled independently; a
// failure in one is logged and the rest still run. The returned error joins
// the database failures.
func (s *Service) Evaluate(ctx context.Context, now time.Time) error {
	rows, err := s.db.Query(ctx, `
SELECT project_id FROM alert_rules WHERE kind = 'issue_spike' AND enabled
UNION
SELECT project_id FROM alert_incidents WHERE status = 'firing'`)
	if err != nil {
		return fmt.Errorf("alerts: list projects to evaluate: %w", err)
	}
	var pids []int64
	for rows.Next() {
		var pid int64
		if err := rows.Scan(&pid); err != nil {
			rows.Close()
			return fmt.Errorf("alerts: scan project: %w", err)
		}
		pids = append(pids, pid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("alerts: list projects to evaluate: %w", err)
	}

	var errs []error
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if err := s.evaluateProject(ctx, now, pid); err != nil {
			slog.Error("alerts: evaluate project", "project_id", pid, "error_class", errClass(err))
			errs = append(errs, fmt.Errorf("alerts: evaluate project %d: %w", pid, err))
		}
	}
	return errors.Join(errs...)
}

// evaluateProject resolves before it fires, so an incident opened in this tick
// is not re-examined until the next one.
func (s *Service) evaluateProject(ctx context.Context, now time.Time, pid int64) error {
	for _, step := range []struct{ name, sql string }{
		{"close issue incidents", closeIssueIncidentsSQL},
		{"close orphan spikes", closeOrphanSpikesSQL},
		{"resolve spikes", resolveSpikesSQL},
		{"fire spikes", fireSpikesSQL},
	} {
		if _, err := s.db.Exec(ctx, step.sql, pid, now); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}
