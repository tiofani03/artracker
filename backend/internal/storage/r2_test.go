package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewR2Validation(t *testing.T) {
	_, err := NewR2(Config{})
	if err == nil {
		t.Fatal("expected error on empty config, got nil")
	}

	_, err = NewR2(Config{BucketName: "test-bucket"})
	if err == nil {
		t.Fatal("expected error on missing access keys, got nil")
	}

	_, err = NewR2(Config{
		BucketName:      "test-bucket",
		AccessKeyID:     "test-key",
		AccessKeySecret: "test-secret",
	})
	if err == nil {
		t.Fatal("expected error on missing endpoint/account_id, got nil")
	}
}

func TestR2PresignedURLs(t *testing.T) {
	cfg := Config{
		AccountID:       "dummyaccount123",
		AccessKeyID:     "dummykey123",
		AccessKeySecret: "dummysecret123",
		BucketName:      "artracker-storage",
		Region:          "auto",
	}

	store, err := NewR2(cfg)
	if err != nil {
		t.Fatalf("unexpected NewR2 error: %v", err)
	}

	if store.Bucket() != "artracker-storage" {
		t.Errorf("expected bucket 'artracker-storage', got '%s'", store.Bucket())
	}

	ctx := context.Background()

	// Test GeneratePresignedPut
	putURL, err := store.GeneratePresignedPut(ctx, "projects/1/sessions/sess_abc/chunk_0.gz", 45000, 5*time.Minute)
	if err != nil {
		t.Fatalf("unexpected GeneratePresignedPut error: %v", err)
	}

	if !strings.Contains(putURL, "artracker-storage") {
		t.Errorf("putURL should contain bucket name: %s", putURL)
	}
	if !strings.Contains(putURL, "X-Amz-Signature=") {
		t.Errorf("putURL should contain signature: %s", putURL)
	}
	if !strings.Contains(putURL, "chunk_0.gz") {
		t.Errorf("putURL should contain object key: %s", putURL)
	}

	// Test GeneratePresignedGet
	getURL, err := store.GeneratePresignedGet(ctx, "projects/1/sessions/sess_abc/chunk_0.gz", 15*time.Minute)
	if err != nil {
		t.Fatalf("unexpected GeneratePresignedGet error: %v", err)
	}

	if !strings.Contains(getURL, "artracker-storage") {
		t.Errorf("getURL should contain bucket name: %s", getURL)
	}
	if !strings.Contains(getURL, "X-Amz-Signature=") {
		t.Errorf("getURL should contain signature: %s", getURL)
	}
}
