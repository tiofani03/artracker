package heatmap

import (
	"sort"
	"time"
)

// Thresholds of the user flow extraction.
const (
	MinStay = time.Second      // a middle stay shorter than this is noise and is dropped
	MaxStay = 30 * time.Minute // longest duration credited to one stay
)

// View is one stay on a screen within a session. EnteredAt is epoch ms.
type View struct {
	Seq        int
	Screen     string
	EnteredAt  int64
	DurationMS int64
	IsEntry    bool
	IsExit     bool
}

type stay struct {
	screen string
	enter  int64
}

// ExtractFlow turns the frames of a session (chunks in any order) into the ordered screens it visited and
// how long it stayed on each. Unrecognized screens are ignored: they neither split nor end a stay. The
// last stay ends at the last frame or touch of the session.
func ExtractFlow(chunks []Chunk) []View {
	sorted := append([]Chunk(nil), chunks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })

	var frames []Frame
	var end int64
	for _, c := range sorted {
		frames = append(frames, c.Frames...)
		for _, f := range c.Frames {
			end = max(end, f.TS)
		}
		for _, t := range c.Touches {
			end = max(end, t.TS)
		}
	}
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].TS < frames[j].TS })

	// Collapse consecutive frames of one recognized screen into a stay entered at the first frame.
	var stays []stay
	for _, f := range frames {
		if isUnrecognized(f.Screen) {
			continue
		}
		if n := len(stays); n > 0 && stays[n-1].screen == f.Screen {
			continue
		}
		stays = append(stays, stay{f.Screen, f.TS})
	}
	if len(stays) == 0 {
		return nil
	}

	stays = dropShortMiddle(stays)

	views := make([]View, len(stays))
	for i, s := range stays {
		next := end
		if i+1 < len(stays) {
			next = stays[i+1].enter
		}
		views[i] = View{
			Seq: i, Screen: s.screen, EnteredAt: s.enter,
			DurationMS: min(next-s.enter, MaxStay.Milliseconds()),
			IsEntry:    i == 0, IsExit: i == len(stays)-1,
		}
	}
	return views
}

// dropShortMiddle repeatedly removes the shortest stay that is neither first nor last and lasts less than
// MinStay (up to the next stay's entry; the earliest wins a tie), merging neighbours that end up on the
// same screen. Shortest first, so a blip between two stays goes before the stay it interrupts.
func dropShortMiddle(stays []stay) []stay {
	for {
		drop := -1
		var shortest int64
		for i := 1; i+1 < len(stays); i++ {
			d := stays[i+1].enter - stays[i].enter
			if time.Duration(d)*time.Millisecond < MinStay && (drop < 0 || d < shortest) {
				drop, shortest = i, d
			}
		}
		if drop < 0 {
			return stays
		}
		stays = append(stays[:drop], stays[drop+1:]...)
		if stays[drop-1].screen == stays[drop].screen {
			stays = append(stays[:drop], stays[drop+1:]...)
		}
	}
}
