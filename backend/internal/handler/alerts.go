package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tiofani03/artracker/backend/internal/alerts"
)

const (
	errTelegramOff   = "telegram is not configured on this server"
	errTelegramTest  = "telegram rejected the test message"
	alertQueryBudget = 5 * time.Second
)

type ruleJSON struct {
	Kind               alerts.RuleKind `json:"kind"`
	Enabled            bool            `json:"enabled"`
	Environment        string          `json:"environment"`
	SpikeMultiplier    float64         `json:"spike_multiplier"`
	SpikeMinCount      int             `json:"spike_min_count"`
	SpikeWindowMinutes int             `json:"spike_window_minutes"`
	CooldownMinutes    int             `json:"cooldown_minutes"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

const ruleCols = `kind, enabled, environment, spike_multiplier, spike_min_count, spike_window_minutes, cooldown_minutes, updated_at`

func scanRule(row pgx.Row) (ruleJSON, error) {
	var r ruleJSON
	err := row.Scan(&r.Kind, &r.Enabled, &r.Environment, &r.SpikeMultiplier, &r.SpikeMinCount,
		&r.SpikeWindowMinutes, &r.CooldownMinutes, &r.UpdatedAt)
	return r, err
}

// ListAlertRules returns the project's alert rules in the order new_issue, regression, issue_spike.
func (h *Handler) ListAlertRules(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()

	rows, err := h.db.Query(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE project_id = $1
		ORDER BY array_position(ARRAY['new_issue','regression','issue_spike'], kind)`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []ruleJSON{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items})
}

// UpdateAlertRule applies a partial update to one rule. Omitted fields stay unchanged.
func (h *Handler) UpdateAlertRule(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	kind := alerts.RuleKind(c.Params("kind"))
	known := false
	for _, k := range alerts.AllKinds {
		known = known || k == kind
	}
	if !known {
		return fiber.NewError(fiber.StatusNotFound, "unknown alert rule")
	}

	var in struct {
		Enabled            *bool    `json:"enabled"`
		Environment        *string  `json:"environment"`
		SpikeMultiplier    *float64 `json:"spike_multiplier"`
		SpikeMinCount      *int     `json:"spike_min_count"`
		SpikeWindowMinutes *int     `json:"spike_window_minutes"`
		CooldownMinutes    *int     `json:"cooldown_minutes"`
	}
	if err := json.Unmarshal(c.Body(), &in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	spikeOnly := in.SpikeMultiplier != nil || in.SpikeMinCount != nil || in.SpikeWindowMinutes != nil || in.CooldownMinutes != nil
	if in.Enabled == nil && in.Environment == nil && !spikeOnly {
		return fiber.NewError(fiber.StatusBadRequest, "no recognised field to update")
	}
	if spikeOnly && kind != alerts.KindIssueSpike {
		return fiber.NewError(fiber.StatusBadRequest,
			"spike_multiplier, spike_min_count, spike_window_minutes and cooldown_minutes apply only to issue_spike")
	}
	switch {
	case in.Environment != nil && len(*in.Environment) > 64:
		return fiber.NewError(fiber.StatusBadRequest, "environment must be at most 64 characters")
	case in.SpikeMultiplier != nil && (*in.SpikeMultiplier < 1.5 || *in.SpikeMultiplier > 100):
		return fiber.NewError(fiber.StatusBadRequest, "spike_multiplier must be between 1.5 and 100")
	case in.SpikeMinCount != nil && (*in.SpikeMinCount < 1 || *in.SpikeMinCount > 100000):
		return fiber.NewError(fiber.StatusBadRequest, "spike_min_count must be between 1 and 100000")
	case in.SpikeWindowMinutes != nil && !validSpikeWindow(*in.SpikeWindowMinutes):
		return fiber.NewError(fiber.StatusBadRequest, "spike_window_minutes must be one of 15, 30, 60, 180")
	case in.CooldownMinutes != nil && (*in.CooldownMinutes < 0 || *in.CooldownMinutes > 1440):
		return fiber.NewError(fiber.StatusBadRequest, "cooldown_minutes must be between 0 and 1440")
	}

	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()
	r, err := scanRule(h.db.QueryRow(ctx, `
UPDATE alert_rules SET
  enabled              = COALESCE($3, enabled),
  environment          = COALESCE($4, environment),
  spike_multiplier     = COALESCE($5, spike_multiplier),
  spike_min_count      = COALESCE($6, spike_min_count),
  spike_window_minutes = COALESCE($7, spike_window_minutes),
  cooldown_minutes     = COALESCE($8, cooldown_minutes),
  updated_at           = now()
WHERE project_id = $1 AND kind = $2
RETURNING `+ruleCols,
		projectID, string(kind), in.Enabled, in.Environment, in.SpikeMultiplier, in.SpikeMinCount,
		in.SpikeWindowMinutes, in.CooldownMinutes))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "unknown alert rule")
	}
	if err != nil {
		return err
	}
	return c.JSON(r)
}

