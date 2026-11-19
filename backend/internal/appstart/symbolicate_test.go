package appstart

import (
	"reflect"
	"testing"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

// fakeResolver resolves a frame to a function named after its C field and records every call.
type fakeResolver struct {
	expand map[string][]ResolvedFrame // keyed by frame C; overrides the default single frame
	calls  []fakeCall
}

type fakeCall struct {
	c      string
	caller bool
}

func (f *fakeResolver) Resolve(fr ingest.ProfileFrame, caller bool) []ResolvedFrame {
	f.calls = append(f.calls, fakeCall{fr.C, caller})
	if out, ok := f.expand[fr.C]; ok {
		return out
	}
	return []ResolvedFrame{{Function: fr.C, Resolved: true}}
}

func (f *fakeResolver) MissingSymbols() bool { return false }

func TestSymbolicateExpandsInline(t *testing.T) {
	p := ingest.AppStartProfile{
		IntervalMs: 10,
		Frames:     []ingest.ProfileFrame{{C: "root"}, {C: "obf"}},
		Stacks:     [][]int{{0, 1}},
		Samples:    []ingest.ProfileSample{{T: 0, S: 0}},
	}
	r := &fakeResolver{expand: map[string][]ResolvedFrame{
		// innermost first, as Resolve returns them
		"obf": {{Function: "inlined", Resolved: true}, {Function: "outer", Resolved: true}},
	}}

	got := Symbolicate(p, r)

	if len(got.Stacks) != 1 || len(got.Stacks[0]) != 3 {
		t.Fatalf("stacks = %v, want one stack of depth 3", got.Stacks)
	}
	var names []string
	for _, idx := range got.Stacks[0] {
		names = append(names, got.Frames[idx].Function)
	}
	if want := []string{"root", "outer", "inlined"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("stack = %v, want root-first %v", names, want)
	}
	if len(got.Frames) != 3 {
		t.Fatalf("frames = %d, want 3 deduplicated frames", len(got.Frames))
	}
	if got.IntervalMs != 10 || !reflect.DeepEqual(got.Samples, p.Samples) {
		t.Fatalf("interval/samples not carried over: %+v", got)
	}
}

func TestSymbolicateDeduplicatesFrames(t *testing.T) {
	p := ingest.AppStartProfile{
		Frames:  []ingest.ProfileFrame{{C: "same"}, {C: "same"}, {C: "other"}},
		Stacks:  [][]int{{0, 1, 2}},
		Samples: []ingest.ProfileSample{{T: 0, S: 0}},
	}
	got := Symbolicate(p, &fakeResolver{})
	if len(got.Frames) != 2 {
		t.Fatalf("frames = %d, want 2 (identical resolved frames merged)", len(got.Frames))
	}
	if st := got.Stacks[0]; st[0] != st[1] || st[0] == st[2] {
		t.Fatalf("stack = %v, want first two entries to share an index", st)
	}
}

func TestSymbolicateCallerFlag(t *testing.T) {
	p := ingest.AppStartProfile{
		Frames:  []ingest.ProfileFrame{{C: "f0"}, {C: "f1"}, {C: "f2"}},
		Stacks:  [][]int{{0, 1}, {0, 1, 2}},
		Samples: []ingest.ProfileSample{{T: 0, S: 0}, {T: 10, S: 1}},
	}
	r := &fakeResolver{}
	Symbolicate(p, r)

	var f1 []bool
	counts := map[fakeCall]int{}
	for _, c := range r.calls {
		counts[c]++
		if c.c == "f1" {
			f1 = append(f1, c.caller)
		}
	}
	if want := []bool{false, true}; !reflect.DeepEqual(f1, want) {
		t.Fatalf("f1 calls (caller flag) = %v, want %v", f1, want)
	}
	for c, n := range counts {
		if n != 1 {
			t.Errorf("%+v resolved %d times, want once per unique (frame, caller)", c, n)
		}
	}
}
