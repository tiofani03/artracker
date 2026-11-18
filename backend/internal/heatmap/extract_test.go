package heatmap

import "testing"

func fr(ts int64, screen string, vw, vh float64, masked int) Frame {
	return Frame{TS: ts, Screen: screen, VW: vw, VH: vh, Masked: masked}
}

func td(ts int64, x, y float64) Touch { return Touch{TS: ts, X: x, Y: y, Kind: "down"} }
func tu(ts int64, x, y float64) Touch { return Touch{TS: ts, X: x, Y: y, Kind: "up"} }

func chunk(seq int, frames []Frame, touches ...Touch) Chunk {
	return Chunk{Seq: seq, Platform: "android", Frames: frames, Touches: touches}
}

func noSkip(string) bool { return false }

// tapsAt builds one down/up pair per timestamp, all at (x, 100).
func tapsAt(x float64, ts ...int64) []Touch {
	var out []Touch
	for _, t := range ts {
		out = append(out, td(t, x, 100), tu(t+20, x, 100))
	}
	return out
}

func TestExtractTapVsSwipe(t *testing.T) {
	cases := []struct {
		name    string
		touches []Touch
		want    int
	}{
		{"tap", []Touch{td(0, 100, 100), tu(100, 105, 100)}, 1},
		{"swipe", []Touch{td(0, 100, 100), tu(100, 100, 140)}, 0},
		{"long press", []Touch{td(0, 100, 100), tu(600, 105, 100)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Extract([]Chunk{chunk(0, []Frame{fr(0, "Home", 400, 800, 0)}, tc.touches...)}, noSkip)
			if len(r.Taps) != tc.want {
				t.Fatalf("taps = %d, want %d", len(r.Taps), tc.want)
			}
		})
	}
}

func TestExtractNormalization(t *testing.T) {
	r := Extract([]Chunk{chunk(0, []Frame{fr(0, "Home", 400, 800, 0)},
		td(10, 200, 400), tu(20, 200, 400), td(30, 410, -5), tu(40, 410, -5))}, noSkip)
	if len(r.Taps) != 2 {
		t.Fatalf("taps = %+v", r.Taps)
	}
	a, b := r.Taps[0], r.Taps[1]
	if a.XNorm != 0.5 || a.YNorm != 0.5 || a.Aspect != 2.0 || a.Landscape || a.VW != 400 || a.VH != 800 {
		t.Fatalf("a = %+v", a)
	}
	if b.XNorm != 1 || b.YNorm != 0 {
		t.Fatalf("b = %+v", b)
	}

	r = Extract([]Chunk{chunk(0, []Frame{fr(0, "Home", 874, 402, 0)}, td(10, 10, 10), tu(20, 10, 10))}, noSkip)
	if len(r.Taps) != 1 || !r.Taps[0].Landscape || r.Taps[0].Aspect != 2.2 {
		t.Fatalf("landscape = %+v", r.Taps)
	}
}

func TestExtractScreenAttribution(t *testing.T) {
	frames := []Frame{fr(0, "A", 400, 800, 0), fr(1000, "B", 400, 800, 0)}
	r := Extract([]Chunk{chunk(0, frames, tapsAt(10, -10, 500, 1500, 31500)...)}, noSkip)
	// -10 precedes the first frame, 31500 is more than 30 s after the last frame.
	if len(r.Taps) != 2 || r.Taps[0].Screen != "A" || r.Taps[1].Screen != "B" {
		t.Fatalf("taps = %+v", r.Taps)
	}
}

func TestExtractAcrossChunks(t *testing.T) {
	c0 := chunk(0, []Frame{fr(0, "A", 400, 800, 0)}, td(100, 50, 50))
	c1 := chunk(1, nil, tu(150, 50, 50))
	r := Extract([]Chunk{c1, c0}, noSkip)
	if len(r.Taps) != 1 || r.Taps[0].Screen != "A" {
		t.Fatalf("taps = %+v", r.Taps)
	}
	if r.Platform != "android" {
		t.Fatalf("platform = %q", r.Platform)
	}
}

func TestExtractDanglingDown(t *testing.T) {
	r := Extract([]Chunk{chunk(0, []Frame{fr(0, "A", 400, 800, 0)},
		td(0, 50, 50), td(50, 50, 50), tu(100, 50, 50), td(5000, 50, 50))}, noSkip)
	if len(r.Taps) != 1 || r.Taps[0].TS != 50 {
		t.Fatalf("taps = %+v", r.Taps)
	}
}

