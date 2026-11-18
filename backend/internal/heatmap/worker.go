package heatmap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/storage"
)

const (
	pendingBatch = 20
	// tickBudget bounds how long one tick keeps draining batches.
	tickBudget = 45 * time.Second
	// maxAttempts failures mark a session processed so it cannot block the queue forever.
	maxAttempts = 8
	// objectTimeout bounds one chunk download plus read.
	objectTimeout = 60 * time.Second
	retryBase     = time.Minute
	retryCap      = 6 * time.Hour
)

// ObjectGetter is the slice of storage.ReplayStore the worker needs.
type ObjectGetter interface {
	GetObject(ctx context.Context, key string) (io.ReadCloser, int64, string, error)
}

// Worker extracts taps from idle replay sessions and stores them.
type Worker struct {
	pool  *pgxpool.Pool
	store ObjectGetter
	log   *slog.Logger
	idle  time.Duration
	tick  time.Duration
	batch int
	// budget is the per-tick time budget for draining batches.
	budget time.Duration
}

func NewWorker(pool *pgxpool.Pool, store ObjectGetter, log *slog.Logger) *Worker {
	return &Worker{pool: pool, store: store, log: log, idle: IdleBeforeWorker, tick: WorkerTick, batch: pendingBatch, budget: tickBudget}
}

// Run processes pending sessions every tick until ctx is done. Errors are logged, never fatal.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := w.ProcessPending(ctx); err != nil && ctx.Err() == nil {
				w.log.Error("heatmap: process pending", "err", err)
			}
		}
	}
}

type pendingSession struct {
	projectID int64
	sessionID string
}

// ProcessPending drains idle sessions that are unprocessed or have new chunks, newest first, in batches
// until no work is left or the tick budget is spent. Web projects are skipped (they have no screen
// chunks). A failing session is logged and backed off (see recordFailure) so it cannot block the queue.
// It returns how many sessions were processed successfully.
func (w *Worker) ProcessPending(ctx context.Context) (int, error) {
	deadline := time.Now().Add(w.budget)
	done := 0
	for {
		pending, err := w.listPending(ctx)
		if err != nil {
			return done, err
		}
		for _, p := range pending {
			if ctx.Err() != nil {
				return done, ctx.Err()
			}
			if err := w.ProcessSession(ctx, p.projectID, p.sessionID); err != nil {
				if ctx.Err() != nil {
					return done, ctx.Err()
				}
				w.recordFailure(ctx, p, err)
				continue
			}
			done++
		}
		// every handled session leaves the pending set (processed or backed off), so a short batch
		// means the queue is drained
		if len(pending) < w.batch || !time.Now().Before(deadline) {
			return done, nil
		}
	}
}

