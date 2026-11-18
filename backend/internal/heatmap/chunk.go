// Package heatmap turns screenshot-replay chunks into taps, screen visits and background frames.
package heatmap

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"math"
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

type rawChunk struct {
	Format   string `json:"format"`
	Platform string `json:"platform"`
	Frames   []struct {
		TS     *float64 `json:"ts"`
		Screen string   `json:"screen"`
		W      float64  `json:"w"`
		H      float64  `json:"h"`
		VW     float64  `json:"vw"`
		VH     float64  `json:"vh"`
		Mime   string   `json:"mime"`
		Masked float64  `json:"masked"`
		Data   string   `json:"data"`
	} `json:"frames"`
	Touches []struct {
		TS   *float64 `json:"ts"`
		X    *float64 `json:"x"`
		Y    *float64 `json:"y"`
		Kind string   `json:"kind"`
	} `json:"touches"`
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
		raw, err = io.ReadAll(zr)
		if err != nil {
			return Chunk{}, false, err
		}
	}
	var rc rawChunk
	if err := json.Unmarshal(raw, &rc); err != nil {
		return Chunk{}, false, err
	}
	if rc.Format != screenChunkFormat {
		return Chunk{}, false, nil
	}
	c = Chunk{Seq: seq, Platform: rc.Platform}
	for i, f := range rc.Frames {
		if !finite(f.TS) || !positive(f.W) || !positive(f.H) || !positive(f.VW) || !positive(f.VH) ||
			f.Mime != "image/jpeg" || f.Data == "" {
			continue
		}
		masked := 0
		if !math.IsNaN(f.Masked) && !math.IsInf(f.Masked, 0) {
			masked = int(f.Masked)
		}
		c.Frames = append(c.Frames, Frame{
			TS: int64(*f.TS), Screen: f.Screen, VW: f.VW, VH: f.VH, Masked: masked, ChunkSeq: seq, Index: i,
		})
	}
	for _, t := range rc.Touches {
		if !finite(t.TS) || !finite(t.X) || !finite(t.Y) {
			continue
		}
		if t.Kind != "down" && t.Kind != "move" && t.Kind != "up" {
			continue
		}
		c.Touches = append(c.Touches, Touch{TS: int64(*t.TS), X: *t.X, Y: *t.Y, Kind: t.Kind})
	}
	return c, true, nil
}
