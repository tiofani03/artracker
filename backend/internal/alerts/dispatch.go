package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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

	// pendingMaxAge: in tenant mode, a notification still pending this long
	// after its incident fired (no working bot all that time) is skipped.
	pendingMaxAge = 24 * time.Hour
	// noBotRecheck is how long a tenant-mode incident waits before the
	// dispatcher looks for a working bot again, so it does not spin.
	noBotRecheck     = time.Minute
	botInvalidReason = "token rejected by Telegram"
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

// Channel origins (alert_channels.origin). A channel is active only in the
// bot mode that matches its origin; this is derived at read time.
const (
	originLinked = "linked" // shared mode: telegram_chat_id, via the shared bot
	originManual = "manual" // tenant mode: destination, via the org's bot
)

// channel is one delivery target; dest is the Telegram chat as sent on the wire
// (the destination of a manual channel, the decimal chat id of a linked one).
type channel struct {
	id     int64
	dest   string
	origin string
}

// delivery is the outcome of sending one message to a project's channels.
type delivery struct {
	sent         bool          // at least one channel accepted the message
	retryAfter   time.Duration // > 0 when Telegram rate-limited us
	unauthorized bool          // tenant mode: Telegram rejected the org's token
}

// orgState is what one tick knows about an organisation in tenant mode.
type orgState struct {
	bot     *orgBot // nil: no working bot
	blocked bool    // rate limited this tick; its pending incidents are deferred
	invalid bool    // token rejected this tick; its pending incidents are deferred
}

// sender is the bot a project's messages go through. org is nil in shared mode.
type sender struct {
	n     Notifier
	orgID int64
	org   *orgState
}

// DispatchPending delivers every due fire and recovery notification. All time
// comparisons and schedules use now. A failure in one project is logged and
// does not stop the others; the returned error joins the database failures.
//
// In shared mode every project goes through s.n, and a nil s.n means Telegram
// is off. In tenant mode s.n is unused: each organisation's own bot is
// resolved once per tick.
func (s *Service) DispatchPending(ctx context.Context, now time.Time) error {
	tenant := s.mode == ModeTenant
	if !tenant && s.n == nil {
		return s.skipAllPending(ctx)
	}
	incs, err := s.loadDue(ctx, now)
	if err != nil {
		return fmt.Errorf("alerts: load pending incidents: %w", err)
	}
	orgs := map[int64]*orgState{}
	var errs []error
	for _, group := range groupByProject(incs) {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		pid := group[0].ProjectID
		if tenant {
			err = s.dispatchTenantProject(ctx, now, orgs, group)
		} else {
			err = s.dispatchProject(ctx, now, group, sender{n: s.n})
		}
		if err != nil {
			slog.Error("alerts: dispatch project", "project_id", pid, "error_class", errClass(err))
			errs = append(errs, fmt.Errorf("alerts: project %d: %w", pid, err))
		}
	}
	return errors.Join(errs...)
}

// dispatchTenantProject sends one project's notifications through its
// organisation's bot. Without a working bot nothing is sent: the incidents
// stay pending (rechecked after noBotRecheck) until they are pendingMaxAge
// old. An error resolving the bot leaves the project untouched.
func (s *Service) dispatchTenantProject(ctx context.Context, now time.Time, orgs map[int64]*orgState, incs []pendingIncident) error {
	orgID := incs[0].OrgID
	st, err := s.resolveOrg(ctx, orgs, orgID)
	if err != nil {
		return err
	}
	switch {
	case st.blocked || st.invalid:
		return nil // the whole organisation was deferred earlier this tick
	case st.bot == nil:
		return s.holdWithoutBot(ctx, now, incs)
	}
	return s.dispatchProject(ctx, now, incs, sender{n: st.bot.tg, orgID: orgID, org: st})
}

// resolveOrg returns the tick's state for orgID, decrypting its bot on first
// use. A failure is not cached, so each project reports it.
func (s *Service) resolveOrg(ctx context.Context, orgs map[int64]*orgState, orgID int64) (*orgState, error) {
	if st, ok := orgs[orgID]; ok {
		return st, nil
	}
	bot, err := s.workingBot(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("alerts: resolve bot for org %d: %w", orgID, err)
	}
	st := &orgState{bot: bot}
	orgs[orgID] = st
	return st, nil
}

