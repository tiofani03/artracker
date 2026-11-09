package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrPresignUnsupported is returned by stores that cannot issue presigned URLs; callers must
// use the API's signed upload/download proxy instead.
var ErrPresignUnsupported = errors.New("presigned URLs are not supported by this replay store")

// LocalStore keeps replay chunks on the local filesystem. Intended for development and tests
// when R2/S3 is not configured.
type LocalStore struct {
	dir string
}

var _ ReplayStore = (*LocalStore)(nil)

// NewLocal creates (if needed) and uses dir as the root for replay objects.
func NewLocal(dir string) (*LocalStore, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve replay dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create replay dir: %w", err)
	}
	return &LocalStore{dir: abs}, nil
}

// path maps an object key to a file under the root, rejecting absolute or escaping keys.
func (s *LocalStore) path(objectKey string) (string, error) {
	if objectKey == "" || strings.HasPrefix(objectKey, "/") || strings.Contains(objectKey, "..") {
		return "", fmt.Errorf("invalid object key %q", objectKey)
	}
	p := filepath.Join(s.dir, filepath.FromSlash(objectKey))
	if !strings.HasPrefix(p, s.dir+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid object key %q", objectKey)
	}
	return p, nil
}

func (s *LocalStore) GeneratePresignedPut(context.Context, string, int64, time.Duration) (string, error) {
	return "", ErrPresignUnsupported
}

func (s *LocalStore) GeneratePresignedGet(context.Context, string, time.Duration) (string, error) {
	return "", ErrPresignUnsupported
}

func (s *LocalStore) PutObject(_ context.Context, objectKey string, data io.Reader, _ int64) error {
	p, err := s.path(objectKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create object dir: %w", err)
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create object: %w", err)
	}
	if _, err := io.Copy(f, data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write object: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close object: %w", err)
	}
	return os.Rename(tmp, p)
}

func (s *LocalStore) GetObject(_ context.Context, objectKey string) (io.ReadCloser, int64, string, error) {
	p, err := s.path(objectKey)
	if err != nil {
		return nil, 0, "", err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, "", fmt.Errorf("open object: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, "", fmt.Errorf("stat object: %w", err)
	}
	return f, info.Size(), "application/gzip", nil
}

func (s *LocalStore) Bucket() string { return "local" }
