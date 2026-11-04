package ingest

import (
	"testing"
)

func TestRateLimiterRequestBursts(t *testing.T) {
	// RPS = 10, burst = 20
	limiter := NewProjectRateLimiter(10, 100)
	defer limiter.Stop()

	projectID := int64(101)

	// Consume entire burst (20 tokens)
	for i := 0; i < 20; i++ {
		ok, retryAfter := limiter.AllowRequest(projectID)
		if !ok {
			t.Fatalf("request #%d should be allowed, got retryAfter=%d", i+1, retryAfter)
		}
	}

	// 21st request should be rejected
	ok, retryAfter := limiter.AllowRequest(projectID)
	if ok {
		t.Fatal("request over burst should be rejected")
	}
	if retryAfter < 1 {
		t.Fatalf("expected retryAfter >= 1, got %d", retryAfter)
	}

	// Different project should have its own quota
	otherProjectID := int64(202)
	ok, _ = limiter.AllowRequest(otherProjectID)
	if !ok {
		t.Fatal("unrelated project should be allowed")
	}
}

func TestRateLimiterEventQuota(t *testing.T) {
	// EPS = 100, burst = 250
	limiter := NewProjectRateLimiter(50, 100)
	defer limiter.Stop()

	projectID := int64(303)

	// Allow 200 events
	ok, _ := limiter.AllowEvents(projectID, 200)
	if !ok {
		t.Fatal("first 200 events should be allowed")
	}

	// Remaining burst is 50. Requesting 100 more events should fail
	ok, retryAfter := limiter.AllowEvents(projectID, 100)
	if ok {
		t.Fatal("events exceeding bucket should be rejected")
	}
	if retryAfter < 1 {
		t.Fatalf("expected retryAfter >= 1, got %d", retryAfter)
	}
}
