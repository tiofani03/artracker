// Package alerts holds the alerting domain: rules, incidents and the signals
// ingest emits about issues.
package alerts

import "time"

// RuleKind identifies which condition an alert rule watches.
type RuleKind string

const (
	KindNewIssue   RuleKind = "new_issue"
	KindRegression RuleKind = "regression"
	KindIssueSpike RuleKind = "issue_spike"
)

// AllKinds lists every rule kind a project has a rule for.
var AllKinds = []RuleKind{KindNewIssue, KindRegression, KindIssueSpike}

// SeedRulesSQL creates the default rule of every kind for a project ($1),
// relying on the column defaults of alert_rules.
const SeedRulesSQL = `INSERT INTO alert_rules(project_id, kind)
SELECT $1::bigint, k FROM unnest(ARRAY['new_issue','regression','issue_spike']) k
ON CONFLICT DO NOTHING`

type Rule struct {
	ProjectID          int64     `json:"project_id"`
	Kind               RuleKind  `json:"kind"`
	Enabled            bool      `json:"enabled"`
	Environment        string    `json:"environment"`
	SpikeMultiplier    float64   `json:"spike_multiplier"`
	SpikeMinCount      int       `json:"spike_min_count"`
	SpikeWindowMinutes int       `json:"spike_window_minutes"`
	CooldownMinutes    int       `json:"cooldown_minutes"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type SpikeDetail struct {
	Count         int     `json:"count"`
	Baseline      float64 `json:"baseline"`
	WindowMinutes int     `json:"window_minutes"`
}

type Incident struct {
	ID             int64        `json:"id"`
	ProjectID      int64        `json:"project_id"`
	IssueID        int64        `json:"issue_id"`
	RuleKind       RuleKind     `json:"rule_kind"`
	Status         string       `json:"status"` // firing | resolved
	Title          string       `json:"title"`
	Environment    string       `json:"environment"`
	ReleaseVersion string       `json:"release_version"`
	SessionID      string       `json:"session_id"`
	Spike          *SpikeDetail `json:"spike,omitempty"`
	FiredAt        time.Time    `json:"fired_at"`
	ResolvedAt     *time.Time   `json:"resolved_at,omitempty"`
	NotifyState    string       `json:"notify_state"`
}

// IssueSignal is what ingest reports when an issue is new or has regressed.
type IssueSignal struct {
	ProjectID      int64
	IssueID        int64
	Kind           RuleKind
	Title          string
	Environment    string
	ReleaseVersion string
	SessionID      string
}
