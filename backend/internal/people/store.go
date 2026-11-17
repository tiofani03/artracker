package people

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Person is an end user of an instrumented app, as shown on the People page.
type Person struct {
	UserID            string         `json:"user_id"`
	Traits            map[string]any `json:"traits"`
	FirstSeen         time.Time      `json:"first_seen"`
	LastSeen          time.Time      `json:"last_seen"`
	SessionCount      int64          `json:"session_count"`
	ErrorSessionCount int64          `json:"error_session_count"`
	LastContext       map[string]any `json:"last_context"`
}

// PersonSession is one session of a person.
type PersonSession struct {
	SessionID string         `json:"session_id"`
	FirstSeen time.Time      `json:"first_seen"`
	LastSeen  time.Time      `json:"last_seen"`
	HasError  bool           `json:"has_error"`
	HasReplay bool           `json:"has_replay"`
	Context   map[string]any `json:"context"`
}

// Store reads and writes user_profiles and session_contexts.
type Store struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func jsonObject(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func splitTraits(m map[string]any) (set []byte, deleted []string) {
	keep := map[string]any{}
	for k, v := range m {
		if v == nil {
			deleted = append(deleted, k)
		} else {
			keep[k] = v
		}
	}
	sort.Strings(deleted)
	if deleted == nil {
		deleted = []string{}
	}
	return jsonObject(keep), deleted
}

const upsertSessionSQL = `
INSERT INTO session_contexts (project_id, session_id, user_id, first_seen, last_seen, has_error, context)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
ON CONFLICT (project_id, session_id) DO UPDATE SET
    user_id    = CASE WHEN session_contexts.user_id = '' THEN EXCLUDED.user_id ELSE session_contexts.user_id END,
    first_seen = LEAST(session_contexts.first_seen, EXCLUDED.first_seen),
    last_seen  = GREATEST(session_contexts.last_seen, EXCLUDED.last_seen),
    has_error  = session_contexts.has_error OR EXCLUDED.has_error,
    context    = CASE WHEN EXCLUDED.context <> '{}'::jsonb THEN EXCLUDED.context ELSE session_contexts.context END,
    updated_at = now()`

const upsertProfileSQL = `
INSERT INTO user_profiles (project_id, user_id, traits, first_seen, last_seen, last_session_id, last_context)
VALUES ($1, $2, $3::jsonb - $8::text[], $4, $5, $6, $7::jsonb)
ON CONFLICT (project_id, user_id) DO UPDATE SET
    traits          = (user_profiles.traits || EXCLUDED.traits) - $8::text[],
    first_seen      = LEAST(user_profiles.first_seen, EXCLUDED.first_seen),
    last_seen       = GREATEST(user_profiles.last_seen, EXCLUDED.last_seen),
    last_session_id = CASE WHEN EXCLUDED.last_seen >= user_profiles.last_seen AND EXCLUDED.last_session_id <> ''
                           THEN EXCLUDED.last_session_id ELSE user_profiles.last_session_id END,
    last_context    = CASE WHEN EXCLUDED.last_seen >= user_profiles.last_seen AND EXCLUDED.last_context <> '{}'::jsonb
                           THEN EXCLUDED.last_context ELSE user_profiles.last_context END,
    updated_at      = now()`

// Apply writes one batch in a single transaction. Rows are written in key order so concurrent workers lock
// them in the same order (no deadlocks).
func (s *Store) Apply(ctx context.Context, b Batch) error {
	if s == nil || s.db == nil || b.Empty() {
		return nil
	}
	sessions := append([]SessionDelta(nil), b.Sessions...)
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].ProjectID != sessions[j].ProjectID {
			return sessions[i].ProjectID < sessions[j].ProjectID
		}
		return sessions[i].SessionID < sessions[j].SessionID
	})
	profiles := append([]ProfileDelta(nil), b.Profiles...)
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].ProjectID != profiles[j].ProjectID {
			return profiles[i].ProjectID < profiles[j].ProjectID
		}
		return profiles[i].UserID < profiles[j].UserID
	})

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, d := range sessions {
		if _, err := tx.Exec(ctx, upsertSessionSQL, d.ProjectID, d.SessionID, d.UserID, d.FirstSeen, d.LastSeen,
			d.HasError, jsonObject(d.Context)); err != nil {
			return fmt.Errorf("upsert session context: %w", err)
		}
	}
	for _, d := range profiles {
		set, deleted := splitTraits(d.Traits)
		if _, err := tx.Exec(ctx, upsertProfileSQL, d.ProjectID, d.UserID, set, d.FirstSeen, d.LastSeen,
			d.LastSessionID, jsonObject(d.LastContext), deleted); err != nil {
			return fmt.Errorf("upsert user profile: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// TouchFromReplay records a session (and its user, when known) seen through a replay chunk upload: the session
// started at start and was still active at seen.
func (s *Store) TouchFromReplay(ctx context.Context, projectID int64, userID, sessionID string, start, seen time.Time) error {
	if sessionID == "" {
		return nil
	}
	if seen.Before(start) {
		seen = start
	}
	b := Batch{Sessions: []SessionDelta{{ProjectID: projectID, SessionID: sessionID, UserID: userID, FirstSeen: start, LastSeen: seen}}}
	if userID != "" {
		b.Profiles = []ProfileDelta{{ProjectID: projectID, UserID: userID, FirstSeen: start, LastSeen: seen, LastSessionID: sessionID}}
	}
	return s.Apply(ctx, b)
}

// likeEscape escapes LIKE wildcards so user input matches literally (ESCAPE '\').
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

const personColumns = `
    p.user_id, p.traits, p.first_seen, p.last_seen, p.last_context,
    COALESCE(c.n, 0), COALESCE(c.e, 0)
FROM user_profiles p
LEFT JOIN LATERAL (
    SELECT count(*) AS n, count(*) FILTER (WHERE sc.has_error OR COALESCE(r.has_error, false)) AS e
    FROM session_contexts sc
    LEFT JOIN replay_sessions r ON r.project_id = sc.project_id AND r.id = sc.session_id
    WHERE sc.project_id = p.project_id AND sc.user_id = p.user_id
) c ON true`

func scanPerson(row pgx.Row) (*Person, error) {
	var p Person
	var traits, lastCtx []byte
	if err := row.Scan(&p.UserID, &traits, &p.FirstSeen, &p.LastSeen, &lastCtx, &p.SessionCount, &p.ErrorSessionCount); err != nil {
		return nil, err
	}
	p.Traits = decodeObject(traits)
	p.LastContext = decodeObject(lastCtx)
	return &p, nil
}

func decodeObject(b []byte) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

const peopleSearchWhere = `p.project_id = $1 AND ($2 = '' OR starts_with(lower(p.user_id), lower($2))
    OR EXISTS (SELECT 1 FROM jsonb_each_text(p.traits) t WHERE t.value ILIKE '%' || $3 || '%' ESCAPE '\'))`

// ListPeople returns one page of people, most recently seen first, plus the total match count.
func (s *Store) ListPeople(ctx context.Context, projectID int64, search string, limit, offset int) ([]Person, int, error) {
	var total int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM user_profiles p WHERE `+peopleSearchWhere,
		projectID, search, likeEscape(search)).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+personColumns+` WHERE `+peopleSearchWhere+`
        ORDER BY p.last_seen DESC, p.user_id LIMIT $4 OFFSET $5`,
		projectID, search, likeEscape(search), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *p)
	}
	return items, total, rows.Err()
}

// GetPerson returns the person, or nil when the project has no profile for that user id.
func (s *Store) GetPerson(ctx context.Context, projectID int64, userID string) (*Person, error) {
	p, err := scanPerson(s.db.QueryRow(ctx, `SELECT `+personColumns+` WHERE p.project_id = $1 AND p.user_id = $2`, projectID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// PersonSessions lists a user's sessions, newest first, leaving out excludeSessionID.
func (s *Store) PersonSessions(ctx context.Context, projectID int64, userID, excludeSessionID string, limit int) ([]PersonSession, error) {
	rows, err := s.db.Query(ctx, `
        SELECT sc.session_id, sc.first_seen, sc.last_seen,
               sc.has_error OR COALESCE(r.has_error, false), sc.context, COALESCE(r.chunk_count, 0) > 0
        FROM session_contexts sc
        LEFT JOIN replay_sessions r ON r.project_id = sc.project_id AND r.id = sc.session_id
        WHERE sc.project_id = $1 AND sc.user_id = $2 AND sc.session_id <> $3
        ORDER BY sc.last_seen DESC, sc.session_id
        LIMIT $4`, projectID, userID, excludeSessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PersonSession{}
	for rows.Next() {
		var ps PersonSession
		var c []byte
		if err := rows.Scan(&ps.SessionID, &ps.FirstSeen, &ps.LastSeen, &ps.HasError, &c, &ps.HasReplay); err != nil {
			return nil, err
		}
		ps.Context = decodeObject(c)
		out = append(out, ps)
	}
	return out, rows.Err()
}

// SessionContext returns the user id and device context recorded for a session. Unknown sessions fall back to
// the replay row's user id, with an empty context.
func (s *Store) SessionContext(ctx context.Context, projectID int64, sessionID string) (string, map[string]any, error) {
	var userID string
	var c []byte
	err := s.db.QueryRow(ctx, `SELECT user_id, context FROM session_contexts WHERE project_id = $1 AND session_id = $2`,
		projectID, sessionID).Scan(&userID, &c)
	if err == nil {
		if userID == "" {
			userID = s.replayUserID(ctx, projectID, sessionID)
		}
		return userID, decodeObject(c), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	return s.replayUserID(ctx, projectID, sessionID), map[string]any{}, nil
}

func (s *Store) replayUserID(ctx context.Context, projectID int64, sessionID string) string {
	var userID string
	_ = s.db.QueryRow(ctx, `SELECT user_id FROM replay_sessions WHERE project_id = $1 AND id = $2`, projectID, sessionID).Scan(&userID)
	return userID
}
