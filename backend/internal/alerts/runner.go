package alerts

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tick intervals; variables so tests can shorten them.
var (
	dispatchInterval = 5 * time.Second
	evaluateInterval = 60 * time.Second
)

// ErrChannelNotFound means the project has no channel with the given id.
var ErrChannelNotFound = errors.New("channel not found")

// Run drives the alert workers until ctx is done: it dispatches pending
// notifications, evaluates spike rules and, when Telegram is configured and
// src is non-nil, long-polls for updates. It returns only after every
// goroutine has exited. A tick failure is logged and never fatal.
func (s *Service) Run(ctx context.Context, src UpdateSource) {
	var wg sync.WaitGroup
	loop := func(name string, every time.Duration, tick func(context.Context, time.Time) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					safeTick(ctx, name, tick)
				}
			}
		}()
	}
	loop("dispatch", dispatchInterval, s.DispatchPending)
	loop("evaluate", evaluateInterval, s.Evaluate)
	if src != nil && s.TelegramConfigured() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.PollUpdates(ctx, src)
		}()
	}
	wg.Wait()
}

// safeTick runs one tick, converting a panic into a log line so a bug in one
// tick can never take down the process.
func safeTick(ctx context.Context, name string, tick func(context.Context, time.Time) error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("alerts: tick panicked", "tick", name, "panic_class", panicClass(r))
		}
	}()
	if err := tick(ctx, time.Now()); err != nil && ctx.Err() == nil {
		slog.Error("alerts: tick failed", "tick", name, "error_class", errClass(err))
	}
}

func panicClass(r any) string {
	if err, ok := r.(error); ok {
		return errClass(err)
	}
	return "non_error_panic"
}

// SendTest sends the test message to one channel of the project. A disabled
// channel may still be tested. Bookkeeping matches regular delivery.
func (s *Service) SendTest(ctx context.Context, projectID, channelID int64) error {
	if !s.TelegramConfigured() {
		return ErrTelegramDisabled
	}
	var chatID, orgID int64
	var name string
	err := s.db.QueryRow(ctx, `
SELECT c.telegram_chat_id, p.org_id, p.name
FROM alert_channels c JOIN projects p ON p.id = c.project_id
WHERE c.id = $1 AND c.project_id = $2`, channelID, projectID).Scan(&chatID, &orgID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChannelNotFound
	}
	if err != nil {
		return err
	}
	mc := MessageContext{ProjectName: name, OrgID: orgID, ProjectID: projectID, PublicURL: s.publicURL}
	sendErr := s.n.SendMessage(ctx, chatID, FormatTest(mc))
	switch {
	case sendErr == nil:
		return s.markChannelSent(ctx, channelID, time.Now())
	case ctx.Err() != nil:
		return sendErr
	case errors.Is(sendErr, ErrChatForbidden):
		if err := s.disableChannelForbidden(ctx, channelID); err != nil {
			return err
		}
	}
	if err := s.markChannelFailed(ctx, channelID, sendErr); err != nil {
		slog.Error("alerts: record test failure", "channel_id", channelID, "error_class", errClass(err))
	}
	return sendErr
}