// holdWithoutBot keeps a project's notifications pending while its
// organisation has no working bot. Each stage ages from when it became
// pending: a fire expires pendingMaxAge after fired_at, a recovery
// pendingMaxAge after resolved_at (fired_at when unknown). A recovery whose
// fire nobody saw (skipped or failed) is skipped, as in normal delivery. Every
// stage still pending afterwards is rechecked after noBotRecheck.
func (s *Service) holdWithoutBot(ctx context.Context, now time.Time, incs []pendingIncident) error {
	ids := make([]int64, len(incs))
	for i, inc := range incs {
		ids[i] = inc.ID
	}
	cutoff := now.Add(-pendingMaxAge)
	// The fire runs first so a fire expired here also skips its recovery.
	fires, err := s.skipStageWhere(ctx, stageFire, ids, `fired_at <= $2`, cutoff)
	if err != nil {
		return err
	}
	recoveries, err := s.skipStageWhere(ctx, stageRecovery, ids, `
notify_state IN ('skipped', 'failed') OR (notify_state = 'sent' AND COALESCE(resolved_at, fired_at) <= $2)`, cutoff)
	if err != nil {
		return err
	}
	if len(fires)+len(recoveries) > 0 {
		slog.Warn("alerts: notifications skipped without a working bot", "org_id", incs[0].OrgID,
			"project_id", incs[0].ProjectID, "fire_incident_ids", fires, "recovery_incident_ids", recoveries)
	}
	return s.deferAll(ctx, ids, now.Add(noBotRecheck))
}

