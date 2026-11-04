package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

func TestKeyCacheFastRejection(t *testing.T) {
	c := NewMemoryKeyCache(nil, time.Minute)
	ctx := context.Background()

	// Empty key
	if _, err := c.Resolve(ctx, ""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}

	// Invalid prefix
	if _, err := c.Resolve(ctx, "invalid_prefix_12345678901234567890"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}

	// Too short
	if _, err := c.Resolve(ctx, "art_live_short"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for short key, got %v", err)
	}
}

func TestKeyCacheHitAndInvalidate(t *testing.T) {
	c := NewMemoryKeyCache(nil, time.Minute)
	ctx := context.Background()

	plainKey := "art_live_12345678901234567890123456789012"
	keyHash := auth.HashKey(plainKey)

	// Pre-populate cache manually
	expectedInfo := KeyInfo{ProjectID: 42, KeyType: auth.KeyPublicClient}
	c.setCache(keyHash, expectedInfo, false, time.Minute)

	info, err := c.Resolve(ctx, plainKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != expectedInfo {
		t.Fatalf("expected %+v, got %+v", expectedInfo, info)
	}

	// Invalidate key
	c.Invalidate(keyHash)

	// After invalidate without DB, should return ErrInvalidKey
	if _, err := c.Resolve(ctx, plainKey); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey after invalidation, got %v", err)
	}
}

func TestKeyCacheNegativeHit(t *testing.T) {
	c := NewMemoryKeyCache(nil, time.Minute)
	ctx := context.Background()

	plainKey := "art_sec_12345678901234567890123456789012"
	keyHash := auth.HashKey(plainKey)

	// Pre-populate negative cache
	c.setCache(keyHash, KeyInfo{}, true, time.Minute)

	if _, err := c.Resolve(ctx, plainKey); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected negative cache hit to return ErrInvalidKey, got %v", err)
	}
}
