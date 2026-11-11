package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tiofani03/artracker/backend/internal/alerts"
)

// Organisation bot and manual channel endpoints. Request bodies here can
// carry a bot token: no error message, response or log line may include a
// body, a token, or Telegram's own error text.

const (
	errTenantOff  = "tenant bots are disabled in this deployment"
	errBadToken   = "invalid bot token"
	errBadLevel   = "invalid detail level"
	errNoBot      = "no bot configured"
	errConnectBot = "connect a Telegram bot first"
	errBadJSON    = "invalid JSON body"
	errBadChat    = "invalid chat"
	errBadLabel   = "invalid label"
	errManualOff  = "manual channels are only available in tenant mode"

	// maxTokenBytes bounds a token before it reaches the service.
	maxTokenBytes = 256
	maxLabelRunes = 80
	// botWriteBudget covers the getMe check (10 s) plus the database write.
	botWriteBudget  = 15 * time.Second
	originLinked    = "linked"
	originManual    = "manual"
	duplicateRowErr = "23505"
)

// Chat destinations in canonical form only: a numeric chat id without leading
// zeros, "+" or whitespace, or a public @username (Telegram: 5 to 32
// characters). The dashboard validates with the same two patterns.
var (
	chatIDRe       = regexp.MustCompile(`^-?[1-9][0-9]{4,19}$`)
	chatUsernameRe = regexp.MustCompile(`^@[A-Za-z][A-Za-z0-9_]{4,31}$`)
)

func validChat(s string) bool { return chatIDRe.MatchString(s) || chatUsernameRe.MatchString(s) }

// validLabel: 1 to 80 Unicode code points, not only whitespace, and a single
// line with no control character (NUL, newline, tab, DEL, C1, ...). Postgres
// rejects NUL in text, so without this check a NUL would be a 500.
func validLabel(s string) bool {
	n := utf8.RuneCountInString(s)
	if n < 1 || n > maxLabelRunes || strings.TrimSpace(s) == "" {
		return false
	}
	return !strings.ContainsFunc(s, unicode.IsControl)
}

// botJSON is the organisation's bot as the API shows it: never the token.
type botJSON struct {
	alerts.BotView
	Mode alerts.BotMode `json:"mode"`
}

// alertsMode is the deployment's bot mode; without an alerts service nothing
// tenant-specific is available, which is shared mode's behaviour.
func (h *Handler) alertsMode() alerts.BotMode {
	if h.alertsSvc == nil {
		return alerts.ModeShared
	}
	return h.alertsSvc.Mode()
}

// orgBotJSON reports the organisation's bot in the current mode. In shared
// mode it is the unconfigured view (minimal detail).
func (h *Handler) orgBotJSON(ctx context.Context, orgID int64) (botJSON, error) {
	mode := h.alertsMode()
	if mode != alerts.ModeTenant {
		return botJSON{BotView: alerts.UnconfiguredBot(), Mode: mode}, nil
	}
	v, err := h.alertsSvc.BotStatus(ctx, orgID)
	if err != nil {
		return botJSON{}, err
	}
	return botJSON{BotView: v, Mode: mode}, nil
}

// decodeObject decodes exactly one JSON object with only known fields into
// dst. Any other shape (null, an array, trailing data) is rejected.
func decodeObject(body []byte, dst any) bool {
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] != '{' {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return false
	}
	_, err := dec.Token()
	return errors.Is(err, io.EOF)
}

// botServiceError maps the service's sentinels to fixed API errors. Anything
// else is an internal error whose text never carries the token.
func botServiceError(err error) error {
	switch {
	case errors.Is(err, alerts.ErrInvalidToken):
		return fiber.NewError(fiber.StatusBadRequest, errBadToken)
	case errors.Is(err, alerts.ErrInvalidLevel):
		return fiber.NewError(fiber.StatusBadRequest, errBadLevel)
	case errors.Is(err, alerts.ErrNoSealer):
		return fiber.NewError(fiber.StatusServiceUnavailable, "alerts master key is not configured")
	case errors.Is(err, alerts.ErrWrongMode):
		return fiber.NewError(fiber.StatusNotFound, errTenantOff)
	case errors.Is(err, alerts.ErrTelegramUnreachable):
		return fiber.NewError(fiber.StatusBadGateway, "could not reach Telegram")
	case errors.Is(err, alerts.ErrBotNotFound):
		return fiber.NewError(fiber.StatusNotFound, errNoBot)
	}
	return err
}

// GetOrgBot shows the organisation's bot to any member; never the token.
func (h *Handler) GetOrgBot(c *fiber.Ctx) error {
	orgID := c.Locals("orgID").(int64)
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()
	v, err := h.orgBotJSON(ctx, orgID)
	if err != nil {
		return err
	}
	return c.JSON(v)
}

// PutOrgBot validates a token with Telegram and stores it sealed, replacing
// any existing bot.
func (h *Handler) PutOrgBot(c *fiber.Ctx) error {
	if h.alertsMode() != alerts.ModeTenant {
		return fiber.NewError(fiber.StatusNotFound, errTenantOff)
	}
	orgID := c.Locals("orgID").(int64)
	var in struct {
		Token       *string `json:"token"`
		DetailLevel *string `json:"detail_level"`
	}
	if !decodeObject(c.Body(), &in) {
		return fiber.NewError(fiber.StatusBadRequest, errBadJSON)
	}
	if in.Token == nil || len(*in.Token) > maxTokenBytes {
		return fiber.NewError(fiber.StatusBadRequest, errBadToken)
	}
	var level alerts.DetailLevel
	if in.DetailLevel != nil {
		if *in.DetailLevel == "" {
			return fiber.NewError(fiber.StatusBadRequest, errBadLevel)
		}
		level = alerts.DetailLevel(*in.DetailLevel)
	}
	ctx, cancel := context.WithTimeout(c.Context(), botWriteBudget)
	defer cancel()
	v, err := h.alertsSvc.SetBot(ctx, orgID, userID(c), *in.Token, level)
	if err != nil {
		return botServiceError(err)
	}
	return c.JSON(botJSON{BotView: v, Mode: alerts.ModeTenant})
}

