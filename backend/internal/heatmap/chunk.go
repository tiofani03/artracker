// Package heatmap turns screenshot-replay chunks into taps, screen visits and background frames.
package heatmap

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const screenChunkFormat = "artracker-screen-v1"

// Frame is the metadata of one captured screenshot; the image data is not kept.
type Frame struct {
	TS       int64
	Screen   string
	VW, VH   float64
	Masked   int
	ChunkSeq int
	Index    int
}

// Touch is one raw touch sample. Kind is "down", "move" or "up".
type Touch struct {
	TS   int64
	X, Y float64
	Kind string
}

// Chunk is one decoded artracker-screen-v1 chunk.
type Chunk struct {
	Seq      int
	Platform string
	Frames   []Frame
	Touches  []Touch
}

type rawFrame struct {
	TS     *float64 `json:"ts"`
	Screen any      `json:"screen"` // a non-string counts as "" (unrecognized), like masked
	W      float64  `json:"w"`
	H      float64  `json:"h"`
	VW     float64  `json:"vw"`
	VH     float64  `json:"vh"`
	Mime   string   `json:"mime"`
	Masked any      `json:"masked"` // iOS sends a bool; like the frontend, non-numbers count as 0
	Data   string   `json:"data"`
}

type rawTouch struct {
	TS   *float64 `json:"ts"`
	X    *float64 `json:"x"`
	Y    *float64 `json:"y"`
	Kind string   `json:"kind"`
}

// maxDecompressed caps the size of one decompressed chunk. A variable so tests can lower it.
var maxDecompressed int64 = 64 << 20

// Epoch-ms bounds (years 2000 to 2100) for plausible frame and touch timestamps.
const (
	minTS = 946684800000
	maxTS = 4102444800000
)

// maxViewport keeps viewport sizes and masked counts inside Postgres REAL/INT.
const maxViewport = 1e6

func tsOK(p *float64) bool { return finite(p) && *p >= minTS && *p <= maxTS }

// cleanText drops invalid UTF-8 and NUL bytes, which Postgres TEXT rejects.
func cleanText(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\x00", "")
}

// maxScreenBytes caps screen names so they fit a btree index key.
const maxScreenBytes = 256

// truncateUTF8 cuts s to at most n bytes on a rune boundary.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func positive(v float64) bool { return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }

func finite(p *float64) bool { return p != nil && !math.IsInf(*p, 0) && !math.IsNaN(*p) }

// DecodeChunk gunzips (or reads plain JSON) one chunk; ok=false for non artracker-screen-v1 payloads.
// Invalid frames and touches are dropped, mirroring frontend/src/replay/screen.ts. Frame image data is
// not kept.
func DecodeChunk(seq int, raw []byte) (c Chunk, ok bool, err error) {
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, zerr := gzip.NewReader(bytes.NewReader(raw))
		if zerr != nil {
			return Chunk{}, false, zerr
		}
		raw, err = io.ReadAll(io.LimitReader(zr, maxDecompressed+1))
		if err != nil {
			return Chunk{}, false, err
		}
		if int64(len(raw)) > maxDecompressed {
			return Chunk{}, false, errors.New("heatmap: decompressed chunk too large")
		}
	}
	if !json.Valid(raw) {
		return Chunk{}, false, errors.New("heatmap: chunk is not valid JSON")
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return Chunk{}, false, nil // valid JSON but not an object (e.g. an rrweb event array)
	}
	var format string
	if json.Unmarshal(top["format"], &format) != nil || format != screenChunkFormat {
		return Chunk{}, false, nil
	}
	c = Chunk{Seq: seq}
	_ = json.Unmarshal(top["platform"], &c.Platform)
	c.Platform = cleanText(c.Platform)

	// Items are decoded one by one so a wrong-typed entry is dropped alone, like the frontend does.
	var rawFrames, rawTouches []json.RawMessage
	_ = json.Unmarshal(top["frames"], &rawFrames)
	_ = json.Unmarshal(top["touches"], &rawTouches)
	for i, item := range rawFrames {
		var f rawFrame
		if json.Unmarshal(item, &f) != nil {
			continue
		}
		if !tsOK(f.TS) || !positive(f.W) || !positive(f.H) || !positive(f.VW) || !positive(f.VH) || f.VW < 1 || f.VH < 1 || f.VW > maxViewport || f.VH > maxViewport ||
			f.Mime != "image/jpeg" || f.Data == "" {
			continue
		}
		masked := 0
		if m, isNum := f.Masked.(float64); isNum && !math.IsNaN(m) && !math.IsInf(m, 0) {
			masked = int(math.Max(0, math.Min(m, maxViewport)))
		}
		screen, _ := f.Screen.(string)
		c.Frames = append(c.Frames, Frame{
			TS: int64(*f.TS), Screen: truncateUTF8(cleanText(screen), maxScreenBytes), VW: f.VW, VH: f.VH, Masked: masked, ChunkSeq: seq, Index: i,
		})
	}
	for _, item := range rawTouches {
		var t rawTouch
		if json.Unmarshal(item, &t) != nil || !tsOK(t.TS) || !finite(t.X) || !finite(t.Y) {
			continue
		}
		if t.Kind != "down" && t.Kind != "move" && t.Kind != "up" {
			continue
		}
		c.Touches = append(c.Touches, Touch{TS: int64(*t.TS), X: *t.X, Y: *t.Y, Kind: t.Kind})
	}
	return c, true, nil
}
