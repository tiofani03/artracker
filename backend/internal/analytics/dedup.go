package analytics

import (
	"fmt"
	"hash/fnv"
	"sync"
	"time"
)

// Deduplicator checks whether an event ID for a given project was seen recently.
type Deduplicator interface {
	// IsDuplicate returns true if (projectID, eventID) was already registered within TTL.
	// If false, it registers the event ID and returns false.
	IsDuplicate(projectID int64, eventID string) bool
}

const numShards = 16

type dedupEntry struct {
	seenAt time.Time
}

type dedupShard struct {
	mu      sync.RWMutex
	entries map[string]dedupEntry
}

// MemoryDeduplicator is a sharded, TTL-based in-memory cache to detect duplicate events.
type MemoryDeduplicator struct {
	shards [numShards]*dedupShard
	ttl    time.Duration
	stopCh chan struct{}
}

// NewMemoryDeduplicator creates a deduplicator with the given TTL (default: 15 minutes).
func NewMemoryDeduplicator(ttl time.Duration) *MemoryDeduplicator {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	d := &MemoryDeduplicator{
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	for i := 0; i < numShards; i++ {
		d.shards[i] = &dedupShard{
			entries: make(map[string]dedupEntry),
		}
	}

	go d.cleanupLoop()
	return d
}

func (d *MemoryDeduplicator) getShard(key string) *dedupShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return d.shards[h.Sum32()%numShards]
}

func (d *MemoryDeduplicator) IsDuplicate(projectID int64, eventID string) bool {
	if eventID == "" {
		return false
	}
	key := fmt.Sprintf("%d:%s", projectID, eventID)
	shard := d.getShard(key)
	now := time.Now()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if entry, exists := shard.entries[key]; exists {
		if now.Sub(entry.seenAt) <= d.ttl {
			return true
		}
	}

	shard.entries[key] = dedupEntry{seenAt: now}
	return false
}

func (d *MemoryDeduplicator) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case now := <-ticker.C:
			d.evictExpired(now)
		}
	}
}

func (d *MemoryDeduplicator) evictExpired(now time.Time) {
	for i := 0; i < numShards; i++ {
		shard := d.shards[i]
		shard.mu.Lock()
		for k, entry := range shard.entries {
			if now.Sub(entry.seenAt) > d.ttl {
				delete(shard.entries, k)
			}
		}
		shard.mu.Unlock()
	}
}

func (d *MemoryDeduplicator) Close() {
	select {
	case <-d.stopCh:
	default:
		close(d.stopCh)
	}
}
