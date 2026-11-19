package appstart

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/ingest"
	"github.com/tiofani03/artracker/backend/internal/symbolication"
)

const (
	workerTick   = 30 * time.Second
	pendingBatch = 20
	purgeEvery   = time.Hour

	// Retention is how long raw profiles and per-function stats are kept.
	Retention = 30 * 24 * time.Hour

	retryBase = 5 * time.Minute
	retryCap  = 6 * time.Hour
	// retryWindow is how long after receipt a profile keeps waiting for symbols.
	retryWindow = 24 * time.Hour

	maxFnNameRunes = 1024
)

// NextAttempt returns when to retry a profile that is still missing symbols after its attempts-th try:
// 5m * 2^(attempts-1), capped at 6h. It returns nil (give up) when that retry would fall after
// receivedAt + 24h (a retry landing exactly on that instant counts as past it).
func NextAttempt(attempts int, receivedAt, now time.Time) *time.Time {
	delay := retryCap
	if attempts < 1 {
		attempts = 1
	}
	if attempts <= 16 { // the guard only prevents shift overflow; the cap is hit long before
		if d := retryBase << (attempts - 1); d < retryCap {
			delay = d
		}
	}
	next := now.Add(delay)
	if !next.Before(receivedAt.Add(retryWindow)) {
		return nil
	}
	return &next
}

// Worker symbolicates stored app start profiles and writes their per-function stats.
type Worker struct {
	pool   *pgxpool.Pool
	loader *symbolication.Loader
	log    *slog.Logger
	tick   time.Duration
	batch  int
}

func NewWorker(pool *pgxpool.Pool, loader *symbolication.Loader, log *slog.Logger) *Worker {
	return &Worker{pool: pool, loader: loader, log: log, tick: workerTick, batch: pendingBatch}
}

// Run processes due profiles every tick and purges expired data at most once an hour, until ctx is done.
// Errors are logged, never fatal.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.tick)
	defer t.Stop()
	var lastPurge time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := w.ProcessPending(ctx); err != nil && ctx.Err() == nil {
				w.log.Error("appstart: process pending", "err", err)
			}
			if time.Since(lastPurge) >= purgeEvery {
				lastPurge = time.Now()
				if n, err := w.PurgeExpired(ctx); err != nil {
					if ctx.Err() == nil {
						w.log.Error("appstart: purge expired", "err", err)
					}
				} else if n > 0 {
					w.log.Info("appstart: purged expired rows", "rows", n)
				}
			}
		}
	}
}

