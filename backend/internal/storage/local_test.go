package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestLocalStoreRoundTrip(t *testing.T) {
	s, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "projects/1/sessions/sess_abc12345/chunk_0.gz"
	want := []byte{0x1f, 0x8b, 1, 2, 3}

	if err := s.PutObject(ctx, key, bytes.NewReader(want), int64(len(want))); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, size, ct, err := s.GetObject(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, want) || size != int64(len(want)) {
		t.Fatalf("got %v (size %d), want %v", got, size, want)
	}
	if ct != "application/gzip" {
		t.Fatalf("content type = %q", ct)
	}
	if s.Bucket() != "local" {
		t.Fatalf("bucket = %q", s.Bucket())
	}
}

func TestLocalStoreRejectsTraversal(t *testing.T) {
	s, _ := NewLocal(t.TempDir())
	ctx := context.Background()
	for _, key := range []string{"../x", "/etc/passwd", "a/../../x"} {
		if err := s.PutObject(ctx, key, bytes.NewReader([]byte("x")), 1); err == nil {
			t.Errorf("put %q: expected error", key)
		}
		if _, _, _, err := s.GetObject(ctx, key); err == nil {
			t.Errorf("get %q: expected error", key)
		}
	}
}

func TestLocalStoreMissingKey(t *testing.T) {
	s, _ := NewLocal(t.TempDir())
	if _, _, _, err := s.GetObject(context.Background(), "nope/chunk_0.gz"); err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestLocalStorePresignUnsupported(t *testing.T) {
	s, _ := NewLocal(t.TempDir())
	if _, err := s.GeneratePresignedPut(context.Background(), "k", 1, time.Minute); !errors.Is(err, ErrPresignUnsupported) {
		t.Fatalf("put presign err = %v", err)
	}
	if _, err := s.GeneratePresignedGet(context.Background(), "k", time.Minute); !errors.Is(err, ErrPresignUnsupported) {
		t.Fatalf("get presign err = %v", err)
	}
}
