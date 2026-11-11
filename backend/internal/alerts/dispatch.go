package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxNotifyAttempts = 5
	// digestThreshold: more pending fire notifications than this for one
	// project in one tick are sent as a single digest.
	digestThreshold  = 3
	retryBackoffStep = 30 * time.Second
	maxRetryDelay    = time.Hour
	forbiddenReason  = "bot was removed from the chat"
	maxLastErrorLen  = 500
)

// notify stages, named by the incident column that tracks them.
const (
	stageFire     = "notify_state"
	stageRecovery = "recovery_notify_state"
)

type pendingIncident struct {
	Incident
	OrgID         int64
	ProjectName   string
	RecoveryState string
}

type channel struct{ id, chatID int64 }

// delivery is the outcome of sending one message to a project's channels.
type delivery struct {
	sent       bool          // at least one channel accepted the message
	retryAfter time.Duration // > 0 when Telegram rate-limited us
}

// DispatchPending delivers every due fire and recovery notification. All time
// comparisons and schedules use now. A failure in one project is logged and
// does not stop the others; the returned error joins the database failures.
func (s *Service) DispatchPending(ctx context.Context, now time.Time) error {
	if s.n == nil {
		return s.skipAllPending(ctx)
	}
	incs, err := s.loadDue(ctx, now)
	if err != nil {
		return fmt.Errorf("alerts: load pending incidents: %w", err)
	}
	var errs []error
	for _, group := range groupByProject(incs) {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		pid := group[0].ProjectID
		if err := s.dispatchProject(ctx, now, group); err != nil {
			slog.Error("alerts: dispatch project", "project_id", pid, "error_class", errClass(err))
			errs = append(errs, fmt.Errorf("alerts: project %d: %w", pid, err))
		}
	}
	return errors.Join(errs...)
}

// skipAllPending is the Telegram-off path: nothing can be delivered.
func (s *Service) skipAllPending(ctx context.Context) error {
	_, err1 := s.db.Exec(ctx, `UPDATE alert_incidents SET notify_state='skipped' WHERE notify_state='pending'`)
	_, err2 := s.db.Exec(ctx, `UPDATE alert_incidents SET recovery_notify_state='skipped' WHERE recovery_notify_state='pending'`)
	if err := errors.Join(err1, err2); err != nil {
		return fmt.Errorf("alerts: skip pending: %w", err)
	}
	return nil
}