func validSpikeWindow(m int) bool {
	return m == 15 || m == 30 || m == 60 || m == 180
}

// channelJSON deliberately omits the Telegram chat id of a linked channel.
// Destination is the admin-typed chat of a manual channel ("" for linked);
// Active is derived from the bot mode (see applyMode).
type channelJSON struct {
	ID             int64      `json:"id"`
	Kind           string     `json:"kind"`
	Origin         string     `json:"origin"`
	Destination    string     `json:"destination"`
	Active         bool       `json:"active"`
	Title          string     `json:"title"`
	ChatType       string     `json:"chat_type"`
	Enabled        bool       `json:"enabled"`
	DisabledReason string     `json:"disabled_reason"`
	LastSentAt     *time.Time `json:"last_sent_at"`
	LastError      string     `json:"last_error"`
	CreatedAt      time.Time  `json:"created_at"`
}

// channelCols never includes telegram_chat_id, which is NULL for manual
// channels and is not part of the JSON.
const channelCols = `id, kind, origin, destination, title, chat_type, enabled, disabled_reason, last_sent_at, last_error, created_at`

func scanChannel(row pgx.Row) (channelJSON, error) {
	var ch channelJSON
	err := row.Scan(&ch.ID, &ch.Kind, &ch.Origin, &ch.Destination, &ch.Title, &ch.ChatType, &ch.Enabled,
		&ch.DisabledReason, &ch.LastSentAt, &ch.LastError, &ch.CreatedAt)
	return ch, err
}

// applyMode derives Active (linked channels work in shared mode, manual ones
// in tenant mode) and hides the destination of linked channels.
func (ch *channelJSON) applyMode(mode alerts.BotMode) {
	if ch.Origin != originManual {
		ch.Destination = ""
	}
	if mode == alerts.ModeTenant {
		ch.Active = ch.Origin == originManual
	} else {
		ch.Active = ch.Origin == originLinked
	}
}

// channelIDParam parses :channelID; anything but a positive integer is "not found".
func channelIDParam(c *fiber.Ctx) (int64, error) {
	id, err := strconv.ParseInt(c.Params("channelID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fiber.NewError(fiber.StatusNotFound, "channel not found")
	}
	return id, nil
}

// ListAlertChannels returns the project's channels, the bot mode, the
// organisation's bot and (shared mode) whether the shared bot is available.
func (h *Handler) ListAlertChannels(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()
	mode := h.alertsMode()

	rows, err := h.db.Query(ctx, `SELECT `+channelCols+` FROM alert_channels WHERE project_id = $1
		ORDER BY created_at ASC, id ASC`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []channelJSON{}
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return err
		}
		ch.applyMode(mode)
		items = append(items, ch)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	tg := fiber.Map{"configured": false, "bot_username": ""}
	if h.alertsSvc != nil {
		tg = fiber.Map{"configured": h.alertsSvc.TelegramConfigured(), "bot_username": h.alertsSvc.BotUsername()}
	}
	bot := botJSON{BotView: alerts.UnconfiguredBot(), Mode: mode}
	if mode == alerts.ModeTenant {
		orgID, err := h.projectOrg(ctx, projectID)
		if err != nil {
			return err
		}
		if bot, err = h.orgBotJSON(ctx, orgID); err != nil {
			return err
		}
	}
	return c.JSON(fiber.Map{"telegram": tg, "mode": mode, "bot": bot, "items": items})
}

// CreateAlertLinkCode issues a short-lived code that binds a Telegram chat to the project.
func (h *Handler) CreateAlertLinkCode(c *fiber.Ctx) error {
	if h.alertsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, errTelegramOff)
	}
	if h.alertsSvc.Mode() == alerts.ModeTenant {
		return fiber.NewError(fiber.StatusNotFound, "link flow is disabled in tenant mode")
	}
	projectID := c.Locals("projectID").(int64)
	userID, _ := c.Locals("userID").(int64)
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()

	lc, err := h.alertsSvc.CreateLinkCode(ctx, projectID, userID)
	if errors.Is(err, alerts.ErrTelegramDisabled) {
		return fiber.NewError(fiber.StatusServiceUnavailable, errTelegramOff)
	}
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(lc)
}

