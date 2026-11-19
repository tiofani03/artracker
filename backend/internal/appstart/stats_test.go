package appstart

import (
	"testing"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

// statsSym builds a Symbolicated directly: frames are named, stacks are root-first.
func statsSym(app map[string]bool, order []string, stacks [][]string, samples []ingest.ProfileSample, interval int) Symbolicated {
	idx := map[string]int{}
	var frames []ResolvedFrame
	for _, n := range order {
		idx[n] = len(frames)
		frames = append(frames, ResolvedFrame{Function: n, IsApp: app[n], Resolved: true})
	}
	var st [][]int
	for _, s := range stacks {
		var row []int
		for _, n := range s {
			row = append(row, idx[n])
		}
		st = append(st, row)
	}
	return Symbolicated{IntervalMs: interval, Frames: frames, Stacks: st, Samples: samples}
}

func statByName(t *testing.T, stats []FnStat, name string) FnStat {
	t.Helper()
	for _, s := range stats {
		if s.Function == name {
			return s
		}
	}
	t.Fatalf("no stat for %q in %+v", name, stats)
	return FnStat{}
}

func TestComputeFnStats(t *testing.T) {
	app := map[string]bool{"App.onCreate": true, "Koin.start": true}
	s := statsSym(app,
		[]string{"main", "App.onCreate", "Koin.start", "Reflect.get", "Prefs.load"},
		[][]string{
			{"main", "App.onCreate", "Koin.start", "Reflect.get"},
			{"main", "App.onCreate", "Prefs.load"},
		},
		[]ingest.ProfileSample{{T: 0, S: 0}, {T: 10, S: 0}, {T: 20, S: 1}, {T: 30, S: 1}}, 10)

	stats := ComputeFnStats(s)

	oc := statByName(t, stats, "App.onCreate")
	if oc.TotalMs != 40 || oc.SelfMs != 0 || oc.AttributedMs != 20 || !oc.IsApp {
		t.Errorf("App.onCreate = %+v, want total 40 self 0 attributed 20 app", oc)
	}
	ks := statByName(t, stats, "Koin.start")
	if ks.TotalMs != 20 || ks.AttributedMs != 20 || ks.SelfMs != 0 {
		t.Errorf("Koin.start = %+v, want total 20 attributed 20 self 0", ks)
	}
	if pl := statByName(t, stats, "Prefs.load"); pl.SelfMs != 20 || pl.AttributedMs != 0 || pl.IsApp {
		t.Errorf("Prefs.load = %+v, want self 20 attributed 0 non-app", pl)
	}
	if rg := statByName(t, stats, "Reflect.get"); rg.SelfMs != 20 || rg.TotalMs != 20 {
		t.Errorf("Reflect.get = %+v, want self 20 total 20", rg)
	}
	// main and App.onCreate tie at total 40; ties break by function name.
	if stats[0].Function != "App.onCreate" || stats[1].Function != "main" {
		t.Errorf("order = %s, %s; want App.onCreate, main (total desc, then name)", stats[0].Function, stats[1].Function)
	}
	for i := 1; i < len(stats); i++ {
		if stats[i-1].TotalMs < stats[i].TotalMs {
			t.Errorf("stats not sorted by total desc at %d: %+v", i, stats)
		}
	}
}

func TestComputeFnStatsCapsGap(t *testing.T) {
	s := statsSym(nil, []string{"leaf"}, [][]string{{"leaf"}},
		[]ingest.ProfileSample{{T: 0, S: 0}, {T: 500, S: 0}}, 10)
	leaf := statByName(t, ComputeFnStats(s), "leaf")
	if leaf.SelfMs != 110 { // 100 (capped gap) + 10 (last sample = interval)
		t.Fatalf("self = %d, want 110", leaf.SelfMs)
	}
}

func TestComputeFnStatsNonPositiveGapWeighsZero(t *testing.T) {
	s := statsSym(nil, []string{"leaf"}, [][]string{{"leaf"}},
		[]ingest.ProfileSample{{T: 50, S: 0}, {T: 50, S: 0}, {T: 40, S: 0}}, 10)
	if leaf := statByName(t, ComputeFnStats(s), "leaf"); leaf.SelfMs != 10 {
		t.Fatalf("self = %d, want 10 (only the last sample weighs)", leaf.SelfMs)
	}
}

func TestComputeFnStatsRecursionCountedOnce(t *testing.T) {
	s := statsSym(map[string]bool{"rec": true}, []string{"main", "rec"},
		[][]string{{"main", "rec", "rec", "rec"}},
		[]ingest.ProfileSample{{T: 0, S: 0}}, 10)
	rec := statByName(t, ComputeFnStats(s), "rec")
	if rec.TotalMs != 10 || rec.SelfMs != 10 || rec.AttributedMs != 10 {
		t.Fatalf("rec = %+v, want total/self/attributed 10 each", rec)
	}
}

func TestComputeFnStatsNoAppFrame(t *testing.T) {
	s := statsSym(nil, []string{"a", "b"}, [][]string{{"a", "b"}},
		[]ingest.ProfileSample{{T: 0, S: 0}}, 10)
	for _, st := range ComputeFnStats(s) {
		if st.AttributedMs != 0 {
			t.Fatalf("%+v attributed time with no app frame", st)
		}
	}
}