func (s *Service) loadDue(ctx context.Context, now time.Time) ([]pendingIncident, error) {
	rows, err := s.db.Query(ctx, `
SELECT i.id, i.project_id, i.issue_id, i.rule_kind, i.status, i.title, i.environment, i.release_version,
       i.session_id, i.detail, i.fired_at, i.resolved_at, i.notify_state, i.recovery_notify_state,
       p.org_id, p.name
FROM alert_incidents i
JOIN projects p ON p.id = i.project_id
WHERE (i.notify_state = 'pending' OR i.recovery_notify_state = 'pending') AND i.next_attempt_at <= $1
ORDER BY i.project_id, i.fired_at, i.id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pendingIncident
	for rows.Next() {
		var pi pendingIncident
		var kind string
		var detail []byte
		if err := rows.Scan(&pi.ID, &pi.ProjectID, &pi.IssueID, &kind, &pi.Status, &pi.Title, &pi.Environment,
			&pi.ReleaseVersion, &pi.SessionID, &detail, &pi.FiredAt, &pi.ResolvedAt, &pi.NotifyState,
			&pi.RecoveryState, &pi.OrgID, &pi.ProjectName); err != nil {
			return nil, err
		}
		pi.RuleKind = RuleKind(kind)
		if pi.RuleKind == KindIssueSpike {
			var sd SpikeDetail
			if json.Unmarshal(detail, &sd) == nil && sd.WindowMinutes > 0 {
				pi.Spike = &sd
			}
		}
		out = append(out, pi)
	}
	return out, rows.Err()
}

// groupByProject splits incidents (ordered by project) into per-project runs.
func groupByProject(incs []pendingIncident) [][]pendingIncident {
	var groups [][]pendingIncident
	for i := 0; i < len(incs); {
		j := i
		for j < len(incs) && incs[j].ProjectID == incs[i].ProjectID {
			j++
		}
		groups = append(groups, incs[i:j])
		i = j
	}
	return groups
}

func (s *Service) dispatchProject(ctx context.Context, now time.Time, incs []pendingIncident) error {
	pid := incs[0].ProjectID
	allIDs := make([]int64, len(incs))
	for i, inc := range incs {
		allIDs[i] = inc.ID
	}
	chans, err := s.enabledChannels(ctx, pid)
	if err != nil {
		return err
	}
	if len(chans) == 0 {
		return s.skipBothStages(ctx, allIDs)
	}

	mc := MessageContext{ProjectName: incs[0].ProjectName, OrgID: incs[0].OrgID, ProjectID: pid, PublicURL: s.publicURL}
	// fireState is each incident's fire notify_state after this tick's sends;
	// a recovery goes out only once its fire message has.
	fireState := make(map[int64]string, len(incs))
	var fires []pendingIncident
	for _, inc := range incs {
		fireState[inc.ID] = inc.NotifyState
		if inc.NotifyState == "pending" {
			fires = append(fires, inc)
		}
	}

	// deliverAndSettle sends html for ids in the given stage and records the
	// result. It reports true when the project must stop for this tick.
	deliverAndSettle := func(stage, html string, ids []int64) (bool, error) {
		d, err := s.deliver(ctx, now, pid, &chans, html)
		if err != nil {
			return true, err
		}
		if d.retryAfter > 0 && !d.sent {
			slog.Warn("alerts: telegram rate limited", "project_id", pid, "incident_ids", ids)
			return true, s.deferAll(ctx, allIDs, now.Add(d.retryAfter))
		}
		states, err := s.settle(ctx, now, stage, ids, d.sent, len(chans) == 0)
		if err != nil {
			return true, err
		}
		if stage == stageFire {
			for id, st := range states {
				fireState[id] = st
			}
		}
		if d.retryAfter > 0 {
			// Delivered to some channels, but Telegram asked us to back off.
			return true, s.deferAll(ctx, allIDs, now.Add(d.retryAfter))
		}
		return false, nil
	}

	if len(fires) > digestThreshold {
		batch := make([]Incident, len(fires))
		ids := make([]int64, len(fires))
		for i, f := range fires {
			batch[i], ids[i] = f.Incident, f.ID
		}
		if stop, err := deliverAndSettle(stageFire, FormatDigest(batch, mc), ids); stop || err != nil {
			return err
		}
	} else {
		for _, f := range fires {
			fmc := mc
			if fmc.HasReplay, err = s.hasReplay(ctx, pid, f.SessionID); err != nil {
				return err
			}
			if stop, err := deliverAndSettle(stageFire, FormatFire(f.Incident, fmc), []int64{f.ID}); stop || err != nil {
				return err
			}
		}
	}

	for _, inc := range incs {
		if inc.RecoveryState != "pending" {
			continue
		}
		switch fireState[inc.ID] {
		case "sent":
			if stop, err := deliverAndSettle(stageRecovery, FormatRecovered(inc.Incident, mc), []int64{inc.ID}); stop || err != nil {
				return err
			}
		case "failed", "skipped":
			// Nobody saw the alert, so a recovery message would only confuse.
			if _, err := s.db.Exec(ctx, `UPDATE alert_incidents SET recovery_notify_state='skipped' WHERE id=$1`, inc.ID); err != nil {
				return err
			}
		}
		// Still pending: the fire is retrying; the recovery waits for it.
	}
	return nil
}

func (s *Service) enabledChannels(ctx context.Context, projectID int64) ([]channel, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, telegram_chat_id FROM alert_channels WHERE project_id=$1 AND enabled ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []channel
	for rows.Next() {
		var c channel
		if err := rows.Scan(&c.id, &c.chatID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) hasReplay(ctx context.Context, projectID int64, sessionID string) (bool, error) {
	if sessionID == "" {
		return false, nil
	}
	var ok bool
	err := s.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM replay_sessions WHERE project_id=$1 AND id=$2)`, projectID, sessionID).Scan(&ok)
	return ok, err
}

// deliver sends html to every channel in chans, handling Telegram's typed
// errors. chans is updated to the channels still usable afterwards. It returns
// an error only for database failures or cancellation.
func (s *Service) deliver(ctx context.Context, now time.Time, projectID int64, chans *[]channel, html string) (delivery, error) {
	var d delivery
	kept := make([]channel, 0, len(*chans))
	for i, ch := range *chans {
		err := s.n.SendMessage(ctx, ch.chatID, html)
		var mig *MigratedError
		if errors.As(err, &mig) {
			merged, merr := s.migrateChannel(ctx, ch.id, mig.NewChatID)
			if merr != nil {
				return d, merr
			}
			if merged {
				// Another channel of this project already has the new chat
				// id; the stale row is gone and that channel delivers.
				slog.Info("alerts: migrated chat merged into existing channel", "project_id", projectID, "channel_id", ch.id)
				continue
			}
			ch.chatID = mig.NewChatID
			err = s.n.SendMessage(ctx, ch.chatID, html)
		}
		var ra *RetryAfterError
		switch {
		case err == nil:
			d.sent = true
			kept = append(kept, ch)
			if err := s.markChannelSent(ctx, ch.id, now); err != nil {
				return d, err
			}
		case errors.As(err, &ra):
			d.retryAfter = clampDelay(time.Duration(ra.Seconds) * time.Second)
			*chans = append(kept, (*chans)[i:]...)
			return d, nil
		case errors.Is(err, ErrChatForbidden):
			slog.Warn("alerts: channel disabled", "project_id", projectID, "channel_id", ch.id, "error_class", errClass(err))
			if err := s.disableChannelForbidden(ctx, ch.id); err != nil {
				return d, err
			}
		default:
			if ctx.Err() != nil {
				return d, ctx.Err() // cancellation is not a delivery failure
			}
			slog.Warn("alerts: telegram send failed", "project_id", projectID, "channel_id", ch.id, "error_class", errClass(err))
			kept = append(kept, ch)
			if err := s.markChannelFailed(ctx, ch.id, err); err != nil {
				return d, err
			}
		}
	}
	*chans = kept
	return d, nil
}

