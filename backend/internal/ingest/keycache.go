package ingest

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

var (
	ErrInvalidKey = errors.New("invalid api key")
)

// KeyInfo holds the resolved project ID and key type.
type KeyInfo struct {
	ProjectID int64
	KeyType   auth.KeyType
}

// KeyResolver provides fast API key authentication.
type KeyResolver interface {
	Resolve(ctx context.Context, plainKey string) (KeyInfo, error)
	Invalidate(keyHash string)
	InvalidateAll()
}

type cacheEntry struct {
	keyHash   string
	info      KeyInfo
	expiresAt time.Time
	isInvalid bool // negative cache marker
}

// MemoryKeyCache is an in-memory LRU cache with negative caching and singleflight dedup.
type MemoryKeyCache struct {
	db          *pgxpool.Pool
	mu          sync.Mutex
	maxEntries  int
	positiveTTL time.Duration
	negativeTTL time.Duration
	entries     map[string]*list.Element
	lru         *list.List
	group       singleflight.Group
}

// NewMemoryKeyCache creates a new in-memory API key cache.
func NewMemoryKeyCache(pool *pgxpool.Pool, positiveTTL time.Duration) *MemoryKeyCache {
	if positiveTTL <= 0 {
		positiveTTL = 60 * time.Second
	}
	return &MemoryKeyCache{
		db:          pool,
		maxEntries:  10000,
		positiveTTL: positiveTTL,
		negativeTTL: 10 * time.Second,
		entries:     make(map[string]*list.Element),
		lru:         list.New(),
	}
}

// Resolve validates the API key.
// 1. Fast prefix check (zero alloc).
// 2. Cache hit (positive or negative).
// 3. Cache miss: queries DB protected by singleflight, then populates LRU.
func (c *MemoryKeyCache) Resolve(ctx context.Context, plainKey string) (KeyInfo, error) {
	if plainKey == "" {
		return KeyInfo{}, ErrInvalidKey
	}
	kt, ok := auth.KeyTypeOf(plainKey)
	if !ok || len(plainKey) < 32 || len(plainKey) > 128 {
		return KeyInfo{}, ErrInvalidKey
	}

	keyHash := auth.HashKey(plainKey)
	now := time.Now()

	// 1. In-memory LRU check
	c.mu.Lock()
	if elem, found := c.entries[keyHash]; found {
		entry := elem.Value.(*cacheEntry)
		if now.Before(entry.expiresAt) {
			c.lru.MoveToFront(elem)
			if entry.isInvalid {
				c.mu.Unlock()
				return KeyInfo{}, ErrInvalidKey
			}
			info := entry.info
			c.mu.Unlock()
			return info, nil
		}
		// Expired: remove
		c.lru.Remove(elem)
		delete(c.entries, keyHash)
	}
	c.mu.Unlock()

	// If no DB pool is configured (e.g. SKIP_DB)
	if c.db == nil {
		return KeyInfo{}, ErrInvalidKey
	}

	// 2. Singleflight DB fetch
	res, err, _ := c.group.Do(keyHash, func() (any, error) {
		dbCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		var pid int64
		var keyTypeStr string
		qErr := c.db.QueryRow(dbCtx,
			`SELECT project_id, key_type FROM api_keys WHERE key_hash = $1 AND is_active`,
			keyHash).Scan(&pid, &keyTypeStr)

		if errors.Is(qErr, pgx.ErrNoRows) {
			// Negative cache
			c.setCache(keyHash, KeyInfo{}, true, c.negativeTTL)
			return nil, ErrInvalidKey
		}
		if qErr != nil {
			return nil, qErr
		}

		info := KeyInfo{
			ProjectID: pid,
			KeyType:   auth.KeyType(keyTypeStr),
		}
		c.setCache(keyHash, info, false, c.positiveTTL)
		return info, nil
	})

	if err != nil {
		return KeyInfo{}, err
	}
	info, ok := res.(KeyInfo)
	if !ok {
		return KeyInfo{}, ErrInvalidKey
	}
	// Sanity check key type matches prefix
	if info.KeyType != kt {
		return KeyInfo{}, ErrInvalidKey
	}
	return info, nil
}

func (c *MemoryKeyCache) setCache(keyHash string, info KeyInfo, isInvalid bool, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if elem, found := c.entries[keyHash]; found {
		entry := elem.Value.(*cacheEntry)
		entry.info = info
		entry.isInvalid = isInvalid
		entry.expiresAt = now.Add(ttl)
		c.lru.MoveToFront(elem)
		return
	}

	for c.lru.Len() >= c.maxEntries {
		back := c.lru.Back()
		if back == nil {
			break
		}
		c.lru.Remove(back)
		delete(c.entries, back.Value.(*cacheEntry).keyHash)
	}

	entry := &cacheEntry{
		keyHash:   keyHash,
		info:      info,
		isInvalid: isInvalid,
		expiresAt: now.Add(ttl),
	}
	elem := c.lru.PushFront(entry)
	c.entries[keyHash] = elem
}

// SeedKey adds a plain key directly to cache (useful for tests or pre-warming).
func (c *MemoryKeyCache) SeedKey(plainKey string, info KeyInfo, ttl time.Duration) {
	keyHash := auth.HashKey(plainKey)
	if ttl <= 0 {
		ttl = c.positiveTTL
	}
	c.setCache(keyHash, info, false, ttl)
}

// Invalidate removes a key from cache immediately (called on Revoke / Rotate).
func (c *MemoryKeyCache) Invalidate(keyHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, found := c.entries[keyHash]; found {
		c.lru.Remove(elem)
		delete(c.entries, keyHash)
	}
}

// InvalidateAll clears the entire cache.
func (c *MemoryKeyCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*list.Element)
	c.lru.Init()
}
