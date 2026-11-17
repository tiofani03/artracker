package people

import (
	"sort"
	"time"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

// IdentifyEventName is the telemetry item name SDKs use for identify(userId, traits).
const IdentifyEventName = "$identify"

// IsSessionStart reports whether a telemetry item name marks the start of a session (Android / iOS spelling).
func IsSessionStart(name string) bool { return name == "session_start" || name == "session.start" }

// SessionDelta is what one flushed batch says about one session.
type SessionDelta struct {
	ProjectID int64
	SessionID string
	UserID    string
	FirstSeen time.Time
	LastSeen  time.Time
	HasError  bool
	Context   map[string]any // nil unless a session-start row was seen
}

// ProfileDelta is what one flushed batch says about one identified user.
type ProfileDelta struct {
	ProjectID     int64
	UserID        string
	FirstSeen     time.Time
	LastSeen      time.Time
	LastSessionID string
	Traits        map[string]any // merged in time order; nil value = delete key; nil map if no $identify
	LastContext   map[string]any // nil unless a session-start row for this user was seen
}

// Batch groups the per-session and per-user deltas of one flush.
type Batch struct {
	Sessions []SessionDelta
	Profiles []ProfileDelta
}

func (b Batch) Empty() bool { return len(b.Sessions) == 0 && len(b.Profiles) == 0 }

type projectKey struct {
	project int64
	id      string
}

// Aggregate folds normalized telemetry rows into per-session and per-user deltas. Pure and deterministic:
// rows are processed in event-time order regardless of slice order.
func Aggregate(rows []ingest.TelemetryRow) Batch {
	sorted := make([]ingest.TelemetryRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].EventTimestamp.Before(sorted[j].EventTimestamp) })

	sessions := map[projectKey]*SessionDelta{}
	profiles := map[projectKey]*ProfileDelta{}
	var sessionOrder, profileOrder []projectKey

	for _, r := range sorted {
		ts := r.EventTimestamp
		start := IsSessionStart(r.Name)
		var ctx map[string]any
		if start {
			ctx = sanitizeContext(r.Attributes)
		}

		if r.SessionID != "" {
			k := projectKey{r.ProjectID, r.SessionID}
			s, ok := sessions[k]
			if !ok {
				s = &SessionDelta{ProjectID: r.ProjectID, SessionID: r.SessionID, FirstSeen: ts, LastSeen: ts}
				sessions[k] = s
				sessionOrder = append(sessionOrder, k)
			}
			if ts.Before(s.FirstSeen) {
				s.FirstSeen = ts
			}
			if ts.After(s.LastSeen) {
				s.LastSeen = ts
			}
			if s.UserID == "" && r.UserID != "" {
				s.UserID = r.UserID
			}
			if r.Level == "error" || r.Level == "fatal" {
				s.HasError = true
			}
			if start {
				s.Context = ctx
			}
		}

		if r.UserID != "" {
			k := projectKey{r.ProjectID, r.UserID}
			p, ok := profiles[k]
			if !ok {
				p = &ProfileDelta{ProjectID: r.ProjectID, UserID: r.UserID, FirstSeen: ts, LastSeen: ts}
				profiles[k] = p
				profileOrder = append(profileOrder, k)
			}
			if ts.Before(p.FirstSeen) {
				p.FirstSeen = ts
			}
			if !ts.Before(p.LastSeen) {
				p.LastSeen = ts
				if r.SessionID != "" {
					p.LastSessionID = r.SessionID
				}
			}
			if r.SessionID != "" && p.LastSessionID == "" {
				p.LastSessionID = r.SessionID
			}
			if start {
				p.LastContext = ctx
			}
			if r.Name == IdentifyEventName {
				if p.Traits == nil {
					p.Traits = map[string]any{}
				}
				for tk, tv := range SanitizeTraits(r.Attributes) {
					p.Traits[tk] = tv
				}
			}
		}
	}

	var b Batch
	for _, k := range sessionOrder {
		b.Sessions = append(b.Sessions, *sessions[k])
	}
	for _, k := range profileOrder {
		b.Profiles = append(b.Profiles, *profiles[k])
	}
	return b
}
