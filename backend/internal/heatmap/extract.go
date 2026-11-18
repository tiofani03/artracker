package heatmap

import (
	"math"
	"sort"
	"time"
)

// Thresholds of the extraction and the worker that drives it.
const (
	TapMaxDistance   = 10.0 // dp between down and up
	TapMaxDuration   = 500 * time.Millisecond
	RageMinTaps      = 3
	RageWindow       = time.Second
	RageRadius       = 24.0 // dp from the first tap of the window
	StaleFrameLimit  = 30 * time.Second
	IdleBeforeWorker = 10 * time.Minute
	WorkerTick       = time.Minute
)

// Tap is one tap attributed to a screen. XNorm/YNorm are fractions of the viewport, clamped to [0,1].
type Tap struct {
	Screen          string
	TS              int64
	XNorm, YNorm    float64
	VW, VH, Aspect  float64
	Landscape, Rage bool
}

// Visit is the first time a screen was seen in one (landscape, aspect) layout.
type Visit struct {
	Screen    string
	TS        int64
	Landscape bool
	Aspect    float64
}

// Background is the best frame to draw taps over for one (screen, landscape, aspect).
type Background struct {
	Screen    string
	Landscape bool
	Aspect    float64
	Frame     Frame
}

// Result is the output of Extract for one session.
type Result struct {
	Platform     string
	Taps         []Tap
	Visits       []Visit
	Backgrounds  []Background
	Unrecognized int // distinct unrecognized screen names seen
}

type layoutKey struct {
	screen    string
	landscape bool
	aspect    float64
}

func aspectOf(vw, vh float64) float64 {
	return math.Round(math.Max(vw, vh)/math.Min(vw, vh)*10) / 10
}

func clamp01(v float64) float64 { return math.Min(1, math.Max(0, v)) }

// Extract treats chunks (any order) as one stream sorted by ts. skip(screen) reports screens to drop
// entirely (sensitive / excluded); unrecognized screens are dropped and counted in Unrecognized.
func Extract(chunks []Chunk, skip func(screen string) bool) Result {
	sorted := append([]Chunk(nil), chunks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })

	var res Result
	var frames []Frame
	var touches []Touch
	for _, c := range sorted {
		if res.Platform == "" {
			res.Platform = c.Platform
		}
		frames = append(frames, c.Frames...)
		touches = append(touches, c.Touches...)
	}
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].TS < frames[j].TS })
	sort.SliceStable(touches, func(i, j int) bool { return touches[i].TS < touches[j].TS })

	unrecognized := map[string]bool{}
	// drop reports whether a screen is excluded, counting unrecognized names once.
	drop := func(screen string) bool {
		if isUnrecognized(screen) {
			unrecognized[screen] = true
			return true
		}
		return skip != nil && skip(screen)
	}

	// Visits and backgrounds.
	visitIdx := map[layoutKey]int{}
	bgIdx := map[layoutKey]int{}
	for _, f := range frames {
		if drop(f.Screen) {
			continue
		}
		k := layoutKey{f.Screen, f.VW > f.VH, aspectOf(f.VW, f.VH)}
		if _, seen := visitIdx[k]; !seen {
			visitIdx[k] = len(res.Visits)
			res.Visits = append(res.Visits, Visit{Screen: k.screen, TS: f.TS, Landscape: k.landscape, Aspect: k.aspect})
		}
		if i, seen := bgIdx[k]; !seen {
			bgIdx[k] = len(res.Backgrounds)
			res.Backgrounds = append(res.Backgrounds, Background{Screen: k.screen, Landscape: k.landscape, Aspect: k.aspect, Frame: f})
		} else if cur := res.Backgrounds[i].Frame; f.Masked < cur.Masked || (f.Masked == cur.Masked && f.TS >= cur.TS) {
			res.Backgrounds[i].Frame = f
		}
	}

	// Taps: pair down -> up with a single open slot; a new down replaces an open one.
	var open *Touch
	var raw []Touch // raw down position of each tap, parallel to res.Taps, for the rage distance
	for i := range touches {
		t := touches[i]
		switch t.Kind {
		case "down":
			open = &touches[i]
		case "up":
			if open == nil {
				continue
			}
			d := *open
			open = nil
			if time.Duration(t.TS-d.TS)*time.Millisecond >= TapMaxDuration ||
				math.Hypot(t.X-d.X, t.Y-d.Y) >= TapMaxDistance {
				continue
			}
			f, ok := frameAt(frames, d.TS)
			if !ok || time.Duration(d.TS-f.TS)*time.Millisecond > StaleFrameLimit || drop(f.Screen) {
				continue
			}
			res.Taps = append(res.Taps, Tap{
				Screen: f.Screen, TS: d.TS,
				XNorm: clamp01(d.X / f.VW), YNorm: clamp01(d.Y / f.VH),
				VW: f.VW, VH: f.VH, Aspect: aspectOf(f.VW, f.VH), Landscape: f.VW > f.VH,
			})
			raw = append(raw, d)
		}
	}
	markRage(res.Taps, raw)
	res.Unrecognized = len(unrecognized)
	return res
}

// frameAt is the frame with the greatest ts <= ts, like activeScreenFrameAt in the frontend.
func frameAt(frames []Frame, ts int64) (Frame, bool) {
	i := sort.Search(len(frames), func(i int) bool { return frames[i].TS > ts })
	if i == 0 {
		return Frame{}, false
	}
	return frames[i-1], true
}

// markRage flags taps that belong to a burst: at least RageMinTaps consecutive taps on one screen within
// RageWindow, each within RageRadius (raw dp) of the first tap of the burst. pos[i] is the raw position of taps[i].
func markRage(taps []Tap, pos []Touch) {
	byScreen := map[string][]int{}
	for i, t := range taps {
		byScreen[t.Screen] = append(byScreen[t.Screen], i)
	}
	for _, idx := range byScreen {
		for s := 0; s < len(idx); s++ {
			first := idx[s]
			e := s + 1
			for e < len(idx) {
				j := idx[e]
				if time.Duration(taps[j].TS-taps[first].TS)*time.Millisecond > RageWindow ||
					math.Hypot(pos[j].X-pos[first].X, pos[j].Y-pos[first].Y) > RageRadius {
					break
				}
				e++
			}
			if e-s >= RageMinTaps {
				for _, i := range idx[s:e] {
					taps[i].Rage = true
				}
			}
		}
	}
}