// PurgeExpired deletes profiles received, and per-function stats started, more than Retention ago.
// It returns the number of rows deleted.
func (w *Worker) PurgeExpired(ctx context.Context) (int64, error) {
	cutoff := time.Now().Add(-Retention)
	var total int64
	for _, q := range []string{
		`DELETE FROM app_start_profiles WHERE received_at < $1`,
		`DELETE FROM app_start_fn_stats WHERE started_at < $1`,
	} {
		tag, err := w.pool.Exec(ctx, q, cutoff)
		if err != nil {
			return total, fmt.Errorf("purge: %w", err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

type pendingProfile struct {
	projectID  int64
	startID    string
	env        string
	appVersion string
	build      string
	raw        []byte
	attempts   int
	receivedAt time.Time
}

// ProcessPending handles one batch of due profiles (pending, or missing_symbols whose backoff elapsed),
// oldest first. A profile that errors or panics is recorded and skipped so it cannot block the queue or
// crash the API. It returns how many profiles were handled; the error is only for failing to list them.
func (w *Worker) ProcessPending(ctx context.Context) (int, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT project_id, start_id, environment, app_version, build, raw, attempts, received_at
		FROM app_start_profiles
		WHERE status IN ('pending', 'missing_symbols') AND next_attempt_at <= now()
		ORDER BY received_at
		LIMIT $1`, w.batch)
	if err != nil {
		return 0, fmt.Errorf("list pending profiles: %w", err)
	}
	var batch []pendingProfile
	for rows.Next() {
		var p pendingProfile
		if err := rows.Scan(&p.projectID, &p.startID, &p.env, &p.appVersion, &p.build, &p.raw, &p.attempts, &p.receivedAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan pending profile: %w", err)
		}
		batch = append(batch, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("list pending profiles: %w", err)
	}

	handled := 0
	for _, p := range batch {
		if ctx.Err() != nil {
			return handled, nil
		}
		w.handle(ctx, p)
		handled++
	}
	return handled, nil
}

// handle processes one profile, converting a panic into a failed profile.
func (w *Worker) handle(ctx context.Context, p pendingProfile) {
	defer func() {
		if r := recover(); r != nil {
			w.log.Error("appstart: profile panicked", "project_id", p.projectID, "start_id", p.startID, "panic", fmt.Sprint(r))
			w.markFailed(ctx, p)
		}
	}()
	err := w.process(ctx, p)
	if err == nil || ctx.Err() != nil {
		return
	}
	w.log.Error("appstart: process profile", "project_id", p.projectID, "start_id", p.startID, "err", err)
	w.recordError(ctx, p)
}

func (w *Worker) markFailed(ctx context.Context, p pendingProfile) {
	_, err := w.pool.Exec(ctx, `
		UPDATE app_start_profiles SET status = 'failed', next_attempt_at = NULL
		WHERE project_id = $1 AND start_id = $2`, p.projectID, p.startID)
	if err != nil && ctx.Err() == nil {
		w.log.Error("appstart: mark profile failed", "project_id", p.projectID, "start_id", p.startID, "err", err)
	}
}

// recordError backs off a profile whose processing hit an unexpected error, and fails it once the retry
// window is spent, so a persistent error cannot make the worker spin on it forever.
func (w *Worker) recordError(ctx context.Context, p pendingProfile) {
	attempts := p.attempts + 1
	next := NextAttempt(attempts, p.receivedAt, time.Now())
	if next == nil {
		w.markFailed(ctx, p)
		return
	}
	_, err := w.pool.Exec(ctx, `
		UPDATE app_start_profiles SET attempts = $3, next_attempt_at = $4
		WHERE project_id = $1 AND start_id = $2`, p.projectID, p.startID, attempts, *next)
	if err != nil && ctx.Err() == nil {
		w.log.Error("appstart: back off profile", "project_id", p.projectID, "start_id", p.startID, "err", err)
	}
}

func gunzipProfile(raw []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, ingest.MaxProfileUncompressedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(out) > ingest.MaxProfileUncompressedBytes {
		return nil, ingest.ErrPayloadTooLarge
	}
	return out, nil
}

func (w *Worker) process(ctx context.Context, p pendingProfile) error {
	rawJSON, err := gunzipProfile(p.raw)
	var upload ingest.AppStartProfileUpload
	if err == nil {
		upload, err = ingest.ParseAppStartProfile(rawJSON)
	}
	if err != nil {
		w.log.Warn("appstart: invalid stored profile", "project_id", p.projectID, "start_id", p.startID, "err", err)
		w.markFailed(ctx, p)
		return nil
	}
	prof := upload.Profile

	resolver := w.buildResolver(ctx, p, prof)
	sym := Symbolicate(prof, resolver)
	cleanFrames(sym.Frames)
	stats := ComputeFnStats(sym)
	symJSON, err := json.Marshal(sym)
	if err != nil {
		return fmt.Errorf("marshal symbolicated profile: %w", err)
	}

	status := "done"
	attempts := p.attempts
	var next *time.Time
	if resolver.MissingSymbols() {
		status = "missing_symbols"
		attempts++
		next = NextAttempt(attempts, p.receivedAt, time.Now())
	}

	return w.store(ctx, p, prof.Platform, stats, symJSON, status, attempts, next)
}

// buildResolver loads the artifacts the profile needs. A load failure is logged and treated like a missing
// artifact, so a corrupt mapping backs the profile off instead of retrying every tick.
func (w *Worker) buildResolver(ctx context.Context, p pendingProfile, prof ingest.AppStartProfile) Resolver {
	if prof.Platform == "android" {
		r, err := w.loader.Proguard(ctx, p.projectID, p.appVersion, p.build)
		if err != nil {
			w.log.Warn("appstart: load proguard mapping", "project_id", p.projectID, "version", p.appVersion, "err", err)
			r = nil
		}
		return NewAndroidResolver(r, prof.AppPackages)
	}

	syms := map[string]*symbolication.IOSSymbolicator{}
	for _, img := range prof.Images {
		if !img.App {
			continue
		}
		uuids := (ingest.AppStartProfile{Images: []ingest.ProfileImage{img}}).ImageUUIDs()
		if len(uuids) != 1 {
			continue
		}
		if _, done := syms[uuids[0]]; done {
			continue
		}
		s, err := w.loader.IOSByUUID(ctx, p.projectID, uuids[0])
		if err != nil {
			w.log.Warn("appstart: load dSYM", "project_id", p.projectID, "uuid", uuids[0], "err", err)
			s = nil
		}
		syms[uuids[0]] = s
	}
	return NewIOSResolver(prof.Images, syms)
}

// store replaces the profile's fn_stats and records its result in one transaction.
func (w *Worker) store(ctx context.Context, p pendingProfile, platform string, stats []FnStat, symJSON []byte, status string, attempts int, next *time.Time) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var startedAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT started_at FROM app_starts WHERE project_id = $1 AND start_id = $2), $3::timestamptz)`,
		p.projectID, p.startID, p.receivedAt).Scan(&startedAt); err != nil {
		return fmt.Errorf("resolve started_at: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM app_start_fn_stats WHERE project_id = $1 AND start_id = $2`, p.projectID, p.startID); err != nil {
		return fmt.Errorf("clear fn stats: %w", err)
	}
	seen := make(map[string]struct{}, len(stats))
	rows := make([][]any, 0, len(stats))
	for _, st := range stats {
		fn := ingest.CleanStoredString(st.Function, maxFnNameRunes)
		if _, dup := seen[fn]; dup {
			continue
		}
		seen[fn] = struct{}{}
		rows = append(rows, []any{p.projectID, p.startID, platform, p.env, p.appVersion, fn, st.IsApp, st.SelfMs, st.TotalMs, st.AttributedMs, startedAt})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"app_start_fn_stats"},
		[]string{"project_id", "start_id", "platform", "environment", "app_version", "function", "is_app", "self_ms", "total_ms", "attributed_ms", "started_at"},
		pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("insert fn stats: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE app_start_profiles
		SET symbolicated = $3, status = $4, attempts = $5, next_attempt_at = $6
		WHERE project_id = $1 AND start_id = $2`,
		p.projectID, p.startID, symJSON, status, attempts, next); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return tx.Commit(ctx)
}

// cleanFrames makes frame text safe for jsonb and text columns (no NUL bytes, valid UTF-8).
func cleanFrames(frames []ResolvedFrame) {
	for i := range frames {
		frames[i].Function = ingest.CleanStoredString(frames[i].Function, maxFnNameRunes)
		frames[i].File = ingest.CleanStoredString(frames[i].File, maxFnNameRunes)
	}
}
