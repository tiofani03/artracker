package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// BotMode selects who owns the Telegram bot: one platform bot shared by every
// organisation (the original link-code flow) or one bot per organisation.
type BotMode string

const (
	ModeTenant BotMode = "tenant"
	ModeShared BotMode = "shared"
)

// DetailLevel is how much of an issue an organisation's messages carry.
type DetailLevel string

const (
	DetailMinimal DetailLevel = "minimal"
	DetailFull    DetailLevel = "full"
)

// DefaultTelegramAPIBase is the Bot API host. It is operator configuration
// only; nothing tenant-supplied ever replaces it.
const DefaultTelegramAPIBase = "https://api.telegram.org"

var (
	ErrNoSealer            = errors.New("alerts master key is not configured")
	ErrWrongMode           = errors.New("not available in this bot mode")
	ErrInvalidToken        = errors.New("telegram rejected the bot token")
	ErrBotNotFound         = errors.New("no bot configured")
	ErrTelegramUnreachable = errors.New("could not reach Telegram")
	ErrInvalidLevel        = errors.New("invalid detail level")
)

// botTokenRe is the shape of a Bot API token: numeric bot id, colon, secret.
var botTokenRe = regexp.MustCompile(`^[0-9]{5,15}:[A-Za-z0-9_-]{30,}$`)

const getMeTimeout = 10 * time.Second

// WithMode sets the bot mode and returns the receiver.
func (s *Service) WithMode(m BotMode) *Service {
	s.mode = m
	return s
}

func (s *Service) Mode() BotMode { return s.mode }

// WithSealer sets the sealer for bot tokens and returns the receiver.
func (s *Service) WithSealer(sl Sealer) *Service {
	s.sealer = sl
	return s
}

// WithAPIBase sets the operator-level Bot API host and returns the receiver.
func (s *Service) WithAPIBase(base string) *Service {
	s.apiBase = base
	return s
}

// BotView is what the API shows about an organisation's bot. It never carries
// the token or its ciphertext.
type BotView struct {
	Configured   bool        `json:"configured"`
	BotUsername  string      `json:"bot_username"`
	Status       string      `json:"status"` // "active" | "invalid" | "" when not configured
	StatusReason string      `json:"status_reason"`
	DetailLevel  DetailLevel `json:"detail_level"`
	UpdatedAt    *time.Time  `json:"updated_at"`
}

const botViewCols = `bot_username, status, status_reason, detail_level, updated_at`

func scanBotView(row pgx.Row) (BotView, error) {
	var v BotView
	var level string
	var updated time.Time
	if err := row.Scan(&v.BotUsername, &v.Status, &v.StatusReason, &level, &updated); err != nil {
		return BotView{}, err
	}
	v.Configured = true
	v.DetailLevel = levelFromDB(level)
	v.UpdatedAt = &updated
	return v, nil
}

// orgBot is an organisation's decrypted, usable bot. It lives only for the
// request or tick that needs it.
type orgBot struct {
	tg       *Telegram
	level    DetailLevel
	username string
	// sealed is the token_ciphertext this client was decrypted from, so a
	// rejection can be pinned to that exact token. Internal: never logged,
	// marshaled or returned outside the package.
	sealed []byte
}

func validLevel(l DetailLevel) bool { return l == DetailMinimal || l == DetailFull }

// levelFromDB maps a stored detail level to the one messages use. Anything
// but exactly "full" is minimal, so a damaged value fails closed on privacy.
func levelFromDB(s string) DetailLevel {
	if s == string(DetailFull) {
		return DetailFull
	}
	return DetailMinimal
}

