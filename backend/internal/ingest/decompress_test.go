package ingest

import (
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecompressValidGzip(t *testing.T) {
	d := NewDecompressor()
	original := []byte(`{"hello":"world"}`)
	compressed := gzipBytes(t, original)

	out, err := d.Gunzip(compressed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, original) {
		t.Fatalf("expected %s, got %s", original, out)
	}
}

func TestDecompressInvalidGzip(t *testing.T) {
	d := NewDecompressor()
	_, err := d.Gunzip([]byte("not a gzip stream"))
	if !errors.Is(err, ErrInvalidGzip) {
		t.Fatalf("expected ErrInvalidGzip, got %v", err)
	}

	_, err = d.Gunzip(nil)
	if !errors.Is(err, ErrInvalidGzip) {
		t.Fatalf("expected ErrInvalidGzip for empty bytes, got %v", err)
	}
}

func TestDecompressZipBombProtection(t *testing.T) {
	d := NewDecompressor()
	// Create payload larger than 2MB
	bigData := make([]byte, MaxUncompressedBytes+1024)
	compressed := gzipBytes(t, bigData)

	_, err := d.Gunzip(compressed)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}
}
