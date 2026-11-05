package analytics

import (
	"sync"
	"time"
)

type tokenBucket struct {
	rate       float64
	capacity   float64
	tokens     float64
	lastUpdate time.Time
}

func newTokenBucket(rate, capacity float64, now time.Time) *tokenBucket {
	return &tokenBucket{
		rate:       rate,
		capacity:   capacity,
		tokens:     capacity,
		lastUpdate: now,
	}
}

func (tb *tokenBucket) allowN(now time.Time, n float64) (bool, int) {
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

	deficit := n - tb.tokens
	retryAfter := int(deficit/tb.rate) + 1
	if retryAfter < 1 {
		retryAfter = 1
	}
	return false, retryAfter
}

type projectLimiters struct {
	reqBucket   *tokenBucket
	eventBucket *tokenBucket
	lastActive  time.Time
}

type rateLimitShard struct {
	mu       sync.Mutex
	limiters map[int64]*projectLimiters
}

type ProjectRateLimiter struct {
	shards          [numShards]rateLimitShard
	defaultRPS      float64
	defaultBurstReq float64
	defaultEPS      float64
	defaultBurstEvt float64
	cleanupInterval time.Duration
	stopCh          chan struct{}
}

func NewProjectRateLimiter(rps, eps float64) *ProjectRateLimiter {
	if rps <= 0 {
		rps = 50.0
	}
	if eps <= 0 {
		eps = 1000.0
	}

	prl := &ProjectRateLimiter{
		defaultRPS:      rps,
		defaultBurstReq: rps * 2,
		defaultEPS:      eps,
		defaultBurstEvt: eps * 2.5,
		cleanupInterval: 5 * time.Minute,
		stopCh:          make(chan struct{}),
	}

	for i := 0; i < numShards; i++ {
		prl.shards[i].limiters = make(map[int64]*projectLimiters)
	}

	go prl.cleanupLoop()

	return prl
}

func (prl *ProjectRateLimiter) getShard(projectID int64) *rateLimitShard {
	idx := uint64(projectID) % numShards
	return &prl.shards[idx]
}

func (prl *ProjectRateLimiter) AllowRequest(projectID int64) (bool, int) {
	s := prl.getShard(projectID)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	p, exists := s.limiters[projectID]
	if !exists {
		p = &projectLimiters{
			reqBucket:   newTokenBucket(prl.defaultRPS, prl.defaultBurstReq, now),
			eventBucket: newTokenBucket(prl.defaultEPS, prl.defaultBurstEvt, now),
			lastActive:  now,
		}
		s.limiters[projectID] = p
	}
	p.lastActive = now

	return p.reqBucket.allowN(now, 1)
}

func (prl *ProjectRateLimiter) AllowEvents(projectID int64, eventCount int) (bool, int) {
	if eventCount <= 0 {
		return true, 0
	}
	s := prl.getShard(projectID)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	p, exists := s.limiters[projectID]
	if !exists {
		p = &projectLimiters{
			reqBucket:   newTokenBucket(prl.defaultRPS, prl.defaultBurstReq, now),
			eventBucket: newTokenBucket(prl.defaultEPS, prl.defaultBurstEvt, now),
			lastActive:  now,
		}
		s.limiters[projectID] = p
	}
	p.lastActive = now

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

func (prl *ProjectRateLimiter) Stop() {
	select {
	case <-prl.stopCh:
	default:
		close(prl.stopCh)
	}
}