// BotStatus reports the organisation's bot. Any member may call it; nothing is
// decrypted. In shared mode it is the zero view.
func (s *Service) BotStatus(ctx context.Context, orgID int64) (BotView, error) {
	if s.mode != ModeTenant {
		return BotView{}, nil
	}
	v, err := scanBotView(s.db.QueryRow(ctx, `SELECT `+botViewCols+` FROM alert_bots WHERE org_id = $1`, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return BotView{DetailLevel: DetailMinimal}, nil
	}
	return v, err
}

// SetBot validates token with getMe and stores it sealed, replacing any
// existing bot and reactivating it. level "" keeps the existing level, or
// minimal for a new bot. The token is never logged or put in an error.
func (s *Service) SetBot(ctx context.Context, orgID, userID int64, token string, level DetailLevel) (BotView, error) {
	if s.mode != ModeTenant {
		return BotView{}, ErrWrongMode
	}
	if level != "" && !validLevel(level) {
		return BotView{}, ErrInvalidLevel
	}
	if s.sealer == nil {
		return BotView{}, ErrNoSealer
	}
	token = strings.TrimSpace(token)
	if !botTokenRe.MatchString(token) {
		return BotView{}, ErrInvalidToken
	}

	gctx, cancel := context.WithTimeout(ctx, getMeTimeout)
	info, err := NewTelegram(token, s.apiBase, nil).GetBot(gctx)
	cancel()
	if errors.Is(err, ErrUnauthorized) {
		return BotView{}, ErrInvalidToken
	}
	if err != nil {
		slog.Warn("alerts: bot token check failed", "org_id", orgID, "error_class", errClass(err))
		return BotView{}, ErrTelegramUnreachable
	}

	pt := []byte(token)
	blob, keyID, err := s.sealer.Seal(orgID, pt)
	for i := range pt {
		pt[i] = 0
	}
	if err != nil {
		return BotView{}, errors.New("alerts: cannot seal bot token")
	}

	var createdBy *int64
	if userID > 0 {
		createdBy = &userID
	}
	return scanBotView(s.db.QueryRow(ctx, `
INSERT INTO alert_bots(org_id, token_ciphertext, key_id, bot_id, bot_username, status, status_reason,
                       detail_level, created_by, last_validated_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 'active', '', COALESCE(NULLIF($6::text, ''), 'minimal'), $7, now(), now())
ON CONFLICT (org_id) DO UPDATE SET
    token_ciphertext  = EXCLUDED.token_ciphertext,
    key_id            = EXCLUDED.key_id,
    bot_id            = EXCLUDED.bot_id,
    bot_username      = EXCLUDED.bot_username,
    status            = 'active',
    status_reason     = '',
    detail_level      = COALESCE(NULLIF($6::text, ''), alert_bots.detail_level),
    last_validated_at = now(),
    updated_at        = now()
RETURNING `+botViewCols,
		orgID, blob, keyID, info.ID, info.Username, string(level), createdBy))
}

// SetDetailLevel changes the organisation's detail level.
func (s *Service) SetDetailLevel(ctx context.Context, orgID int64, level DetailLevel) (BotView, error) {
	if s.mode != ModeTenant {
		return BotView{}, ErrWrongMode
	}
	if !validLevel(level) {
		return BotView{}, ErrInvalidLevel
	}
	v, err := scanBotView(s.db.QueryRow(ctx, `
UPDATE alert_bots SET detail_level = $2, updated_at = now() WHERE org_id = $1
RETURNING `+botViewCols, orgID, string(level)))
	if errors.Is(err, pgx.ErrNoRows) {
		return BotView{}, ErrBotNotFound
	}
	return v, err
}

// DeleteBot removes the organisation's bot; deleting none is not an error.
func (s *Service) DeleteBot(ctx context.Context, orgID int64) error {
	if s.mode != ModeTenant {
		return ErrWrongMode
	}
	_, err := s.db.Exec(ctx, `DELETE FROM alert_bots WHERE org_id = $1`, orgID)
	return err
}

// workingBot decrypts the organisation's active bot. It returns nil, nil when
// not in tenant mode (a leftover row is never used by the shared bot), when
// there is no bot, it is invalid, there is no sealer, or the ciphertext does
// not open (ErrSealOpen: a rotated key or a row copied from another
// organisation). A database failure or any other sealer failure is an error.
// A decrypt failure is logged, not written back.
func (s *Service) workingBot(ctx context.Context, orgID int64) (*orgBot, error) {
	if s.mode != ModeTenant {
		return nil, nil
	}
	if s.sealer == nil {
		return nil, nil
	}
	var blob []byte
	var keyID, username, status, level string
	err := s.db.QueryRow(ctx, `
SELECT token_ciphertext, key_id, bot_username, status, detail_level FROM alert_bots WHERE org_id = $1`,
		orgID).Scan(&blob, &keyID, &username, &status, &level)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, nil
	}
	pt, err := s.sealer.Open(orgID, keyID, blob)
	if errors.Is(err, ErrSealOpen) {
		slog.Error("alerts: cannot decrypt org bot token", "org_id", orgID, "error_class", errClass(err))
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("alerts: open org bot token: %w", err)
	}
	tg := NewTelegram(string(pt), s.apiBase, nil)
	for i := range pt {
		pt[i] = 0
	}
	return &orgBot{tg: tg, level: levelFromDB(level), username: username, sealed: blob}, nil
}
