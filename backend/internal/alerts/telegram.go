package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Notifier delivers an HTML-formatted message to a chat.
type Notifier interface {
	SendMessage(ctx context.Context, chat string, html string) error
}

// Telegram is a minimal Bot API client. The token never appears in errors.
type Telegram struct {
	token   string
	apiBase string
	hc      *http.Client
}

// NewTelegram builds a client. A nil hc gets a 40 s timeout, enough for the
// 30 s long poll of GetUpdates.
func NewTelegram(token, apiBase string, hc *http.Client) *Telegram {
	if hc == nil {
		hc = &http.Client{Timeout: 40 * time.Second}
	}
	return &Telegram{token: token, apiBase: strings.TrimRight(apiBase, "/"), hc: hc}
}

const defaultRetryAfterSeconds = 5

// retryAfterSeconds prefers the envelope value, then the Retry-After header,
// then a fixed default, so the result is always >= 1.
func retryAfterSeconds(param int, header string) int {
	if param > 0 {
		return param
	}
	if n, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && n > 0 {
		return n
	}
	return defaultRetryAfterSeconds
}

// RetryAfterError is a 429; retry after Seconds.
type RetryAfterError struct{ Seconds int }

func (e *RetryAfterError) Error() string {
	return fmt.Sprintf("telegram: rate limited, retry after %ds", e.Seconds)
}

// MigratedError means the group was upgraded to a supergroup with a new id.
type MigratedError struct{ NewChatID int64 }

func (e *MigratedError) Error() string {
	return fmt.Sprintf("telegram: chat migrated to %d", e.NewChatID)
}

var (
	// ErrChatForbidden is a 403: the bot was blocked or removed.
	ErrChatForbidden = errors.New("telegram: bot is forbidden in this chat")
	// ErrConflict is a 409: another poller is consuming updates.
	ErrConflict = errors.New("telegram: conflict, another getUpdates consumer is active")
	// ErrUnauthorized is a 401: the bot token was rejected or revoked.
	ErrUnauthorized = errors.New("telegram: bot token rejected")
)

// APIError is any other Bot API failure.
type APIError struct {
	Code        int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: api error %d: %s", e.Code, e.Description)
}

type Update struct {
	UpdateID     int64              `json:"update_id"`
	Message      *Message           `json:"message"`
	MyChatMember *ChatMemberUpdated `json:"my_chat_member"`
}

type Message struct {
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}

type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type ChatMemberUpdated struct {
	Chat          Chat `json:"chat"`
	NewChatMember struct {
		Status string `json:"status"`
	} `json:"new_chat_member"`
}

type envelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter      int   `json:"retry_after"`
		MigrateToChatID int64 `json:"migrate_to_chat_id"`
	} `json:"parameters"`
}

// SendMessage sends html to chat. A chat in canonical decimal form goes on
// the wire as a JSON number (as it always has); anything else, such as
// "@news", "007" or "+123", as a JSON string, so it is never reinterpreted.
func (t *Telegram) SendMessage(ctx context.Context, chat, html string) error {
	var chatID any = chat
	if n, err := strconv.ParseInt(chat, 10, 64); err == nil && strconv.FormatInt(n, 10) == chat {
		chatID = n
	}
	body, _ := json.Marshal(map[string]any{
		"chat_id":                  chatID,
		"text":                     html,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	_, err := t.call(ctx, http.MethodPost, "sendMessage", nil, body)
	return err
}

// BotInfo is the identity getMe reports for a token.
type BotInfo struct {
	ID       int64
	Username string
}

func (t *Telegram) GetBot(ctx context.Context) (BotInfo, error) {
	res, err := t.call(ctx, http.MethodGet, "getMe", nil, nil)
	if err != nil {
		return BotInfo{}, err
	}
	var me struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(res, &me); err != nil {
		return BotInfo{}, fmt.Errorf("telegram: decode getMe: %w", err)
	}
	return BotInfo{ID: me.ID, Username: me.Username}, nil
}

func (t *Telegram) GetMe(ctx context.Context) (string, error) {
	b, err := t.GetBot(ctx)
	if err != nil {
		return "", err
	}
	return b.Username, nil
}

func (t *Telegram) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	q.Set("timeout", strconv.Itoa(timeoutSec))
	q.Set("allowed_updates", `["message","my_chat_member"]`)
	res, err := t.call(ctx, http.MethodGet, "getUpdates", q, nil)
	if err != nil {
		return nil, err
	}
	var ups []Update
	if err := json.Unmarshal(res, &ups); err != nil {
		return nil, fmt.Errorf("telegram: decode getUpdates: %w", err)
	}
	return ups, nil
}

func (t *Telegram) call(ctx context.Context, httpMethod, method string, q url.Values, body []byte) (json.RawMessage, error) {
	u := t.apiBase + "/bot" + t.token + "/" + method
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, httpMethod, u, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("telegram: build %s request", method)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.hc.Do(req)
	if err != nil {
		// *url.Error embeds the full URL (and so the token); keep only the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("telegram: %s: %w", method, err)
	}
	defer resp.Body.Close()

	var env envelope
	decodeErr := json.NewDecoder(resp.Body).Decode(&env)
	if decodeErr == nil && env.OK && resp.StatusCode == http.StatusOK {
		return env.Result, nil
	}
	code := env.ErrorCode
	if code == 0 {
		code = resp.StatusCode
	}
	switch {
	case code == http.StatusTooManyRequests:
		return nil, &RetryAfterError{Seconds: retryAfterSeconds(env.Parameters.RetryAfter, resp.Header.Get("Retry-After"))}
	case code == http.StatusBadRequest && env.Parameters.MigrateToChatID != 0:
		return nil, &MigratedError{NewChatID: env.Parameters.MigrateToChatID}
	case code == http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case code == http.StatusForbidden:
		return nil, ErrChatForbidden
	case code == http.StatusConflict:
		return nil, ErrConflict
	}
	desc := env.Description
	if desc == "" {
		desc = http.StatusText(resp.StatusCode)
	}
	return nil, &APIError{Code: code, Description: desc}
}