// UpdateAlertChannel enables or disables a channel. Enabling clears the disabled reason.
func (h *Handler) UpdateAlertChannel(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	id, err := channelIDParam(c)
	if err != nil {
		return err
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(c.Body(), &in); err != nil || in.Enabled == nil {
		return fiber.NewError(fiber.StatusBadRequest, "enabled (boolean) is required")
	}
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()

	ch, err := scanChannel(h.db.QueryRow(ctx, `
UPDATE alert_channels SET enabled = $3,
  disabled_reason = CASE WHEN $3 THEN '' ELSE disabled_reason END
WHERE id = $1 AND project_id = $2
RETURNING `+channelCols, id, projectID, *in.Enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "channel not found")
	}
	if err != nil {
		return err
	}
	ch.applyMode(h.alertsMode())
	return c.JSON(ch)
}

// DeleteAlertChannel removes a channel of the project.
func (h *Handler) DeleteAlertChannel(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	id, err := channelIDParam(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()
	tag, err := h.db.Exec(ctx, `DELETE FROM alert_channels WHERE id = $1 AND project_id = $2`, id, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fiber.NewError(fiber.StatusNotFound, "channel not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// TestAlertChannel sends a test message to one channel, through the shared
// bot or (tenant mode) the organisation's bot. Telegram's error text is never
// echoed.
func (h *Handler) TestAlertChannel(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	id, err := channelIDParam(c)
	if err != nil {
		return err
	}
	if h.alertsSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, errTelegramOff)
	}
	ctx, cancel := context.WithTimeout(c.Context(), 15*time.Second)
	defer cancel()

	switch err := h.alertsSvc.SendTest(ctx, projectID, id); {
	case err == nil:
		return c.SendStatus(fiber.StatusNoContent)
	case errors.Is(err, alerts.ErrChannelNotFound):
		return fiber.NewError(fiber.StatusNotFound, "channel not found")
	case errors.Is(err, alerts.ErrBotNotFound):
		return fiber.NewError(fiber.StatusConflict, errConnectBot)
	case errors.Is(err, alerts.ErrTelegramDisabled):
		return fiber.NewError(fiber.StatusServiceUnavailable, errTelegramOff)
	default:
		return fiber.NewError(fiber.StatusBadGateway, errTelegramTest)
	}
}

type spikeJSON struct {
	Count         int     `json:"count"`
	Baseline      float64 `json:"baseline"`
	WindowMinutes int     `json:"window_minutes"`
}

type incidentJSON struct {
	ID             int64           `json:"id"`
	RuleKind       alerts.RuleKind `json:"rule_kind"`
	IssueID        int64           `json:"issue_id"`
	Status         string          `json:"status"`
	Title          string          `json:"title"`
	Environment    string          `json:"environment"`
	ReleaseVersion string          `json:"release_version"`
	SessionID      string          `json:"session_id"`
	Spike          *spikeJSON      `json:"spike"`
	FiredAt        time.Time       `json:"fired_at"`
	ResolvedAt     *time.Time      `json:"resolved_at"`
	NotifyState    string          `json:"notify_state"`
}

// ListAlertIncidents lists incidents, newest first. ?status=firing|resolved|all, ?limit=1..200.
func (h *Handler) ListAlertIncidents(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	status := c.Query("status", "all")
	if status != "firing" && status != "resolved" && status != "all" {
		return fiber.NewError(fiber.StatusBadRequest, "status must be firing, resolved or all")
	}
	limit := 50
	if n, err := strconv.Atoi(c.Query("limit")); err == nil {
		limit = min(max(n, 1), 200)
	}
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()

	rows, err := h.db.Query(ctx, `
SELECT id, rule_kind, issue_id, status, title, environment, release_version, session_id,
       detail, fired_at, resolved_at, notify_state
FROM alert_incidents
WHERE project_id = $1 AND ($2 = 'all' OR status = $2)
ORDER BY fired_at DESC, id DESC
LIMIT $3`, projectID, status, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []incidentJSON{}
	for rows.Next() {
		var it incidentJSON
		var detail []byte
		if err := rows.Scan(&it.ID, &it.RuleKind, &it.IssueID, &it.Status, &it.Title, &it.Environment,
			&it.ReleaseVersion, &it.SessionID, &detail, &it.FiredAt, &it.ResolvedAt, &it.NotifyState); err != nil {
			return err
		}
		var sd spikeJSON
		if json.Unmarshal(detail, &sd) == nil && sd.WindowMinutes > 0 {
			it.Spike = &sd
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items})
}
