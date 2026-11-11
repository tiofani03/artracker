package alerts

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrTelegramDisabled is returned when no bot is configured.
var ErrTelegramDisabled = errors.New("telegram is not configured")

// LinkCode is a short-lived, single-use secret that binds a chat to a project.
type LinkCode struct {
	Code        string    `json:"code"`
	GroupLink   string    `json:"group_link"`
	PrivateLink string    `json:"private_link"`
	ExpiresAt   time.Time `json:"expires_at"`
}

const (
	linkAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	linkCodeLen  = 16
	// Bytes at or above this are rejected so b % 36 is unbiased (252 = 7*36).
	linkByteLimit = 252

	replyExpired = "⚠️ This link has expired or was already used. Generate a new one in Artracker → Alerts."
	removedMsg   = "bot was removed from the chat"
)

var startRe = regexp.MustCompile(`^/start(@\w+)?\s+([a-z0-9]{16})$`)

// Poll back-off durations; variables so tests can shorten them.
var (
	pollConflictSleep = 30 * time.Second
	pollErrorSleep    = 5 * time.Second
)

func generateLinkCode() (string, error) {
	out := make([]byte, 0, linkCodeLen)
	buf := make([]byte, 32)
	for len(out) < linkCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("alerts: generate link code: %w", err)
		}
		for _, b := range buf {
			if b >= linkByteLimit {
				continue
			}
			out = append(out, linkAlphabet[int(b)%len(linkAlphabet)])
			if len(out) == linkCodeLen {
				break
			}
		}
	}
	return string(out), nil
}

// CreateLinkCode issues a code valid for 15 minutes. A non-positive userID is
// stored as NULL.
func (s *Service) CreateLinkCode(ctx context.Context, projectID, userID int64) (LinkCode, error) {
	if !s.TelegramConfigured() {
		return LinkCode{}, ErrTelegramDisabled
	}
	code, err := generateLinkCode()
	if err != nil {
		return LinkCode{}, err
	}
	var exp time.Time
	err = s.db.QueryRow(ctx, `
INSERT INTO alert_link_codes(code, project_id, created_by, expires_at)
VALUES ($1, $2, NULLIF($3::bigint, 0), now() + interval '15 minutes')
RETURNING expires_at`, code, projectID, userID).Scan(&exp)
	if err != nil {
		return LinkCode{}, fmt.Errorf("alerts: insert link code: %w", err)
	}
	return LinkCode{
		Code:        code,
		GroupLink:   "https://t.me/" + s.botUsername + "?startgroup=" + code,
		PrivateLink: "https://t.me/" + s.botUsername + "?start=" + code,
		ExpiresAt:   exp,
	}, nil
}

// HandleUpdate processes one Telegram update. It never logs message text or
// codes, which are secrets.
func (s *Service) HandleUpdate(ctx context.Context, u Update) error {
	switch {
	case u.Message != nil:
		m := startRe.FindStringSubmatch(strings.TrimSpace(u.Message.Text))
		if m == nil {
			return nil
		}
		return s.bindChat(ctx, u.Message.Chat, m[2])
	case u.MyChatMember != nil:
		st := u.MyChatMember.NewChatMember.Status
		if st != "left" && st != "kicked" {
			return nil
		}
		_, err := s.db.Exec(ctx,
			`UPDATE alert_channels SET enabled = false, disabled_reason = $2 WHERE telegram_chat_id = $1`,
			u.MyChatMember.Chat.ID, removedMsg)
		if err != nil {
			return fmt.Errorf("alerts: disable removed chat: %w", err)
		}
	}
	return nil
}

func chatTitle(c Chat) string {
	switch {
	case c.Title != "":
		return c.Title
	case c.Username != "":
		return "@" + c.Username
	case c.FirstName != "":
		return c.FirstName
	}
	return "Telegram chat"
}

// bindChat claims the code and upserts the channel in one transaction, so a
// failed bind does not burn the code.
func (s *Service) bindChat(ctx context.Context, chat Chat, code string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("alerts: begin link tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var projectID int64
	err = tx.QueryRow(ctx, `
UPDATE alert_link_codes SET used_at = now()
WHERE code = $1 AND used_at IS NULL AND expires_at > now()
RETURNING project_id`, code).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		s.reply(ctx, chat.ID, replyExpired)
		return nil
	}
	if err != nil {
		return fmt.Errorf("alerts: claim link code: %w", err)
	}

	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM projects WHERE id = $1`, projectID).Scan(&name); err != nil {
		return fmt.Errorf("alerts: load project: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO alert_channels(project_id, telegram_chat_id, title, chat_type)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, telegram_chat_id)
DO UPDATE SET title = EXCLUDED.title, chat_type = EXCLUDED.chat_type, enabled = true, disabled_reason = ''`,
		projectID, chat.ID, chatTitle(chat), chat.Type)
	if err != nil {
		return fmt.Errorf("alerts: upsert channel: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("alerts: commit link: %w", err)
	}
	s.reply(ctx, chat.ID, "✅ Connected to <b>"+html.EscapeString(name)+"</b>. Alerts for this project will arrive here.")
	return nil
}

// reply is best effort: a failure is logged by id and never undoes a binding.
func (s *Service) reply(ctx context.Context, chatID int64, text string) {
	if s.n == nil {
		return
	}
	if err := s.n.SendMessage(ctx, chatID, text); err != nil {
		slog.Warn("alerts: link reply failed", "chat_id", chatID, "error_class", fmt.Sprintf("%T", err))
	}
}

type UpdateSource interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error)
}

// PollUpdates long-polls src until ctx is done. The offset advances past every
// returned update, even one whose handling failed, so a poison update is never
// re-fetched forever.
func (s *Service) PollUpdates(ctx context.Context, src UpdateSource) {
	var offset int64
	for ctx.Err() == nil {
		ups, err := src.GetUpdates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			d := pollErrorSleep
			if errors.Is(err, ErrConflict) {
				d = pollConflictSleep
			}
			slog.Warn("alerts: getUpdates failed", "error_class", fmt.Sprintf("%T", err), "retry_in", d.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
			continue
		}
		for _, u := range ups {
			if err := s.HandleUpdate(ctx, u); err != nil {
				slog.Error("alerts: handle update", "update_id", u.UpdateID, "error_class", fmt.Sprintf("%T", err))
			}
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
		}
	}
}
