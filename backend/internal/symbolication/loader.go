package symbolication

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
)

// Querier is the slice of a pgx pool the loader needs.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Loader loads parsed symbolication artifacts (ProGuard/R8 mappings and dSYMs)
// from project_release_artifacts and keeps the most recently used ones in a
// bounded LRU cache. It is safe for concurrent use: the HTTP handlers and the
// app start profile worker share one instance.
//
// Entries are keyed by the artifact row id, so a newer upload (new id) is never
// served stale. The cheap id lookup runs on every call; the content fetch and
// the parse only run on a cache miss.
type Loader struct {
	db       Querier
	capacity int

	mu    sync.Mutex
	order *list.List              // front = most recently used; values are *loaderEntry
	items map[int64]*list.Element // artifact id -> element in order

	// test hooks
	parseHook func(kind string) // called before each parse
	closeHook func(id int64)    // called when an evicted *IOSSymbolicator is closed
}

type loaderEntry struct {
	id    int64
	value any // *ProguardRetracer or *IOSSymbolicator
}

// NewLoader returns a Loader holding at most capacity parsed artifacts
// (minimum 1). Production uses a capacity of 8.
func NewLoader(db Querier, capacity int) *Loader {
	if capacity < 1 {
		capacity = 1
	}
	return &Loader{
		db:       db,
		capacity: capacity,
		order:    list.New(),
		items:    make(map[int64]*list.Element),
	}
}

// Len reports the number of cached artifacts.
func (l *Loader) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}

// Proguard returns the parsed mapping for a release: the upload whose build
// number equals build when there is one, otherwise the newest upload for the
// version. It returns (nil, nil) when nothing was uploaded.
func (l *Loader) Proguard(ctx context.Context, projectID int64, version, build string) (*ProguardRetracer, error) {
	if version == "" {
		return nil, nil
	}
	v, err := l.load(ctx, "proguard", `
		SELECT id FROM project_release_artifacts
		WHERE project_id = $1 AND version = $2 AND artifact_type = 'proguard_mapping'
		ORDER BY ($3::text <> '' AND build_number = $3::text) DESC, created_at DESC, id DESC
		LIMIT 1`,
		func(content []byte) (any, error) { return ParseProguardMapping(content) },
		projectID, version, build)
	if err != nil || v == nil {
		return nil, err
	}
	return v.(*ProguardRetracer), nil
}

// IOSByVersion returns the symbolicator for the newest dSYM uploaded for a
// release version, or (nil, nil) when there is none.
func (l *Loader) IOSByVersion(ctx context.Context, projectID int64, version string) (*IOSSymbolicator, error) {
	if version == "" {
		return nil, nil
	}
	return l.ios(ctx, `
		SELECT id FROM project_release_artifacts
		WHERE project_id = $1 AND version = $2 AND artifact_type = 'dsym'
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		projectID, version)
}

// IOSByUUID returns the symbolicator for the newest dSYM whose Mach-O UUID
// matches uuid (case-insensitively), or (nil, nil) when there is none.
func (l *Loader) IOSByUUID(ctx context.Context, projectID int64, uuid string) (*IOSSymbolicator, error) {
	if uuid == "" {
		return nil, nil
	}
	return l.ios(ctx, `
		SELECT id FROM project_release_artifacts
		WHERE project_id = $1 AND artifact_type = 'dsym' AND upper(debug_uuid) = upper($2)
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		projectID, uuid)
}

func (l *Loader) ios(ctx context.Context, idQuery string, args ...any) (*IOSSymbolicator, error) {
	v, err := l.load(ctx, "ios", idQuery,
		func(content []byte) (any, error) { return NewIOSSymbolicator(content) },
		args...)
	if err != nil || v == nil {
		return nil, err
	}
	return v.(*IOSSymbolicator), nil
}

// load resolves idQuery to an artifact id, then returns the cached parse of that
// artifact or fetches and parses it. It returns (nil, nil) when no artifact (or
// an empty one) exists. No lock is held while querying or parsing.
func (l *Loader) load(ctx context.Context, kind, idQuery string, parse func([]byte) (any, error), args ...any) (any, error) {
	var id int64
	if err := l.db.QueryRow(ctx, idQuery, args...).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup %s artifact: %w", kind, err)
	}
	if v, ok := l.get(id); ok {
		return v, nil
	}

	var content []byte
	if err := l.db.QueryRow(ctx, `SELECT content FROM project_release_artifacts WHERE id = $1`, id).Scan(&content); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // deleted between the two queries
		}
		return nil, fmt.Errorf("fetch %s artifact %d: %w", kind, id, err)
	}
	if len(content) == 0 {
		return nil, nil
	}

	if l.parseHook != nil {
		l.parseHook(kind)
	}
	v, err := parse(content)
	if err != nil {
		return nil, fmt.Errorf("parse %s artifact %d: %w", kind, id, err)
	}
	return l.put(id, v), nil
}

// get returns the cached value for id and marks it most recently used.
func (l *Loader) get(id int64) (any, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.items[id]
	if !ok {
		return nil, false
	}
	l.order.MoveToFront(el)
	return el.Value.(*loaderEntry).value, true
}

// put caches v under id and returns the value to use. Concurrent callers may
// each parse the same artifact; the first stored result wins and later ones are
// discarded, so every caller ends up sharing one instance. Values evicted to
// stay within capacity are released.
func (l *Loader) put(id int64, v any) any {
	l.mu.Lock()
	if el, ok := l.items[id]; ok {
		l.order.MoveToFront(el)
		winner := el.Value.(*loaderEntry).value
		l.mu.Unlock()
		if winner != v {
			l.release(id, v) // our duplicate parse was never shared
		}
		return winner
	}
	l.items[id] = l.order.PushFront(&loaderEntry{id: id, value: v})
	var evicted []*loaderEntry
	for l.order.Len() > l.capacity {
		back := l.order.Back()
		e := l.order.Remove(back).(*loaderEntry)
		delete(l.items, e.id)
		evicted = append(evicted, e)
	}
	l.mu.Unlock()

	for _, e := range evicted {
		l.release(e.id, e.value)
	}
	return v
}

// release closes an iOS symbolicator that left the cache. A symbolicator
// reads from an in-memory byte slice (macho.NewFile over a bytes.Reader holds no
// closer), so Close releases nothing and cannot break a lookup still running in
// another goroutine; the memory is reclaimed by the GC once the last user drops
// it.
func (l *Loader) release(id int64, v any) {
	if s, ok := v.(*IOSSymbolicator); ok {
		_ = s.Close()
		if l.closeHook != nil {
			l.closeHook(id)
		}
	}
}
