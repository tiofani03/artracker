package ingest

import "sort"

// MaxPerfResponseSamples caps how many samples the read API returns for one session.
const MaxPerfResponseSamples = 1200

// netSeverity ranks connections from healthiest to worst so a downsampled bucket keeps its worst moment.
var netSeverity = map[string]int{"": 0, "ethernet": 1, "wifi": 2, "cellular": 3, "none": 4}

var thermalSeverity = map[string]int{"": 0, "nominal": 1, "fair": 2, "serious": 3, "critical": 4}

// MergePerfSamples flattens batches into one series sorted by T. SDK retries can deliver the same instant
// twice; the first occurrence (in batch order) wins.
func MergePerfSamples(batches [][]PerfSample) []PerfSample {
	total := 0
	for _, b := range batches {
		total += len(b)
	}
	all := make([]PerfSample, 0, total)
	for _, b := range batches {
		all = append(all, b...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].T < all[j].T })

	out := make([]PerfSample, 0, len(all))
	for _, s := range all {
		if n := len(out); n > 0 && out[n-1].T == s.T {
			continue
		}
		out = append(out, s)
	}
	return out
}

// DownsamplePerf reduces s to at most maxPoints samples. Each output sample summarizes a contiguous bucket
// and keeps the WORST value of every metric (max cpu/mem/jank/frozen/threads, min fps/battery/free ram/free storage, most severe
// thermal and connection), so a single spike is never averaged away. The bool reports whether anything was reduced.
func DownsamplePerf(s []PerfSample, maxPoints int) ([]PerfSample, bool) {
	n := len(s)
	if maxPoints <= 0 || n <= maxPoints {
		return s, false
	}
	out := make([]PerfSample, 0, maxPoints)
	for i := 0; i < maxPoints; i++ {
		lo, hi := i*n/maxPoints, (i+1)*n/maxPoints
		if hi <= lo {
			continue
		}
		out = append(out, worstOf(s[lo:hi]))
	}
	return out, true
}

func worstOf(bucket []PerfSample) PerfSample {
	w := PerfSample{T: bucket[0].T}
	for _, s := range bucket {
		w.CPU = pickFloat(w.CPU, s.CPU, true)
		w.MemMB = pickFloat(w.MemMB, s.MemMB, true)
		w.FPS = pickFloat(w.FPS, s.FPS, false)
		w.Battery = pickFloat(w.Battery, s.Battery, false)
		w.RAMFreeMB = pickFloat(w.RAMFreeMB, s.RAMFreeMB, false)
		w.StorageFreeMB = pickFloat(w.StorageFreeMB, s.StorageFreeMB, false)
		if netSeverity[s.Net] > netSeverity[w.Net] {
			w.Net = s.Net
		}
		w.Jank = pickInt(w.Jank, s.Jank)
		w.Frozen = pickInt(w.Frozen, s.Frozen)
		w.Threads = pickInt(w.Threads, s.Threads)
		if thermalSeverity[s.Thermal] > thermalSeverity[w.Thermal] {
			w.Thermal = s.Thermal
		}
	}
	return w
}

func pickFloat(cur, next *float64, wantMax bool) *float64 {
	if next == nil {
		return cur
	}
	if cur == nil || (wantMax && *next > *cur) || (!wantMax && *next < *cur) {
		v := *next
		return &v
	}
	return cur
}

func pickInt(cur, next *int) *int {
	if next == nil {
		return cur
	}
	if cur == nil || *next > *cur {
		v := *next
		return &v
	}
	return cur
}

// PresentPerfMetrics lists, in stable order, the metric keys that at least one sample carries.
func PresentPerfMetrics(s []PerfSample) []string {
	var cpu, mem, fps, jank, frozen, threads, battery, thermal, ramFree, storageFree, net bool
	for _, x := range s {
		cpu = cpu || x.CPU != nil
		mem = mem || x.MemMB != nil
		fps = fps || x.FPS != nil
		jank = jank || x.Jank != nil
		frozen = frozen || x.Frozen != nil
		threads = threads || x.Threads != nil
		battery = battery || x.Battery != nil
		thermal = thermal || x.Thermal != ""
		ramFree = ramFree || x.RAMFreeMB != nil
		storageFree = storageFree || x.StorageFreeMB != nil
		net = net || x.Net != ""
	}
	out := []string{}
	for _, m := range []struct {
		key string
		ok  bool
	}{{"cpu", cpu}, {"mem_mb", mem}, {"fps", fps}, {"jank", jank}, {"frozen", frozen}, {"threads", threads}, {"battery", battery}, {"thermal", thermal}, {"ram_free_mb", ramFree}, {"storage_free_mb", storageFree}, {"net", net}} {
		if m.ok {
			out = append(out, m.key)
		}
	}
	return out
}

// EffectivePerfInterval is the spacing, in ms, between points of a series of n samples (originally intervalMs
// apart) after DownsamplePerf(…, maxPoints): each point then stands for up to ceil(n/maxPoints) samples. Clients
// must use it instead of the SDK's interval, or runs and "current value" lookups see gaps that are not there.
func EffectivePerfInterval(intervalMs, n, maxPoints int) int {
	if maxPoints <= 0 || n <= maxPoints {
		return intervalMs
	}
	return intervalMs * ((n + maxPoints - 1) / maxPoints)
}
