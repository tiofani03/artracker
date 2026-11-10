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

func TestReplayBucketIndependentOfIngest(t *testing.T) {
	// ingest burst = 2, replay burst = 2
	limiter := NewProjectRateLimiterWithReplay(1, 100, 1)
	defer limiter.Stop()

	for i := 0; i < 50; i++ {
		if ok, _ := limiter.AllowReplayRequest(7); !ok {
			break
		}
	}
	if ok, _ := limiter.AllowReplayRequest(7); ok {
		t.Fatal("replay bucket for project 7 should be drained")
	}
	if ok, _ := limiter.AllowRequest(7); !ok {
		t.Fatal("draining the replay bucket must not consume ingest requests")
	}

	for i := 0; i < 50; i++ {
		if ok, _ := limiter.AllowRequest(7); !ok {
			break
		}
	}
	if ok, _ := limiter.AllowReplayRequest(8); !ok {
		t.Fatal("another project's replay bucket must be untouched")
	}
}

func TestReplayBucketDefaults(t *testing.T) {
	limiter := NewProjectRateLimiter(50, 1000)
	defer limiter.Stop()

	// default replay rate 100 req/s, burst 2x = 200
	for i := 0; i < 200; i++ {
		if ok, _ := limiter.AllowReplayRequest(1); !ok {
			t.Fatalf("replay request #%d should be allowed", i+1)
		}
	}
	ok, retryAfter := limiter.AllowReplayRequest(1)
	if ok {
		t.Fatal("the 201st replay request should be rejected")
	}
	if retryAfter < 1 {
		t.Fatalf("retryAfter = %d, want >= 1", retryAfter)
	}
}
