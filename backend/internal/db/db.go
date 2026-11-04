package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect waits for Postgres: in docker compose the API can start before the
// database is ready to accept connections.
func Connect(ctx context.Context, dsn string, maxConns ...int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10
	if len(maxConns) > 0 && maxConns[0] > 0 {
		cfg.MaxConns = int32(maxConns[0])
	}
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = 5 * time.Minute

	var lastErr error
	for attempt := 1; attempt <= 20; attempt++ {
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		slog.Warn("database not ready, retrying", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("database unreachable after retries: %w", lastErr)
}
