package alerts

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service owns incident creation and delivery wiring. n may be nil, meaning
// Telegram is off: incidents are still recorded and shown in the dashboard.
type Service struct {
	db        *pgxpool.Pool
	n         Notifier
	publicURL string
	// botUsername is set at boot or later by the getMe retry loop, while the
	// workers and HTTP handlers read it, hence the atomic.
	botUsername atomic.Pointer[string]

	// Bot mode wiring, set once at boot through the With* setters (bot.go).
	mode    BotMode
	sealer  Sealer
	apiBase string
}

// NewService builds a service in ModeShared with the public Bot API host.
func NewService(db *pgxpool.Pool, n Notifier, publicURL string) *Service {
	return &Service{db: db, n: n, publicURL: strings.TrimRight(publicURL, "/"),
		mode: ModeShared, apiBase: DefaultTelegramAPIBase}
}

// SetBotUsername records the bot's username; safe for concurrent use.
func (s *Service) SetBotUsername(u string) { s.botUsername.Store(&u) }

// WithBotUsername records the bot's username and returns the receiver.
func (s *Service) WithBotUsername(u string) *Service {
	s.SetBotUsername(u)
	return s
}

func (s *Service) BotUsername() string {
	if u := s.botUsername.Load(); u != nil {
		return *u
	}
	return ""
}

// TelegramConfigured reports whether link codes and polling can work: a
// notifier exists and the bot's username is known.
func (s *Service) TelegramConfigured() bool { return s.n != nil && s.BotUsername() != "" }

// OnIssue opens a firing incident when an enabled rule of the signal's kind
// matches the environment. A duplicate for an issue that already has a firing
// incident of that kind is absorbed by the partial unique index.
func (s *Service) OnIssue(ctx context.Context, sig IssueSignal) error {
	_, err := s.db.Exec(ctx, `
INSERT INTO alert_incidents(project_id, rule_kind, issue_id, title, environment, release_version, session_id)
SELECT project_id, kind, $3, $4, $5, $6, $7
FROM alert_rules
WHERE project_id = $1 AND kind = $2 AND enabled AND (environment = '' OR environment = $5)
ON CONFLICT (project_id, rule_kind, issue_id) WHERE status = 'firing' DO NOTHING`,
		sig.ProjectID, string(sig.Kind), sig.IssueID, sig.Title, sig.Environment, sig.ReleaseVersion, sig.SessionID)
	return err
}