// PatchOrgBot changes the organisation's detail level.
func (h *Handler) PatchOrgBot(c *fiber.Ctx) error {
	if h.alertsMode() != alerts.ModeTenant {
		return fiber.NewError(fiber.StatusNotFound, errTenantOff)
	}
	orgID := c.Locals("orgID").(int64)
	var in struct {
		DetailLevel *string `json:"detail_level"`
	}
	if !decodeObject(c.Body(), &in) {
		return fiber.NewError(fiber.StatusBadRequest, errBadJSON)
	}
	if in.DetailLevel == nil {
		return fiber.NewError(fiber.StatusBadRequest, errBadLevel)
	}
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()
	v, err := h.alertsSvc.SetDetailLevel(ctx, orgID, alerts.DetailLevel(*in.DetailLevel))
	if err != nil {
		return botServiceError(err)
	}
	return c.JSON(botJSON{BotView: v, Mode: alerts.ModeTenant})
}

// DeleteOrgBot removes the organisation's bot; removing none is not an error.
func (h *Handler) DeleteOrgBot(c *fiber.Ctx) error {
	if h.alertsMode() != alerts.ModeTenant {
		return fiber.NewError(fiber.StatusNotFound, errTenantOff)
	}
	orgID := c.Locals("orgID").(int64)
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()
	if err := h.alertsSvc.DeleteBot(ctx, orgID); err != nil {
		return botServiceError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// projectOrg returns the organisation that owns the project.
func (h *Handler) projectOrg(ctx context.Context, projectID int64) (int64, error) {
	var orgID int64
	err := h.db.QueryRow(ctx, `SELECT org_id FROM projects WHERE id = $1`, projectID).Scan(&orgID)
	return orgID, err
}

// CreateAlertChannel adds a manual channel (tenant mode): a chat the
// organisation's bot delivers to. Telegram is not asked anything about the
// chat: the platform only calls sendMessage and getMe, so chat_type stays ”.
func (h *Handler) CreateAlertChannel(c *fiber.Ctx) error {
	if h.alertsMode() != alerts.ModeTenant {
		return fiber.NewError(fiber.StatusBadRequest, errManualOff)
	}
	projectID := c.Locals("projectID").(int64)
	var in struct {
		Chat  *string `json:"chat"`
		Label *string `json:"label"`
	}
	if !decodeObject(c.Body(), &in) {
		return fiber.NewError(fiber.StatusBadRequest, errBadJSON)
	}
	if in.Chat == nil || !validChat(*in.Chat) {
		return fiber.NewError(fiber.StatusBadRequest, errBadChat)
	}
	if in.Label == nil || !validLabel(*in.Label) {
		return fiber.NewError(fiber.StatusBadRequest, errBadLabel)
	}
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()

	orgID, err := h.projectOrg(ctx, projectID)
	if err != nil {
		return err
	}
	bot, err := h.alertsSvc.BotStatus(ctx, orgID)
	if err != nil {
		return err
	}
	if !bot.Configured || bot.Status != "active" {
		return fiber.NewError(fiber.StatusConflict, errConnectBot)
	}

	var createdBy *int64
	if uid := userID(c); uid > 0 {
		createdBy = &uid
	}
	ch, err := scanChannel(h.db.QueryRow(ctx, `
INSERT INTO alert_channels(project_id, kind, origin, destination, title, chat_type, created_by)
VALUES ($1, 'telegram', 'manual', $2, $3, '', $4)
RETURNING `+channelCols, projectID, *in.Chat, *in.Label, createdBy))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == duplicateRowErr {
		return fiber.NewError(fiber.StatusConflict, "chat already added")
	}
	if err != nil {
		return err
	}
	ch.applyMode(alerts.ModeTenant)
	return c.Status(fiber.StatusCreated).JSON(ch)
}

// DiscoverAlertChats queries the project's Telegram bot to return recently active chats
// so admins can pick a chat without having to manually look up numeric IDs.
func (h *Handler) DiscoverAlertChats(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	ctx, cancel := context.WithTimeout(c.Context(), alertQueryBudget)
	defer cancel()

	orgID, err := h.projectOrg(ctx, projectID)
	if err != nil {
		return err
	}

	chats, botUsername, err := h.alertsSvc.DiscoverChats(ctx, orgID)
	if errors.Is(err, alerts.ErrBotNotFound) {
		return fiber.NewError(fiber.StatusConflict, errConnectBot)
	}
	if errors.Is(err, alerts.ErrTelegramDisabled) {
		return fiber.NewError(fiber.StatusServiceUnavailable, errTelegramOff)
	}
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "failed to query Telegram updates: "+err.Error())
	}
	if chats == nil {
		chats = []alerts.DiscoveredChat{}
	}
	return c.JSON(fiber.Map{
		"bot_username": botUsername,
		"chats":        chats,
	})
}