func (w *Worker) listPending(ctx context.Context) ([]pendingSession, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT s.project_id, s.id FROM replay_sessions s
		JOIN projects p ON p.id = s.project_id
		WHERE s.updated_at < now() - make_interval(secs => $1)
		  AND (s.taps_processed_at IS NULL OR s.chunk_count > s.taps_processed_chunks)
		  AND (s.taps_retry_at IS NULL OR s.taps_retry_at <= now())
		  AND p.platform <> 'web'
		ORDER BY s.updated_at DESC
		LIMIT $2`, w.idle.Seconds(), w.batch)
	if err != nil {
		return nil, fmt.Errorf("list pending sessions: %w", err)
	}
	defer rows.Close()
	var pending []pendingSession
	for rows.Next() {
		var p pendingSession
		if err := rows.Scan(&p.projectID, &p.sessionID); err != nil {
			return nil, fmt.Errorf("scan pending session: %w", err)
		}
		pending = append(pending, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending sessions: %w", err)
	}
	return pending, nil
}

// recordFailure counts a failed attempt and schedules the retry after retryBase*2^attempts (capped at
// retryCap). After maxAttempts the session is marked processed so it stops being picked.
func (w *Worker) recordFailure(ctx context.Context, p pendingSession, cause error) {
	var attempts int
	err := w.pool.QueryRow(ctx, `
		UPDATE replay_sessions SET
			taps_attempts = taps_attempts + 1,
			taps_retry_at = now() + LEAST(make_interval(secs => $3), make_interval(secs => $4 * power(2, taps_attempts)))
		WHERE project_id=$1 AND id=$2
		RETURNING taps_attempts`, p.projectID, p.sessionID, retryCap.Seconds(), retryBase.Seconds()).Scan(&attempts)
	if err != nil {
		w.log.Error("heatmap: record failure", "project_id", p.projectID, "session_id", p.sessionID, "err", err)
		return
	}
	if attempts >= maxAttempts {
		if _, err := w.pool.Exec(ctx, `
			UPDATE replay_sessions SET taps_processed_at = now(), taps_processed_chunks = chunk_count
			WHERE project_id=$1 AND id=$2`, p.projectID, p.sessionID); err != nil {
			w.log.Error("heatmap: give up on session", "project_id", p.projectID, "session_id", p.sessionID, "err", err)
			return
		}
		w.log.Error("heatmap: giving up on session after repeated failures",
			"project_id", p.projectID, "session_id", p.sessionID, "attempts", attempts, "err", cause)
		return
	}
	w.log.Error("heatmap: process session", "project_id", p.projectID, "session_id", p.sessionID, "attempts", attempts, "err", cause)
}

// ProcessSession reads all chunks of one session and replaces its rows in one transaction.
func (w *Worker) ProcessSession(ctx context.Context, projectID int64, sessionID string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic processing session: %v", r)
		}
	}()
	var chunkCount int
	if err := w.pool.QueryRow(ctx,
		`SELECT chunk_count FROM replay_sessions WHERE project_id=$1 AND id=$2`, projectID, sessionID).Scan(&chunkCount); err != nil {
		return fmt.Errorf("read session: %w", err)
	}
	var appVersion string
	if err := w.pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT release_version FROM telemetry_events
			WHERE project_id=$1 AND session_id=$2 AND release_version <> ''
			GROUP BY release_version ORDER BY count(*) DESC, release_version LIMIT 1), '')`,
		projectID, sessionID).Scan(&appVersion); err != nil {
		return fmt.Errorf("read app version: %w", err)
	}

	chunks, err := w.loadChunks(ctx, projectID, sessionID)
	if err != nil {
		return err
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1 || ':' || $2))`, fmt.Sprint(projectID), sessionID); err != nil {
		return fmt.Errorf("lock session: %w", err)
	}
	// Read the exclusion list inside the tx, after the lock and before any write: a concurrent settings
	// PATCH either committed already (we see it) or waits on our share lock. projects is locked first,
	// like the PATCH does, to avoid a deadlock.
	var excluded []string
	if err := tx.QueryRow(ctx, `SELECT heatmap_excluded_screens FROM projects WHERE id=$1 FOR SHARE`, projectID).Scan(&excluded); err != nil {
		return fmt.Errorf("read excluded screens: %w", err)
	}
	res := Extract(chunks, SkipFunc(excluded))
	if _, err := tx.Exec(ctx, `DELETE FROM screen_taps WHERE project_id=$1 AND session_id=$2`, projectID, sessionID); err != nil {
		return fmt.Errorf("delete taps: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM screen_visits WHERE project_id=$1 AND session_id=$2`, projectID, sessionID); err != nil {
		return fmt.Errorf("delete visits: %w", err)
	}

	batch := &pgx.Batch{}
	for _, t := range res.Taps {
		batch.Queue(`INSERT INTO screen_taps(project_id, session_id, screen, ts, x_norm, y_norm, vw, vh, aspect_bucket, platform, app_version, landscape, rage)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			projectID, sessionID, t.Screen, time.UnixMilli(t.TS), t.XNorm, t.YNorm, t.VW, t.VH, t.Aspect,
			res.Platform, appVersion, t.Landscape, t.Rage)
	}
	for _, v := range res.Visits {
		batch.Queue(`INSERT INTO screen_visits(project_id, session_id, screen, ts, platform, app_version, landscape, aspect_bucket)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			projectID, sessionID, v.Screen, time.UnixMilli(v.TS), res.Platform, appVersion, v.Landscape, v.Aspect)
	}
	for _, b := range res.Backgrounds {
		f := b.Frame
		batch.Queue(`INSERT INTO screen_backgrounds(project_id, screen, platform, app_version, landscape, aspect_bucket, session_id, chunk_seq, frame_index, vw, vh, masked, captured_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (project_id, screen, platform, app_version, landscape, aspect_bucket) DO UPDATE SET
				session_id=EXCLUDED.session_id, chunk_seq=EXCLUDED.chunk_seq, frame_index=EXCLUDED.frame_index,
				vw=EXCLUDED.vw, vh=EXCLUDED.vh, masked=EXCLUDED.masked, captured_at=EXCLUDED.captured_at
			WHERE EXCLUDED.masked < screen_backgrounds.masked
			   OR (EXCLUDED.masked = screen_backgrounds.masked AND EXCLUDED.captured_at > screen_backgrounds.captured_at)`,
			projectID, b.Screen, res.Platform, appVersion, b.Landscape, b.Aspect, sessionID, f.ChunkSeq, f.Index,
			f.VW, f.VH, f.Masked, time.UnixMilli(f.TS))
	}
	batch.Queue(`UPDATE replay_sessions SET taps_processed_at = now(), taps_processed_chunks = $3, taps_unrecognized = $4,
			taps_attempts = 0, taps_retry_at = NULL
		WHERE project_id=$1 AND id=$2`, projectID, sessionID, chunkCount, res.Unrecognized)

	br := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("write heatmap rows: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("write heatmap rows: %w", err)
	}
	return tx.Commit(ctx)
}

// loadChunks reads and decodes every chunk of the session. Missing objects and undecodable chunks are
// skipped; any other storage error fails the session. If the first readable chunk is not a screen
// chunk (a web rrweb replay) the rest is not read at all.
func (w *Worker) loadChunks(ctx context.Context, projectID int64, sessionID string) ([]Chunk, error) {
	rows, err := w.pool.Query(ctx,
		`SELECT chunk_seq, object_key FROM replay_chunks WHERE project_id=$1 AND session_id=$2 ORDER BY chunk_seq`,
		projectID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	type ref struct {
		seq int
		key string
	}
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.seq, &r.key); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		refs = append(refs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}

	var chunks []Chunk
	readable := false
	for _, r := range refs {
		raw, err := w.readObject(ctx, r.key)
		if errors.Is(err, storage.ErrNotFound) {
			w.log.Warn("heatmap: skip missing chunk", "project_id", projectID, "session_id", sessionID, "seq", r.seq)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read chunk %d: %w", r.seq, err)
		}
		c, ok, err := DecodeChunk(r.seq, raw)
		if err != nil {
			w.log.Warn("heatmap: skip chunk", "project_id", projectID, "session_id", sessionID, "seq", r.seq, "err", err)
			continue
		}
		if !ok {
			if !readable {
				return nil, nil // not a screen replay: processed with 0 rows
			}
			continue
		}
		readable = true
		chunks = append(chunks, c)
	}
	return chunks, nil
}

func (w *Worker) readObject(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, objectTimeout)
	defer cancel()
	rc, _, _, err := w.store.GetObject(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