// skipStageWhere sets stage to skipped for the ids whose stage is pending and
// that match cond ($2 is cutoff), returning their ids.
func (s *Service) skipStageWhere(ctx context.Context, stage string, ids []int64, cond string, cutoff time.Time) ([]int64, error) {
	rows, err := s.db.Query(ctx, `
UPDATE alert_incidents SET `+stage+` = 'skipped'
WHERE id = ANY($1) AND `+stage+` = 'pending' AND (`+cond+`)
RETURNING id`, ids, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
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

// dispatchProject sends one project's notifications through snd. In tenant
// mode only manual channels are active, in shared mode only linked ones.
func (s *Service) dispatchProject(ctx context.Context, now time.Time, incs []pendingIncident, snd sender) error {
	pid := incs[0].ProjectID
	allIDs := make([]int64, len(incs))
	for i, inc := range incs {
		allIDs[i] = inc.ID
	}
	origin := originLinked
	if snd.org != nil {
		origin = originManual
	}
	chans, err := s.enabledChannels(ctx, pid, origin)
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
		d, err := s.deliver(ctx, now, snd, pid, &chans, html)
		if err != nil {
			return true, err
		}
		halted := d.retryAfter > 0 || d.unauthorized
		if halted && !d.sent {
			if d.retryAfter > 0 {
				slog.Warn("alerts: telegram rate limited", "project_id", pid, "incident_ids", ids)
			}
			return true, s.halt(ctx, now, snd, allIDs, d)
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
		if halted {
			// Delivered to some channels, but the rest must wait.
			return true, s.halt(ctx, now, snd, allIDs, d)
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

// halt records why a project's delivery stopped mid-tick. In shared mode a
// rate limit defers this project only, as it always has. In tenant mode the
// bot is the unit: a rate limit or a rejected token defers every pending
// incident of the organisation and stops it for the rest of the tick.
func (s *Service) halt(ctx context.Context, now time.Time, snd sender, projectIDs []int64, d delivery) error {
	if snd.org == nil {
		return s.deferAll(ctx, projectIDs, now.Add(d.retryAfter))
	}
	if d.unauthorized {
		snd.org.invalid = true
		slog.Warn("alerts: org bot token rejected; bot marked invalid", "org_id", snd.orgID)
		if err := s.markBotInvalid(ctx, snd.orgID, snd.org.bot.sealed); err != nil {
			return err
		}
		return s.deferOrg(ctx, snd.orgID, now.Add(noBotRecheck))
	}
	snd.org.blocked = true
	slog.Warn("alerts: org bot rate limited", "org_id", snd.orgID, "retry_after", d.retryAfter.String())
	return s.deferOrg(ctx, snd.orgID, now.Add(d.retryAfter))
}

// markBotInvalid marks the organisation's bot invalid only while it still
// holds the token that was rejected (sealed is the ciphertext that was
// decrypted for the send), so a late 401 for an old token never invalidates
// a token saved in the meantime.
func (s *Service) markBotInvalid(ctx context.Context, orgID int64, sealed []byte) error {
	_, err := s.db.Exec(ctx, `
UPDATE alert_bots SET status='invalid', status_reason=$2, updated_at=now()
WHERE org_id=$1 AND token_ciphertext=$3`, orgID, botInvalidReason, sealed)
	return err
}

// deferOrg pushes every pending incident of every project of the organisation
// to at least until, never shortening an existing backoff.
func (s *Service) deferOrg(ctx context.Context, orgID int64, until time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE alert_incidents i SET next_attempt_at = GREATEST(i.next_attempt_at, $2)
FROM projects p
WHERE p.id = i.project_id AND p.org_id = $1
  AND (i.notify_state = 'pending' OR i.recovery_notify_state = 'pending')`, orgID, until)
	return err
}

// enabledChannels lists the project's enabled channels of one origin.
func (s *Service) enabledChannels(ctx context.Context, projectID int64, origin string) ([]channel, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, destination, telegram_chat_id FROM alert_channels
WHERE project_id=$1 AND origin=$2 AND enabled
  AND CASE WHEN origin='manual' THEN destination <> '' ELSE telegram_chat_id IS NOT NULL END
ORDER BY id`, projectID, origin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []channel
	for rows.Next() {
		c := channel{origin: origin}
		var dest string
		var chatID *int64
		if err := rows.Scan(&c.id, &dest, &chatID); err != nil {
			return nil, err
		}
		if origin == originManual {
			c.dest = dest
		} else {
			c.dest = strconv.FormatInt(*chatID, 10)
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

// deliver sends html through snd to every channel in chans, handling
// Telegram's typed errors. chans is updated to the channels still usable
// afterwards. It returns an error only for database failures or cancellation.
func (s *Service) deliver(ctx context.Context, now time.Time, snd sender, projectID int64, chans *[]channel, html string) (delivery, error) {
	var d delivery
	kept := make([]channel, 0, len(*chans))
	for i, ch := range *chans {
		err := snd.n.SendMessage(ctx, ch.dest, html)
		var mig *MigratedError
		if errors.As(err, &mig) {
			merged, merr := s.migrateChannel(ctx, ch, mig.NewChatID)
			if merr != nil {
				return d, merr
			}
			if merged {
				// Another channel of this project already has the new chat
				// id; the stale row is gone and that channel delivers.
				slog.Info("alerts: migrated chat merged into existing channel", "project_id", projectID, "channel_id", ch.id)
				continue
			}
			ch.dest = strconv.FormatInt(mig.NewChatID, 10)
			err = snd.n.SendMessage(ctx, ch.dest, html)
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
		case snd.org != nil && errors.Is(err, ErrUnauthorized):
			// The org's token, not this channel, is at fault: stop here and
			// leave the channel as it is.
			d.unauthorized = true
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

// migrateChannel points a channel at its supergroup's new chat id: the
// destination of a manual channel, the telegram_chat_id of a linked one. When
// the project already has a channel for that chat, the stale row is deleted
// instead and merged is true.
func (s *Service) migrateChannel(ctx context.Context, ch channel, newChatID int64) (merged bool, err error) {
	if ch.origin == originManual {
		_, err = s.db.Exec(ctx, `UPDATE alert_channels SET destination=$2 WHERE id=$1`, ch.id, strconv.FormatInt(newChatID, 10))
	} else {
		_, err = s.db.Exec(ctx, `UPDATE alert_channels SET telegram_chat_id=$2 WHERE id=$1`, ch.id, newChatID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		_, err = s.db.Exec(ctx, `DELETE FROM alert_channels WHERE id=$1`, ch.id)
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
	case errors.Is(err, ErrSealOpen):
		return "seal_open"
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
