package alerts

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service owns incident creation and delivery wiring. n may be nil, meaning
// Telegram is off: incidents are still recorded and shown in the dashboard.
type Service struct {
	db          *pgxpool.Pool
	n           Notifier
	publicURL   string
	botUsername string
}

func NewService(db *pgxpool.Pool, n Notifier, publicURL string) *Service {
	return &Service{db: db, n: n, publicURL: strings.TrimRight(publicURL, "/")}
}

// WithBotUsername records the bot's username and returns the receiver.
func (s *Service) WithBotUsername(u string) *Service {
	s.botUsername = u
	return s
}

func (s *Service) BotUsername() string { return s.botUsername }

func (s *Service) TelegramConfigured() bool { return s.n != nil && s.botUsername != "" }

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
