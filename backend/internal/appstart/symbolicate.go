// Package appstart holds the pure app start profiling logic: symbolication of sampled profiles and
// per-function statistics.
package appstart

import "github.com/tiofani03/artracker/backend/internal/ingest"

// ResolvedFrame is one symbolicated frame.
type ResolvedFrame struct {
	Function string `json:"function"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	IsApp    bool   `json:"is_app"`
	Resolved bool   `json:"resolved"`
}

// Resolver turns one raw profile frame into symbolicated frames.
type Resolver interface {
	// Resolve returns 1+ frames, innermost first (Android inline expansion), never 0.
	// caller is true when the frame is not the leaf of the stack it appears in.
	Resolve(f ingest.ProfileFrame, caller bool) []ResolvedFrame
	// MissingSymbols reports that an app image or mapping had no artifact.
	MissingSymbols() bool
}

// Symbolicated is a profile whose frames have been resolved. Stacks are root-first indexes into Frames.
type Symbolicated struct {
	IntervalMs int                    `json:"interval_ms"`
	Frames     []ResolvedFrame        `json:"frames"`
	Stacks     [][]int                `json:"stacks"`
	Samples    []ingest.ProfileSample `json:"samples"`
}

type resolveKey struct {
	frame  int
	caller bool
}

// Symbolicate resolves every frame of p with r. Each unique (frame index, caller) pair is resolved once:
// in a root-first stack every position except the last is a caller. A frame that expands to several
// frames is replaced in every stack by its expansion, outer first, so inlined callees sit leaf-wards.
// Identical resolved frames are merged into one frame table.
func Symbolicate(p ingest.AppStartProfile, r Resolver) Symbolicated {
	out := Symbolicated{
		IntervalMs: p.IntervalMs,
		Frames:     []ResolvedFrame{},
		Stacks:     make([][]int, 0, len(p.Stacks)),
		Samples:    p.Samples,
	}
	table := map[ResolvedFrame]int{}
	expansions := map[resolveKey][]int{} // output frame indexes, root-first

	expand := func(frame int, caller bool) []int {
		key := resolveKey{frame, caller}
		if idx, ok := expansions[key]; ok {
			return idx
		}
		resolved := r.Resolve(p.Frames[frame], caller)
		idx := make([]int, 0, len(resolved))
		for i := len(resolved) - 1; i >= 0; i-- { // Resolve is innermost first; stacks are root-first
			rf := resolved[i]
			n, ok := table[rf]
			if !ok {
				n = len(out.Frames)
				table[rf] = n
				out.Frames = append(out.Frames, rf)
			}
			idx = append(idx, n)
		}
		expansions[key] = idx
		return idx
	}

	for _, stack := range p.Stacks {
		row := make([]int, 0, len(stack))
		for pos, frame := range stack {
			if frame < 0 || frame >= len(p.Frames) {
				continue
			}
			row = append(row, expand(frame, pos < len(stack)-1)...)
		}
		out.Stacks = append(out.Stacks, row)
	}
	return out
}