func TestExtractRage(t *testing.T) {
	cases := []struct {
		name string
		ts   []int64
		rage bool
	}{
		{"three within 900ms", []int64{0, 400, 900}, true},
		{"three over 1.2s", []int64{0, 600, 1200}, false},
		{"two taps", []int64{0, 400}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Extract([]Chunk{chunk(0, []Frame{fr(0, "A", 400, 800, 0)}, tapsAt(100, tc.ts...)...)}, noSkip)
			if len(r.Taps) != len(tc.ts) {
				t.Fatalf("taps = %d", len(r.Taps))
			}
			for _, tp := range r.Taps {
				if tp.Rage != tc.rage {
					t.Fatalf("rage = %v, want %v (%+v)", tp.Rage, tc.rage, r.Taps)
				}
			}
		})
	}

	t.Run("spread beyond 24dp", func(t *testing.T) {
		var touches []Touch
		touches = append(touches, tapsAt(100, 0)...)
		touches = append(touches, tapsAt(110, 300)...)
		touches = append(touches, tapsAt(130, 600)...)
		r := Extract([]Chunk{chunk(0, []Frame{fr(0, "A", 400, 800, 0)}, touches...)}, noSkip)
		for _, tp := range r.Taps {
			if tp.Rage {
				t.Fatalf("unexpected rage: %+v", r.Taps)
			}
		}
	})
}

func TestExtractUnrecognizedAndSkip(t *testing.T) {
	screens := []string{"App", "Screen", "UITrackingElementWindowController", ""}
	var frames []Frame
	var touches []Touch
	for i, s := range screens {
		ts := int64(i) * 1000
		frames = append(frames, fr(ts, s, 400, 800, 0))
		touches = append(touches, tapsAt(10, ts+100)...)
	}
	r := Extract([]Chunk{chunk(0, frames, touches...)}, noSkip)
	if len(r.Taps)+len(r.Visits)+len(r.Backgrounds) != 0 || r.Unrecognized != 4 {
		t.Fatalf("result = %+v", r)
	}

	// Unrecognized counts taps, not screens.
	frames = []Frame{fr(0, "App", 400, 800, 0), fr(5000, "Screen", 400, 800, 0)}
	touches = append(tapsAt(10, 100, 200), tapsAt(10, 5100)...)
	r = Extract([]Chunk{chunk(0, frames, touches...)}, noSkip)
	if len(r.Taps) != 0 || r.Unrecognized != 3 {
		t.Fatalf("tap count result = %+v", r)
	}

	skip := func(s string) bool { return s == "PinActivity" }
	r = Extract([]Chunk{chunk(0, []Frame{fr(0, "PinActivity", 400, 800, 0)}, tapsAt(10, 100)...)}, skip)
	if len(r.Taps)+len(r.Visits)+len(r.Backgrounds) != 0 || r.Unrecognized != 0 {
		t.Fatalf("skip result = %+v", r)
	}
}

func TestExtractVisitsAndBackgrounds(t *testing.T) {
	r := Extract([]Chunk{chunk(0, []Frame{fr(500, "A", 400, 800, 0), fr(0, "A", 400, 800, 2)})}, noSkip)
	if len(r.Visits) != 1 || r.Visits[0].TS != 0 || r.Visits[0].Aspect != 2.0 || r.Visits[0].Landscape {
		t.Fatalf("visits = %+v", r.Visits)
	}
	if len(r.Backgrounds) != 1 || r.Backgrounds[0].Frame.TS != 500 {
		t.Fatalf("backgrounds = %+v", r.Backgrounds)
	}

	// Same screen in another layout is a separate visit and background; equal masks pick the latest.
	r = Extract([]Chunk{chunk(0, []Frame{
		fr(0, "A", 400, 800, 1), fr(100, "A", 800, 400, 1), fr(200, "A", 400, 800, 1)})}, noSkip)
	if len(r.Visits) != 2 || len(r.Backgrounds) != 2 || r.Backgrounds[0].Frame.TS != 200 {
		t.Fatalf("layouts: visits=%+v backgrounds=%+v", r.Visits, r.Backgrounds)
	}
}
