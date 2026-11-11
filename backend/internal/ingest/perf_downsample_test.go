package ingest

import (
	"reflect"
	"testing"
)

func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }

func TestMergePerfSamplesDedupesByT(t *testing.T) {
	merged := MergePerfSamples([][]PerfSample{
		{{T: 1, CPU: fp(10)}, {T: 2}},
		{{T: 2, CPU: fp(99)}, {T: 3}},
	})
	if len(merged) != 3 {
		t.Fatalf("want 3 samples, got %d", len(merged))
	}
	if merged[1].T != 2 || merged[1].CPU != nil {
		t.Errorf("first occurrence of t=2 must win (no cpu), got %+v", merged[1])
	}
}

func TestMergePerfSamplesSortsAcrossBatches(t *testing.T) {
	merged := MergePerfSamples([][]PerfSample{{{T: 5}, {T: 6}}, {{T: 1}, {T: 2}}})
	var got []int64
	for _, s := range merged {
		got = append(got, s.T)
	}
	if !reflect.DeepEqual(got, []int64{1, 2, 5, 6}) {
		t.Fatalf("unsorted: %v", got)
	}
}

func TestDownsamplePerfKeepsSingleSpike(t *testing.T) {
	samples := make([]PerfSample, 2000)
	for i := range samples {
		samples[i] = PerfSample{T: int64(1000 + i*1000), CPU: fp(10)}
	}
	samples[1337].CPU = fp(99)

	out, downsampled := DownsamplePerf(samples, MaxPerfResponseSamples)
	if !downsampled || len(out) != MaxPerfResponseSamples {
		t.Fatalf("want %d downsampled samples, got %d (downsampled=%v)", MaxPerfResponseSamples, len(out), downsampled)
	}
	var max float64
	for _, s := range out {
		if s.CPU != nil && *s.CPU > max {
			max = *s.CPU
		}
	}
	if max != 99 {
		t.Fatalf("spike lost: max cpu = %v", max)
	}
}

func TestDownsamplePerfKeepsMinFPSAndWorstThermal(t *testing.T) {
	samples := []PerfSample{
		{T: 1, FPS: fp(60), Thermal: "nominal", Jank: ip(0)},
		{T: 2, FPS: fp(12), Thermal: "serious", Jank: ip(7)},
		{T: 3, FPS: fp(58), Thermal: "fair", Jank: ip(1)},
	}
	out, downsampled := DownsamplePerf(samples, 1)
	if !downsampled || len(out) != 1 {
		t.Fatalf("want 1 bucket, got %d (downsampled=%v)", len(out), downsampled)
	}
	got := out[0]
	if got.FPS == nil || *got.FPS != 12 {
		t.Errorf("fps should be the min, got %v", got.FPS)
	}
	if got.Thermal != "serious" {
		t.Errorf("thermal should be the worst, got %q", got.Thermal)
	}
	if got.Jank == nil || *got.Jank != 7 {
		t.Errorf("jank should be the max, got %v", got.Jank)
	}
	if got.T != 1 {
		t.Errorf("bucket T should be its first sample's T, got %d", got.T)
	}
}

func TestDownsamplePerfNoopUnderCap(t *testing.T) {
	samples := make([]PerfSample, 500)
	for i := range samples {
		samples[i] = PerfSample{T: int64(i + 1)}
	}
	out, downsampled := DownsamplePerf(samples, MaxPerfResponseSamples)
	if downsampled || len(out) != 500 {
		t.Fatalf("under cap must pass through, got %d downsampled=%v", len(out), downsampled)
	}
}

func TestDownsamplePerfLeavesAbsentMetricsAbsent(t *testing.T) {
	samples := []PerfSample{{T: 1, CPU: fp(5)}, {T: 2, CPU: fp(6)}, {T: 3, CPU: fp(7)}, {T: 4, CPU: fp(8)}}
	out, _ := DownsamplePerf(samples, 2)
	for _, s := range out {
		if s.FPS != nil || s.MemMB != nil || s.Thermal != "" {
			t.Fatalf("absent metrics must stay absent, got %+v", s)
		}
	}
}

func TestPresentPerfMetrics(t *testing.T) {
	got := PresentPerfMetrics([]PerfSample{{T: 1, FPS: fp(60)}, {T: 2, CPU: fp(1)}})
	if !reflect.DeepEqual(got, []string{"cpu", "fps"}) {
		t.Fatalf("got %v", got)
	}
	if got := PresentPerfMetrics(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty input must give non-nil empty slice, got %#v", got)
	}
}
