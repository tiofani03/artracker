package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/alerts"
	"github.com/tiofani03/artracker/backend/internal/config"
)

// buildAlerts constructs the alerts service for the configured bot mode.
// ctx bounds the startup getMe call; retryCtx and wg own the background
// getMe retry goroutine. The returned src is a nil interface unless shared
// mode has a token (typed-nil guard: a nil *Telegram is never assigned to an
// interface). It returns an error only for a fatal construction problem.
func buildAlerts(ctx, retryCtx context.Context, wg *sync.WaitGroup, cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) (*alerts.Service, alerts.UpdateSource, error) {
	var n alerts.Notifier
	var src alerts.UpdateSource
	var tg *alerts.Telegram
	tenant := cfg.AlertsBotMode == string(alerts.ModeTenant)
	if !tenant && cfg.TelegramBotToken != "" {
		tg = alerts.NewTelegram(cfg.TelegramBotToken, cfg.TelegramAPIBase, nil)
		n, src = tg, tg
	}
	svc := alerts.NewService(pool, n, cfg.PublicURL).
		WithMode(alerts.BotMode(cfg.AlertsBotMode)).
		WithAPIBase(cfg.TelegramAPIBase)

	if tenant {
		if cfg.AlertsMasterKey == nil {
			log.Info("alerts master key not configured: organisation bot API disabled")
			return svc, src, nil
		}
		sealer, err := alerts.NewLocalSealer(cfg.AlertsMasterKey, cfg.AlertsMasterKeyID)
		if err != nil {
			return nil, nil, fmt.Errorf("alerts master key: %w", err)
		}
		return svc.WithSealer(sealer), src, nil
	}

	if tg != nil {
		meCtx, meCancel := context.WithTimeout(ctx, 10*time.Second)
		name, err := tg.GetMe(meCtx)
		meCancel()
		if err != nil {
			log.Warn("telegram getMe failed; retrying in the background", "error_class", fmt.Sprintf("%T", err))
			wg.Add(1)
			go func() {
				defer wg.Done()
				svc.RetryBotUsername(retryCtx, tg.GetMe)
			}()
		} else {
			svc.SetBotUsername(name)
			log.Info("telegram alerts configured", "bot", name)
		}
	}
	return svc, src, nil
}
