package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func perfItem(sessionID string, attrs string) RawItem {
	return RawItem{
		Type:       PerfItemType,
		Name:       "perf.samples",
		SessionID:  sessionID,
		Attributes: json.RawMessage(attrs),
	}
}

func TestParsePerfItemValid(t *testing.T) {
	item := perfItem("ses_a", `{
		"v":1,"platform":"ios","interval_ms":1000,
		"device":{"ram_mb":3000,"cores":6,"refresh_hz":120},
		"samples":[{"t":3000,"cpu":30},{"t":1000,"cpu":10},{"t":2000,"cpu":20}]
	}`)
	row, err := ParsePerfItem(7, time.UnixMilli(9000), item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(row.Samples) != 3 {
		t.Fatalf("want 3 samples, got %d", len(row.Samples))
	}
	for i, want := range []int64{1000, 2000, 3000} {
		if row.Samples[i].T != want {
			t.Errorf("sample %d: T=%d want %d", i, row.Samples[i].T, want)
		}
	}
	if !row.StartTS.Equal(time.UnixMilli(1000)) || !row.EndTS.Equal(time.UnixMilli(3000)) {
		t.Errorf("bad range: %v..%v", row.StartTS, row.EndTS)
	}
	if row.IntervalMs != 1000 || row.Device.Cores != 6 || row.ProjectID != 7 || row.SessionID != "ses_a" || row.Platform != "ios" {
		t.Errorf("unexpected row: %+v", row)
	}
}

func TestParsePerfItemClamps(t *testing.T) {
	item := perfItem("ses_a", `{"v":1,"interval_ms":1000,"samples":[{"t":1000,"cpu":140,"fps":-3,"battery":1.7,"thermal":"hot"}]}`)
	row, err := ParsePerfItem(1, time.Now(), item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := row.Samples[0]
	if s.CPU == nil || *s.CPU != 100 {
		t.Errorf("cpu not clamped to 100: %v", s.CPU)
	}
	if s.FPS == nil || *s.FPS != 0 {
		t.Errorf("fps not clamped to 0: %v", s.FPS)
	}
	if s.Battery == nil || *s.Battery != 1 {
		t.Errorf("battery not clamped to 1: %v", s.Battery)
	}
	if s.Thermal != "" {
		t.Errorf("unknown thermal should be dropped, got %q", s.Thermal)
	}
}

func TestParsePerfItemRejects(t *testing.T) {
	many := make([]string, MaxPerfSamples+1)
	for i := range many {
		many[i] = fmt.Sprintf(`{"t":%d}`, 1000+i)
	}
	cases := map[string]RawItem{
		"wrong version":     perfItem("s", `{"v":2,"interval_ms":1000,"samples":[{"t":1}]}`),
		"empty session":     perfItem("", `{"v":1,"interval_ms":1000,"samples":[{"t":1}]}`),
		"zero samples":      perfItem("s", `{"v":1,"interval_ms":1000,"samples":[]}`),
		"too many samples":  perfItem("s", `{"v":1,"interval_ms":1000,"samples":[`+strings.Join(many, ",")+`]}`),
		"all t<=0":          perfItem("s", `{"v":1,"interval_ms":1000,"samples":[{"t":0},{"t":-5}]}`),
		"interval too low":  perfItem("s", `{"v":1,"interval_ms":100,"samples":[{"t":1}]}`),
		"interval too high": perfItem("s", `{"v":1,"interval_ms":20000,"samples":[{"t":1}]}`),
		"not json":          perfItem("s", `not-json`),
	}
	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePerfItem(1, time.Now(), item)
			if !errors.Is(err, ErrInvalidPerf) {
				t.Fatalf("want ErrInvalidPerf, got %v", err)
			}
		})
	}
}

func TestParsePerfItemDropsUnknownKeys(t *testing.T) {
	item := perfItem("s", `{"v":1,"interval_ms":1000,"samples":[{"t":1000,"cpu":5,"foo":"bar"}]}`)
	row, err := ParsePerfItem(1, time.Now(), item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := json.Marshal(row.Samples)
	if strings.Contains(string(b), "foo") {
		t.Errorf("unknown key leaked: %s", b)
	}
}
