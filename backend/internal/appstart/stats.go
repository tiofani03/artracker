package appstart

import "sort"

// maxSampleGapMs caps the weight of one sample so a stall in sampling is not billed to one stack.
const maxSampleGapMs = 100

// FnStat is the time attributed to one function across a profile.
type FnStat struct {
	Function     string
	IsApp        bool
	SelfMs       int // time the function was the leaf
	TotalMs      int // time the function was anywhere on the stack (once per sample)
	AttributedMs int // time the function was the leaf-most app frame
}

// ComputeFnStats returns per-function stats sorted by TotalMs descending, then Function.
//
// Sample i weighs min(t[i+1]-t[i], 100) ms, the last sample weighs interval_ms, and a gap <= 0 weighs 0.
func ComputeFnStats(s Symbolicated) []FnStat {
	byFn := map[string]*FnStat{}
	stat := func(fr ResolvedFrame) *FnStat {
		st, ok := byFn[fr.Function]
		if !ok {
			st = &FnStat{Function: fr.Function, IsApp: fr.IsApp}
			byFn[fr.Function] = st
		}
		return st
	}

	for i, smp := range s.Samples {
		if smp.S < 0 || smp.S >= len(s.Stacks) {
			continue
		}
		stack := s.Stacks[smp.S]
		if len(stack) == 0 {
			continue
		}
		w := s.IntervalMs
		if i+1 < len(s.Samples) {
			w = int(min(s.Samples[i+1].T-smp.T, maxSampleGapMs))
			if w < 0 {
				w = 0
			}
		}

		stat(s.Frames[stack[len(stack)-1]]).SelfMs += w

		seen := make(map[string]struct{}, len(stack))
		attributed := false
		for pos := len(stack) - 1; pos >= 0; pos-- { // leaf to root
			fr := s.Frames[stack[pos]]
			st := stat(fr)
			if _, dup := seen[fr.Function]; !dup {
				seen[fr.Function] = struct{}{}
				st.TotalMs += w
			}
			if fr.IsApp && !attributed {
				attributed = true
				st.AttributedMs += w
			}
		}
	}

	out := make([]FnStat, 0, len(byFn))
	for _, st := range byFn {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalMs != out[j].TotalMs {
			return out[i].TotalMs > out[j].TotalMs
		}
		return out[i].Function < out[j].Function
	})
	return out
}
