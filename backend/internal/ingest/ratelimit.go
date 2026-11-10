package ingest

import (
	"sync"
	"time"
)

// TokenBucket implements a standard token bucket rate limiter.
type TokenBucket struct {
	rate       float64 // tokens per second
	capacity   float64 // maximum burst tokens
	tokens     float64
	lastUpdate time.Time
}

func newTokenBucket(rate, capacity float64, now time.Time) *TokenBucket {
	return &TokenBucket{
		rate:       rate,
		capacity:   capacity,
		tokens:     capacity,
		lastUpdate: now,
	}
}

// allowN checks and consumes n tokens if available.
// Returns (allowed, retryAfterSeconds).
func (tb *TokenBucket) allowN(now time.Time, n float64) (bool, int) {
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	if elapsed > 0 {
		tb.tokens += elapsed * tb.rate
		if tb.tokens > tb.capacity {
			tb.tokens = tb.capacity
		}
		tb.lastUpdate = now
	}

	if tb.tokens >= n {
		tb.tokens -= n
		return true, 0
	}

	// Calculate retry-after
	deficit := n - tb.tokens
	retryAfter := int(deficit/tb.rate) + 1
	if retryAfter < 1 {
		retryAfter = 1
	}
	return false, retryAfter
}

type projectLimiters struct {
	reqBucket    *TokenBucket
	eventBucket  *TokenBucket
	replayBucket *TokenBucket
	lastActive   time.Time
}

// defaultReplayRPS is the per-project replay request rate when none is configured.
const defaultReplayRPS = 100.0

const numShards = 16

type shard struct {
	mu       sync.Mutex
	limiters map[int64]*projectLimiters
}

// ProjectRateLimiter manages token buckets per project with sharding.
type ProjectRateLimiter struct {
	shards           [numShards]shard
	defaultRPS       float64
	defaultBurstReq  float64
	defaultEPS       float64
	defaultBurstEvt  float64
	defaultReplayRPS float64
	defaultBurstRepl float64
	cleanupInterval  time.Duration
	stopCh           chan struct{}
}

// NewProjectRateLimiter creates a new sharded token bucket rate limiter with the default replay rate.
func NewProjectRateLimiter(rps, eps float64) *ProjectRateLimiter {
	return NewProjectRateLimiterWithReplay(rps, eps, defaultReplayRPS)
}

// NewProjectRateLimiterWithReplay is NewProjectRateLimiter with an explicit per-project replay request
// rate (burst 2x). Replay requests use their own bucket so they never starve telemetry ingest.
func NewProjectRateLimiterWithReplay(rps, eps, replayRPS float64) *ProjectRateLimiter {
	if replayRPS <= 0 {
		replayRPS = defaultReplayRPS
	}
	if rps <= 0 {
		rps = 50.0
	}
	if eps <= 0 {
		eps = 1000.0
	}

	prl := &ProjectRateLimiter{
		defaultRPS:       rps,
		defaultBurstReq:  rps * 2,
		defaultEPS:       eps,
		defaultBurstEvt:  eps * 2.5,
		defaultReplayRPS: replayRPS,
		defaultBurstRepl: replayRPS * 2,
		cleanupInterval:  5 * time.Minute,
		stopCh:           make(chan struct{}),
	}

	for i := 0; i < numShards; i++ {
		prl.shards[i].limiters = make(map[int64]*projectLimiters)
	}

	go prl.cleanupLoop()

	return prl
}

func (prl *ProjectRateLimiter) getShard(projectID int64) *shard {
	idx := uint64(projectID) % numShards
	return &prl.shards[idx]
}

// limitersLocked returns the project's buckets, creating them on first use. s.mu must be held.
func (prl *ProjectRateLimiter) limitersLocked(s *shard, projectID int64, now time.Time) *projectLimiters {
	p, exists := s.limiters[projectID]
	if !exists {
		p = &projectLimiters{
			reqBucket:    newTokenBucket(prl.defaultRPS, prl.defaultBurstReq, now),
			eventBucket:  newTokenBucket(prl.defaultEPS, prl.defaultBurstEvt, now),
			replayBucket: newTokenBucket(prl.defaultReplayRPS, prl.defaultBurstRepl, now),
			lastActive:   now,
		}
		s.limiters[projectID] = p
	}
	p.lastActive = now
	return p
}

// AllowReplayRequest checks if project is allowed to make 1 replay HTTP request (own bucket).
func (prl *ProjectRateLimiter) AllowReplayRequest(projectID int64) (bool, int) {
	s := prl.getShard(projectID)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	return prl.limitersLocked(s, projectID, now).replayBucket.allowN(now, 1)
}

// AllowRequest checks if project is allowed to make 1 HTTP request.
func (prl *ProjectRateLimiter) AllowRequest(projectID int64) (bool, int) {
	s := prl.getShard(projectID)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	p := prl.limitersLocked(s, projectID, now)

	return p.reqBucket.allowN(now, 1)
}

// AllowEvents checks if project has quota for n events.
func (prl *ProjectRateLimiter) AllowEvents(projectID int64, eventCount int) (bool, int) {
	if eventCount <= 0 {
		return true, 0
	}
	s := prl.getShard(projectID)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	p := prl.limitersLocked(s, projectID, now)

	return p.eventBucket.allowN(now, float64(eventCount))
}

func (prl *ProjectRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(prl.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-prl.stopCh:
			return
		case now := <-ticker.C:
			threshold := now.Add(-15 * time.Minute)
			for i := 0; i < numShards; i++ {
				s := &prl.shards[i]
				s.mu.Lock()
				for pid, p := range s.limiters {
					if p.lastActive.Before(threshold) {
						delete(s.limiters, pid)
					}
				}
				s.mu.Unlock()
			}
		}
	}
}

// Stop stops the background cleanup goroutine.
func (prl *ProjectRateLimiter) Stop() {
	select {
	case <-prl.stopCh:
	default:
		close(prl.stopCh)
	}
}
