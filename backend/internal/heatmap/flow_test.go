package heatmap

import (
	"testing"
	"time"
)

func f(ts int64, screen string) Frame { return fr(ts, screen, 360, 800, 0) }

// assertFlow compares views and checks the entry/exit flags and seq numbering.
func assertFlow(t *testing.T, got []View, want []View) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d views %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		w := want[i]
		w.Seq = i
		w.IsEntry = i == 0
		w.IsExit = i == len(want)-1
		if got[i] != w {
			t.Errorf("view %d = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestFlowCollapsesConsecutiveFrames(t *testing.T) {
	c := chunk(0, []Frame{f(0, "Home"), f(500, "Home"), f(3000, "Cart"), f(4000, "Cart")},
		td(9000, 1, 1))
	assertFlow(t, ExtractFlow([]Chunk{c}), []View{
		{Screen: "Home", EnteredAt: 0, DurationMS: 3000},
		{Screen: "Cart", EnteredAt: 3000, DurationMS: 6000},
	})
}

func TestFlowRevisit(t *testing.T) {
	c := chunk(0, []Frame{f(0, "A"), f(5000, "B"), f(10000, "A"), f(12000, "A")})
	assertFlow(t, ExtractFlow([]Chunk{c}), []View{
		{Screen: "A", EnteredAt: 0, DurationMS: 5000},
		{Screen: "B", EnteredAt: 5000, DurationMS: 5000},
		{Screen: "A", EnteredAt: 10000, DurationMS: 2000},
	})
}

func TestFlowDropsUnrecognized(t *testing.T) {
	c := chunk(0, []Frame{f(0, "Home"), f(2000, "App"), f(2500, "UITrackingElementWindowController"),
		f(3000, "Home"), f(8000, "Cart")})
	assertFlow(t, ExtractFlow([]Chunk{c}), []View{
		{Screen: "Home", EnteredAt: 0, DurationMS: 8000},
		{Screen: "Cart", EnteredAt: 8000, DurationMS: 0},
	})
}

func TestFlowUnrecognizedFramesStillExtendSessionEnd(t *testing.T) {
	c := chunk(0, []Frame{f(0, "Home"), f(6000, "App")})
	assertFlow(t, ExtractFlow([]Chunk{c}), []View{
		{Screen: "Home", EnteredAt: 0, DurationMS: 6000},
	})
}

func TestFlowAllUnrecognized(t *testing.T) {
	c := chunk(0, []Frame{f(0, "App"), f(1000, "App")})
	if got := ExtractFlow([]Chunk{c}); len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
	if got := ExtractFlow(nil); len(got) != 0 {
		t.Fatalf("got %+v for no chunks, want none", got)
	}
}

func TestFlowShortMiddleStayRemerged(t *testing.T) {
	c := chunk(0, []Frame{f(0, "A"), f(2000, "B"), f(2400, "C"), f(2600, "B"), f(9000, "D")})
	assertFlow(t, ExtractFlow([]Chunk{c}), []View{
		{Screen: "A", EnteredAt: 0, DurationMS: 2000},
		{Screen: "B", EnteredAt: 2000, DurationMS: 7000},
		{Screen: "D", EnteredAt: 9000, DurationMS: 0},
	})
}

func TestFlowShortEdgeStayKept(t *testing.T) {
	// first stay 500 ms, last stay 400 ms: both kept
	c := chunk(0, []Frame{f(0, "A"), f(500, "B"), f(5000, "C"), f(5400, "C")})
	assertFlow(t, ExtractFlow([]Chunk{c}), []View{
		{Screen: "A", EnteredAt: 0, DurationMS: 500},
		{Screen: "B", EnteredAt: 500, DurationMS: 4500},
		{Screen: "C", EnteredAt: 5000, DurationMS: 400},
	})
}

func TestFlowCapsDuration(t *testing.T) {
	c := chunk(0, []Frame{f(0, "A"), f((40 * time.Minute).Milliseconds(), "B")})
	got := ExtractFlow([]Chunk{c})
	assertFlow(t, got, []View{
		{Screen: "A", EnteredAt: 0, DurationMS: MaxStay.Milliseconds()},
		{Screen: "B", EnteredAt: (40 * time.Minute).Milliseconds(), DurationMS: 0},
	})
}

func TestFlowAcrossChunksOutOfOrder(t *testing.T) {
	one := ExtractFlow([]Chunk{chunk(0, []Frame{f(0, "A"), f(5000, "B"), f(10000, "A")}, td(12000, 1, 1))})
	split := ExtractFlow([]Chunk{
		chunk(1, []Frame{f(10000, "A")}, td(12000, 1, 1)),
		chunk(0, []Frame{f(0, "A"), f(5000, "B")}),
	})
	if len(one) != 3 || len(split) != len(one) {
		t.Fatalf("one=%+v split=%+v", one, split)
	}
	for i := range one {
		if one[i] != split[i] {
			t.Errorf("view %d: one=%+v split=%+v", i, one[i], split[i])
		}
	}
}
