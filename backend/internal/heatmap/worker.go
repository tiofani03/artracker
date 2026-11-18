package heatmap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pendingBatch = 20

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
}

func NewWorker(pool *pgxpool.Pool, store ObjectGetter, log *slog.Logger) *Worker {
	return &Worker{pool: pool, store: store, log: log, idle: IdleBeforeWorker, tick: WorkerTick, batch: pendingBatch}
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

// ProcessPending processes up to batch idle sessions that are unprocessed or have new chunks, oldest first.
// A failing session is logged and skipped. It returns how many sessions were processed successfully.
func (w *Worker) ProcessPending(ctx context.Context) (int, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT project_id, id FROM replay_sessions
		WHERE updated_at < now() - make_interval(secs => $1)
		  AND (taps_processed_at IS NULL OR chunk_count > taps_processed_chunks)
		ORDER BY updated_at ASC
		LIMIT $2`, w.idle.Seconds(), w.batch)
	if err != nil {
		return 0, fmt.Errorf("list pending sessions: %w", err)
	}
	var pending []pendingSession
	for rows.Next() {
		var p pendingSession
		if err := rows.Scan(&p.projectID, &p.sessionID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan pending session: %w", err)
		}
		pending = append(pending, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("list pending sessions: %w", err)
	}

	done := 0
	for _, p := range pending {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		if err := w.ProcessSession(ctx, p.projectID, p.sessionID); err != nil {
			w.log.Error("heatmap: process session", "project_id", p.projectID, "session_id", p.sessionID, "err", err)
			continue
		}
		done++
	}
	return done, nil
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
	var excluded []string
	if err := w.pool.QueryRow(ctx,
		`SELECT heatmap_excluded_screens FROM projects WHERE id=$1`, projectID).Scan(&excluded); err != nil {
		return fmt.Errorf("read excluded screens: %w", err)
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
	res := Extract(chunks, SkipFunc(excluded))

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1 || ':' || $2))`, fmt.Sprint(projectID), sessionID); err != nil {
		return fmt.Errorf("lock session: %w", err)
	}
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
	batch.Queue(`UPDATE replay_sessions SET taps_processed_at = now(), taps_processed_chunks = $3, taps_unrecognized = $4
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

// loadChunks reads and decodes every chunk of the session; unreadable or non-screen chunks are skipped.
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
	for _, r := range refs {
		raw, err := w.readObject(ctx, r.key)
		if err != nil {
			w.log.Warn("heatmap: skip chunk", "project_id", projectID, "session_id", sessionID, "seq", r.seq, "err", err)
			continue
		}
		c, ok, err := DecodeChunk(r.seq, raw)
		if err != nil {
			w.log.Warn("heatmap: skip chunk", "project_id", projectID, "session_id", sessionID, "seq", r.seq, "err", err)
			continue
		}
		if ok {
			chunks = append(chunks, c)
		}
	}
	return chunks, nil
}

func (w *Worker) readObject(ctx context.Context, key string) ([]byte, error) {
	rc, _, _, err := w.store.GetObject(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
