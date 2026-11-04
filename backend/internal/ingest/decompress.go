package ingest

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"sync"
)

var (
	ErrPayloadTooLarge = errors.New("payload exceeds maximum size")
	ErrInvalidGzip     = errors.New("invalid gzip stream")
)

type Decompressor struct {
	readerPool sync.Pool
	bufPool    sync.Pool
}

func NewDecompressor() *Decompressor {
	return &Decompressor{
		readerPool: sync.Pool{
			New: func() any {
				return new(gzip.Reader)
			},
		},
		bufPool: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, 32*1024))
			},
		},
	}
}

// Gunzip decompresses a gzip byte slice safely with zip-bomb protection.
func (d *Decompressor) Gunzip(compressed []byte) ([]byte, error) {
	if len(compressed) == 0 {
		return nil, ErrInvalidGzip
	}

	zr := d.readerPool.Get().(*gzip.Reader)
	defer d.readerPool.Put(zr)

	if err := zr.Reset(bytes.NewReader(compressed)); err != nil {
		return nil, errors.Join(ErrInvalidGzip, err)
	}
	defer zr.Close()

	buf := d.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer d.bufPool.Put(buf)

	// Read at most MaxUncompressedBytes + 1 to detect oversize
	limited := io.LimitReader(zr, MaxUncompressedBytes+1)
	n, err := buf.ReadFrom(limited)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.Join(ErrInvalidGzip, err)
	}

	if n > MaxUncompressedBytes {
		return nil, ErrPayloadTooLarge
	}

	// Make an independent copy of bytes so buffer can be returned to pool safely
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, nil
}