// Channel bookkeeping shared by dispatch and SendTest.

func (s *Service) markChannelSent(ctx context.Context, channelID int64, now time.Time) error {
	_, err := s.db.Exec(ctx, `UPDATE alert_channels SET last_sent_at=$2, last_error='' WHERE id=$1`, channelID, now)
	return err
}

func (s *Service) markChannelFailed(ctx context.Context, channelID int64, sendErr error) error {
	_, err := s.db.Exec(ctx, `UPDATE alert_channels SET last_error=$2 WHERE id=$1`, channelID, truncateRunes(sendErr.Error(), maxLastErrorLen))
	return err
}

func (s *Service) disableChannelForbidden(ctx context.Context, channelID int64) error {
	_, err := s.db.Exec(ctx, `UPDATE alert_channels SET enabled=false, disabled_reason=$2 WHERE id=$1`, channelID, forbiddenReason)
	return err
}

// migrateChannel points a channel at its supergroup's new chat id. When the
// project already has a channel for that id, the stale row is deleted instead
// and merged is true.
func (s *Service) migrateChannel(ctx context.Context, channelID, newChatID int64) (merged bool, err error) {
	_, err = s.db.Exec(ctx, `UPDATE alert_channels SET telegram_chat_id=$2 WHERE id=$1`, channelID, newChatID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		_, err = s.db.Exec(ctx, `DELETE FROM alert_channels WHERE id=$1`, channelID)
		return err == nil, err
	}
	return false, err
}

// settle records a delivery result for ids in one stage and returns each
// incident's resulting state in that stage.
func (s *Service) settle(ctx context.Context, now time.Time, stage string, ids []int64, sent, noChannelsLeft bool) (map[int64]string, error) {
	var query string
	args := []any{ids}
	switch {
	case sent:
		query = `UPDATE alert_incidents SET ` + stage + `='sent', notify_attempts=0 WHERE id = ANY($1) RETURNING id, ` + stage
	case noChannelsLeft:
		query = `UPDATE alert_incidents SET ` + stage + `='skipped' WHERE id = ANY($1) RETURNING id, ` + stage
	default:
		slog.Warn("alerts: notification not delivered", "incident_ids", ids, "stage", stage)
		query = `UPDATE alert_incidents SET
  notify_attempts = notify_attempts + 1,
  ` + stage + ` = CASE WHEN notify_attempts + 1 >= $2 THEN 'failed' ELSE ` + stage + ` END,
  next_attempt_at = $3::timestamptz + LEAST($4 * (notify_attempts + 1), $5) * interval '1 second'
WHERE id = ANY($1) RETURNING id, ` + stage
		args = append(args, maxNotifyAttempts, now, int(retryBackoffStep.Seconds()), int(maxRetryDelay.Seconds()))
	}
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := make(map[int64]string, len(ids))
	for rows.Next() {
		var id int64
		var st string
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		states[id] = st
	}
	return states, rows.Err()
}

// deferAll pushes every still-pending incident of ids to at least until.
func (s *Service) deferAll(ctx context.Context, ids []int64, until time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE alert_incidents SET next_attempt_at = GREATEST(next_attempt_at, $2)
WHERE id = ANY($1) AND (notify_state = 'pending' OR recovery_notify_state = 'pending')`, ids, until)
	return err
}

func (s *Service) skipBothStages(ctx context.Context, ids []int64) error {
	_, err := s.db.Exec(ctx, `
UPDATE alert_incidents SET
  notify_state = CASE WHEN notify_state = 'pending' THEN 'skipped' ELSE notify_state END,
  recovery_notify_state = CASE WHEN recovery_notify_state = 'pending' THEN 'skipped' ELSE recovery_notify_state END
WHERE id = ANY($1)`, ids)
	return err
}

func clampDelay(d time.Duration) time.Duration {
	return max(min(d, maxRetryDelay), time.Second)
}

// errClass names an error without its text, which may carry user data.
func errClass(err error) string {
	var ra *RetryAfterError
	var mig *MigratedError
	var api *APIError
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &ra):
		return "rate_limited"
	case errors.As(err, &mig):
		return "chat_migrated"
	case errors.Is(err, ErrChatForbidden):
		return "forbidden"
	case errors.As(err, &api):
		return fmt.Sprintf("api_error_%d", api.Code)
	case errors.As(err, &pgErr):
		return "postgres_" + pgErr.Code
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	}
	return fmt.Sprintf("%T", err)
}
