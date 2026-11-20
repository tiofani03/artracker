package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

type startSeed struct {
	startID  string
	typ      string
	platform string
	env      string
	version  string
	duration int
	ramMB    *int
	at       time.Time
	phases   string
	profiled bool
}

func intp(v int) *int { return &v }

func (e *testEnv) addAppStart(t *testing.T, pid int64, s startSeed) {
	t.Helper()
	if s.typ == "" {
		s.typ = "cold"
	}
	if s.platform == "" {
		s.platform = "android"
	}
	if s.env == "" {
		s.env = "production"
	}
	if s.version == "" {
		s.version = "1.0.0"
	}
	if s.at.IsZero() {
		s.at = time.Now().Add(-time.Hour)
	}
	if s.phases == "" {
		s.phases = "[]"
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO app_starts(project_id, start_id, platform, environment, type, duration_ms, phases, app_version, build, ram_mb, profiled, started_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,'1203',$9,$10,$11)`,
		pid, s.startID, s.platform, s.env, s.typ, s.duration, s.phases, s.version, s.ramMB, s.profiled, s.at); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) addFnStat(t *testing.T, pid int64, startID, fn string, isApp bool, self, total, attributed int) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO app_start_fn_stats(project_id, start_id, platform, environment, app_version, function, is_app, self_ms, total_ms, attributed_ms, started_at)
		 VALUES ($1,$2,'android','production','1.0.0',$3,$4,$5,$6,$7, now() - interval '1 hour')`,
		pid, startID, fn, isApp, self, total, attributed); err != nil {
		t.Fatal(err)
	}
}

type startsEnv struct {
	e   *testEnv
	pid int64
	hdr map[string]string
}

func newStartsEnv(t *testing.T, slug string) startsEnv {
	t.Helper()
	e := newTestEnv(t)
	org := e.addOrg(t, slug)
	uid, hdr := e.addUser(t, slug+"viewer")
	e.addMember(t, org, uid, "viewer")
	return startsEnv{e: e, pid: e.addProject(t, org, slug+"-app"), hdr: hdr}
}

func (s startsEnv) path(suffix string) string {
	return fmt.Sprintf("/api/v1/projects/%d/app-starts%s", s.pid, suffix)
}

type summaryResp struct {
	Environment  string   `json:"environment"`
	Environments []string `json:"environments"`
	Versions     []string `json:"versions"`
	Count        int      `json:"count"`
	P50          int      `json:"p50"`
	P75          int      `json:"p75"`
	P90          int      `json:"p90"`
	P95          int      `json:"p95"`
	ProfiledPct  float64  `json:"profiled_pct"`
	Phases       []struct {
		Name     string `json:"name"`
		MedianMs int    `json:"median_ms"`
	} `json:"phases"`
	Daily []struct {
		Day   string `json:"day"`
		P50   int    `json:"p50"`
		P90   int    `json:"p90"`
		Count int    `json:"count"`
	} `json:"daily"`
	ByVersion []struct {
		Version string `json:"version"`
		P50     int    `json:"p50"`
		P90     int    `json:"p90"`
		Count   int    `json:"count"`
	} `json:"by_version"`
}

func near(got, want, tol int) bool { return got >= want-tol && got <= want+tol }

func TestAppStartSummaryPercentiles(t *testing.T) {
	s := newStartsEnv(t, "as-pct")
	for i := 1; i <= 100; i++ {
		s.e.addAppStart(t, s.pid, startSeed{startID: fmt.Sprintf("c%d", i), duration: i, profiled: i <= 25})
	}
	s.e.addAppStart(t, s.pid, startSeed{startID: "w1", typ: "warm", duration: 9999})

	var got summaryResp
	if st := hmGet(t, s.e, s.hdr, s.path("/summary"), &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	if got.Count != 100 {
		t.Errorf("count = %d, want 100 (warm excluded)", got.Count)
	}
	if !near(got.P50, 50, 1) || !near(got.P90, 90, 1) || !near(got.P75, 75, 1) || !near(got.P95, 95, 1) {
		t.Errorf("percentiles p50=%d p75=%d p90=%d p95=%d", got.P50, got.P75, got.P90, got.P95)
	}
	if got.ProfiledPct != 25 {
		t.Errorf("profiled_pct = %v, want 25", got.ProfiledPct)
	}
	if len(got.Daily) != 1 || got.Daily[0].Count != 100 || len(got.ByVersion) != 1 || got.ByVersion[0].Version != "1.0.0" || got.ByVersion[0].Count != 100 {
		t.Errorf("daily=%+v by_version=%+v", got.Daily, got.ByVersion)
	}

	var warm summaryResp
	hmGet(t, s.e, s.hdr, s.path("/summary?type=warm"), &warm)
	if warm.Count != 1 || warm.P50 != 9999 {
		t.Errorf("warm summary = %+v", warm)
	}
	if st := hmGet(t, s.e, s.hdr, s.path("/summary?type=bogus"), nil); st != 400 {
		t.Errorf("bad type = %d, want 400", st)
	}
	if st := hmGet(t, s.e, s.hdr, s.path("/summary?ram_tier=huge"), nil); st != 400 {
		t.Errorf("bad ram_tier = %d, want 400", st)
	}
}

func TestAppStartSummaryEmpty(t *testing.T) {
	s := newStartsEnv(t, "as-empty")
	var got summaryResp
	if st := hmGet(t, s.e, s.hdr, s.path("/summary"), &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	if got.Count != 0 || got.P50 != 0 || got.Phases == nil || got.Daily == nil || got.ByVersion == nil || got.Environments == nil || got.Versions == nil {
		t.Errorf("empty summary must have zero values and non-nil lists: %+v", got)
	}
}

func TestAppStartSummaryPhasesOrdered(t *testing.T) {
	s := newStartsEnv(t, "as-phases")
	for i := 1; i <= 3; i++ {
		s.e.addAppStart(t, s.pid, startSeed{
			startID: fmt.Sprintf("p%d", i), duration: 1000,
			phases: fmt.Sprintf(`[{"name":"process_start","ms":%d},{"name":"application","ms":%d},{"name":"first_frame","ms":%d}]`, i*10, i*100, i*1000),
		})
	}
	var got summaryResp
	hmGet(t, s.e, s.hdr, s.path("/summary"), &got)
	want := []struct {
		name string
		ms   int
	}{{"process_start", 20}, {"application", 200}, {"first_frame", 2000}}
	if len(got.Phases) != len(want) {
		t.Fatalf("phases = %+v", got.Phases)
	}
	for i, w := range want {
		if got.Phases[i].Name != w.name || got.Phases[i].MedianMs != w.ms {
			t.Errorf("phase %d = %+v, want %+v", i, got.Phases[i], w)
		}
	}
}

func TestAppStartRamTier(t *testing.T) {
	s := newStartsEnv(t, "as-ram")
	s.e.addAppStart(t, s.pid, startSeed{startID: "low", duration: 100, ramMB: intp(3000)})
	s.e.addAppStart(t, s.pid, startSeed{startID: "mid", duration: 200, ramMB: intp(6000)})
	s.e.addAppStart(t, s.pid, startSeed{startID: "high", duration: 300, ramMB: intp(12000)})
	s.e.addAppStart(t, s.pid, startSeed{startID: "none", duration: 400})
	for tier, want := range map[string]int{"low": 1, "mid": 1, "high": 1, "": 4} {
		var got summaryResp
		hmGet(t, s.e, s.hdr, s.path("/summary?ram_tier="+tier), &got)
		if got.Count != want {
			t.Errorf("ram_tier=%q count = %d, want %d", tier, got.Count, want)
		}
	}
	var list appStartListResp
	hmGet(t, s.e, s.hdr, s.path("?ram_tier=mid"), &list)
	if len(list.Starts) != 1 || list.Starts[0].StartID != "mid" || list.Starts[0].Build != "1203" {
		t.Errorf("list mid = %+v", list.Starts)
	}
}

func TestAppStartEnvironmentDefault(t *testing.T) {
	s := newStartsEnv(t, "as-env")
	s.e.addAppStart(t, s.pid, startSeed{startID: "s1", env: "staging", duration: 100})
	s.e.addAppStart(t, s.pid, startSeed{startID: "s2", env: "staging", duration: 200})

	var got summaryResp
	hmGet(t, s.e, s.hdr, s.path("/summary"), &got)
	if got.Environment != "" || got.Count != 2 || len(got.Environments) != 1 || got.Environments[0] != "staging" {
		t.Fatalf("staging only: %+v", got)
	}

	s.e.addAppStart(t, s.pid, startSeed{startID: "p1", env: "production", duration: 300})
	got = summaryResp{}
	hmGet(t, s.e, s.hdr, s.path("/summary"), &got)
	if got.Environment != "production" || got.Count != 1 || len(got.Environments) != 2 {
		t.Fatalf("production default: %+v", got)
	}
	got = summaryResp{}
	hmGet(t, s.e, s.hdr, s.path("/summary?environment=staging"), &got)
	if got.Environment != "staging" || got.Count != 2 {
		t.Fatalf("explicit staging: %+v", got)
	}
}

func TestAppStartEnvironmentAll(t *testing.T) {
	s := newStartsEnv(t, "as-envall")
	s.e.addAppStart(t, s.pid, startSeed{startID: "s1", env: "staging", version: "1.0.0", duration: 100})
	s.e.addAppStart(t, s.pid, startSeed{startID: "p1", env: "production", version: "2.0.0", duration: 300})
	s.e.addAppStart(t, s.pid, startSeed{startID: "p2", env: "production", version: "2.0.0", duration: 300})

	var got summaryResp
	hmGet(t, s.e, s.hdr, s.path("/summary?environment=all"), &got)
	if got.Environment != "" || got.Count != 3 || len(got.Environments) != 2 || len(got.Versions) != 2 {
		t.Fatalf("environment=all: %+v", got)
	}
	// Without the parameter production is still the default.
	got = summaryResp{}
	hmGet(t, s.e, s.hdr, s.path("/summary"), &got)
	if got.Environment != "production" || got.Count != 2 {
		t.Fatalf("default: %+v", got)
	}
}

func TestAppStartByVersionLatest10(t *testing.T) {
	s := newStartsEnv(t, "as-ver")
	for i := 1; i <= 12; i++ {
		s.e.addAppStart(t, s.pid, startSeed{
			startID: fmt.Sprintf("v%d", i), version: fmt.Sprintf("1.%d.0", i), duration: 100,
			at: time.Now().Add(-time.Duration(13-i) * time.Hour),
		})
	}
	var got summaryResp
	hmGet(t, s.e, s.hdr, s.path("/summary"), &got)
	if len(got.ByVersion) != 10 || got.ByVersion[0].Version != "1.12.0" {
		t.Errorf("by_version = %+v", got.ByVersion)
	}
	if len(got.Versions) != 12 || got.Versions[0] != "1.12.0" || got.Versions[1] != "1.11.0" || got.Versions[11] != "1.1.0" {
		t.Errorf("versions must be newest first by recency: %v", got.Versions)
	}
}

type fnResp struct {
	Profiles  int `json:"profiles"`
	Functions []struct {
		Function           string  `json:"function"`
		IsApp              bool    `json:"is_app"`
		StartsPct          float64 `json:"starts_pct"`
		MedianTotalMs      int     `json:"median_total_ms"`
		P90TotalMs         int     `json:"p90_total_ms"`
		MedianSelfMs       int     `json:"median_self_ms"`
		MedianAttributedMs int     `json:"median_attributed_ms"`
		ExampleStartID     string  `json:"example_start_id"`
	} `json:"functions"`
}

func TestAppStartFunctionsAppOnlyRanking(t *testing.T) {
	s := newStartsEnv(t, "as-fn")
	for _, id := range []string{"a", "b"} {
		s.e.addAppStart(t, s.pid, startSeed{startID: id, duration: 100})
	}
	// app fn "app.Heavy": attributed 300, "app.Light": 50, framework "fw.Spin": self 900 (not app).
	s.e.addFnStat(t, s.pid, "a", "app.Heavy", true, 10, 400, 300)
	s.e.addFnStat(t, s.pid, "b", "app.Heavy", true, 20, 200, 100)
	s.e.addFnStat(t, s.pid, "a", "app.Light", true, 5, 60, 50)
	s.e.addFnStat(t, s.pid, "a", "fw.Spin", false, 900, 900, 0)

	var got fnResp
	if st := hmGet(t, s.e, s.hdr, s.path("/functions"), &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	if got.Profiles != 2 || len(got.Functions) != 2 || got.Functions[0].Function != "app.Heavy" || got.Functions[1].Function != "app.Light" {
		t.Fatalf("app_only: %+v", got)
	}
	h := got.Functions[0]
	if !h.IsApp || h.StartsPct != 100 || h.MedianTotalMs != 300 || h.MedianSelfMs != 15 || h.MedianAttributedMs != 200 || h.ExampleStartID != "a" || h.P90TotalMs < 300 {
		t.Errorf("heavy = %+v", h)
	}
	if got.Functions[1].StartsPct != 50 {
		t.Errorf("light starts_pct = %v", got.Functions[1].StartsPct)
	}

	got = fnResp{}
	hmGet(t, s.e, s.hdr, s.path("/functions?app_only=false"), &got)
	if len(got.Functions) != 3 || got.Functions[0].Function != "fw.Spin" {
		t.Errorf("all functions ranked by self: %+v", got.Functions)
	}
}

type appStartListResp struct {
	Starts []struct {
		StartID    string `json:"start_id"`
		Type       string `json:"type"`
		Platform   string `json:"platform"`
		AppVersion string `json:"app_version"`
		Build      string `json:"build"`
		DurationMs int    `json:"duration_ms"`
		Profiled   bool   `json:"profiled"`
		StartedAt  string `json:"started_at"`
	} `json:"starts"`
	NextCursor string `json:"next_cursor"`
}

func TestListAppStartsCursor(t *testing.T) {
	s := newStartsEnv(t, "as-list")
	for i := 1; i <= 5; i++ {
		s.e.addAppStart(t, s.pid, startSeed{
			startID: fmt.Sprintf("s%d", i), duration: i * 100,
			at: time.Now().Add(-time.Duration(6-i) * time.Hour),
		})
	}
	// A duplicate duration exercises the id tie-break.
	s.e.addAppStart(t, s.pid, startSeed{startID: "s6", duration: 300, at: time.Now().Add(-30 * time.Minute)})

	// limit 3 puts the tied duration (s6, s3 at 300 ms) across the first page boundary for "slowest".
	for _, limit := range []int{2, 3} {
		for _, tc := range []struct {
			sort string
			want []string
		}{
			{"slowest", []string{"s5", "s4", "s6", "s3", "s2", "s1"}},
			{"recent", []string{"s6", "s5", "s4", "s3", "s2", "s1"}},
		} {
			var seen []string
			cursor := ""
			for page := 0; page < 10; page++ {
				var got appStartListResp
				url := s.path(fmt.Sprintf("?limit=%d&sort=%s", limit, tc.sort))
				if cursor != "" {
					url += "&cursor=" + cursor
				}
				if st := hmGet(t, s.e, s.hdr, url, &got); st != 200 {
					t.Fatalf("%s limit %d page %d status %d", tc.sort, limit, page, st)
				}
				for _, r := range got.Starts {
					seen = append(seen, r.StartID)
				}
				if got.NextCursor == "" {
					break
				}
				if len(got.Starts) != limit {
					t.Fatalf("%s limit %d: page with cursor has %d rows", tc.sort, limit, len(got.Starts))
				}
				cursor = got.NextCursor
			}
			if fmt.Sprint(seen) != fmt.Sprint(tc.want) {
				t.Errorf("%s limit %d order = %v, want %v", tc.sort, limit, seen, tc.want)
			}
		}
	}
	if st := hmGet(t, s.e, s.hdr, s.path("?cursor=!!!"), nil); st != 400 {
		t.Errorf("bad cursor = %d, want 400", st)
	}
	if st := hmGet(t, s.e, s.hdr, s.path("?sort=bogus"), nil); st != 400 {
		t.Errorf("bad sort = %d, want 400", st)
	}
}

func TestListAppStartsAllTypes(t *testing.T) {
	s := newStartsEnv(t, "as-alltypes")
	s.e.addAppStart(t, s.pid, startSeed{startID: "c1", typ: "cold", duration: 900})
	s.e.addAppStart(t, s.pid, startSeed{startID: "w1", typ: "warm", duration: 400})
	s.e.addAppStart(t, s.pid, startSeed{startID: "h1", typ: "hot", duration: 600})

	list := func(query string) []string {
		t.Helper()
		var got appStartListResp
		if st := hmGet(t, s.e, s.hdr, s.path(query), &got); st != 200 {
			t.Fatalf("%s status %d", query, st)
		}
		var out []string
		for _, r := range got.Starts {
			out = append(out, r.StartID+":"+r.Type)
		}
		return out
	}
	if got := fmt.Sprint(list("?type=all")); got != "[c1:cold h1:hot w1:warm]" {
		t.Errorf("type=all slowest = %s", got)
	}
	if got := fmt.Sprint(list("")); got != "[c1:cold]" {
		t.Errorf("default type = %s, want cold only", got)
	}
	if got := fmt.Sprint(list("?type=hot")); got != "[h1:hot]" {
		t.Errorf("type=hot = %s", got)
	}
	if st := hmGet(t, s.e, s.hdr, s.path("?type=bogus"), nil); st != 400 {
		t.Errorf("list bad type = %d, want 400", st)
	}
	// Percentiles never mix start types: the summary rejects "all".
	if st := hmGet(t, s.e, s.hdr, s.path("/summary?type=all"), nil); st != 400 {
		t.Errorf("summary type=all = %d, want 400", st)
	}
}

func TestGetAppStartDetail(t *testing.T) {
	s := newStartsEnv(t, "as-detail")
	s.e.addAppStart(t, s.pid, startSeed{startID: "d1", duration: 700, ramMB: intp(4096), profiled: true,
		phases: `[{"name":"application","ms":300}]`})
	if _, err := s.e.pool.Exec(context.Background(),
		`INSERT INTO app_start_profiles(project_id, start_id, platform, raw, status, symbolicated)
		 VALUES ($1,'d1','android','\x00','done',$2::jsonb)`, s.pid,
		`{"interval_ms":10,"frames":[{"function":"a.B.c","is_app":true,"resolved":true}],"stacks":[[0]],"samples":[{"t":10,"s":0}]}`); err != nil {
		t.Fatal(err)
	}
	var got struct {
		StartID    string `json:"start_id"`
		Build      string `json:"build"`
		DurationMs int    `json:"duration_ms"`
		Phases     []struct {
			Name string `json:"name"`
			Ms   int    `json:"ms"`
		} `json:"phases"`
		RAMMB   *int `json:"ram_mb"`
		Profile *struct {
			Status string `json:"status"`
			Data   *struct {
				IntervalMs int `json:"interval_ms"`
				Frames     []struct {
					Function string `json:"function"`
				} `json:"frames"`
			} `json:"data"`
		} `json:"profile"`
	}
	if st := hmGet(t, s.e, s.hdr, s.path("/d1"), &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	if got.StartID != "d1" || got.Build != "1203" || got.DurationMs != 700 || len(got.Phases) != 1 || got.Phases[0].Ms != 300 || got.RAMMB == nil || *got.RAMMB != 4096 {
		t.Errorf("detail = %+v", got)
	}
	if got.Profile == nil || got.Profile.Status != "done" || got.Profile.Data == nil || got.Profile.Data.IntervalMs != 10 || got.Profile.Data.Frames[0].Function != "a.B.c" {
		t.Errorf("profile = %+v", got.Profile)
	}

	// A row without a profile reports profile: null.
	s.e.addAppStart(t, s.pid, startSeed{startID: "d2", duration: 10})
	resp := s.e.do(t, "GET", s.path("/d2"), nil, s.hdr)
	var raw map[string]json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	if string(raw["profile"]) != "null" {
		t.Errorf("profile = %s, want null", raw["profile"])
	}
}

func TestGetAppStartProfileOnly(t *testing.T) {
	s := newStartsEnv(t, "as-ponly")
	if _, err := s.e.pool.Exec(context.Background(),
		`INSERT INTO app_start_profiles(project_id, start_id, platform, raw, status) VALUES ($1,'orphan','android','\x00','pending')`, s.pid); err != nil {
		t.Fatal(err)
	}
	resp := s.e.do(t, "GET", s.path("/orphan"), nil, s.hdr)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got struct {
		StartID    string `json:"start_id"`
		Type       string `json:"type"`
		DurationMs int    `json:"duration_ms"`
		Phases     []any  `json:"phases"`
		Profile    *struct {
			Status string          `json:"status"`
			Data   json.RawMessage `json:"data"`
		} `json:"profile"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.StartID != "orphan" || got.Type != "" || got.DurationMs != 0 || got.Phases == nil || got.Profile == nil || got.Profile.Status != "pending" || string(got.Profile.Data) != "null" {
		t.Errorf("profile only = %+v", got)
	}
}

func TestGetAppStart404(t *testing.T) {
	s := newStartsEnv(t, "as-404")
	if st := hmGet(t, s.e, s.hdr, s.path("/nope"), nil); st != 404 {
		t.Errorf("status = %d, want 404", st)
	}
}

func TestAppStartRequiresProjectRole(t *testing.T) {
	s := newStartsEnv(t, "as-role")
	s.e.addAppStart(t, s.pid, startSeed{startID: "x", duration: 1})
	otherOrg := s.e.addOrg(t, "as-role-other")
	ouid, ohdr := s.e.addUser(t, "asoutsider")
	s.e.addMember(t, otherOrg, ouid, "viewer")
	for _, suffix := range []string{"/summary", "/functions", "", "/x"} {
		// RequireProjectRole answers 404 for a project the caller has no membership in.
		if st := hmGet(t, s.e, ohdr, s.path(suffix), nil); st != 403 && st != 404 {
			t.Errorf("cross-org GET app-starts%s = %d, want 403/404", suffix, st)
		}
		if st := hmGet(t, s.e, map[string]string{}, s.path(suffix), nil); st != 401 {
			t.Errorf("anonymous GET app-starts%s = %d, want 401", suffix, st)
		}
	}
}

func TestAppStartSummaryByType(t *testing.T) {
	s := newStartsEnv(t, "as-bytype")
	for i := 1; i <= 4; i++ {
		s.e.addAppStart(t, s.pid, startSeed{startID: fmt.Sprintf("w%d", i), typ: "warm", duration: i * 100, profiled: false})
	}
	s.e.addAppStart(t, s.pid, startSeed{startID: "h1", typ: "hot", duration: 50})
	s.e.addAppStart(t, s.pid, startSeed{startID: "h2", typ: "hot", duration: 70, version: "2.0.0"})

	type byType struct {
		Type  string `json:"type"`
		Count int    `json:"count"`
		P50   int    `json:"p50"`
		P90   int    `json:"p90"`
	}
	var got struct {
		Count  int      `json:"count"`
		ByType []byType `json:"by_type"`
	}
	// cold (the default) has no starts, yet every type is listed so the page can show the others
	if st := hmGet(t, s.e, s.hdr, s.path("/summary"), &got); st != 200 {
		t.Fatalf("status %d", st)
	}
	want := []byType{{"cold", 0, 0, 0}, {"warm", 4, 250, 370}, {"hot", 2, 60, 68}}
	if got.Count != 0 || fmt.Sprint(got.ByType) != fmt.Sprint(want) {
		t.Fatalf("count=%d by_type=%+v, want 0 and %+v", got.Count, got.ByType, want)
	}
	// other filters still apply
	got.ByType = nil
	hmGet(t, s.e, s.hdr, s.path("/summary?type=hot&app_version=2.0.0"), &got)
	want = []byType{{"cold", 0, 0, 0}, {"warm", 0, 0, 0}, {"hot", 1, 70, 70}}
	if got.Count != 1 || fmt.Sprint(got.ByType) != fmt.Sprint(want) {
		t.Fatalf("version filter: count=%d by_type=%+v, want 1 and %+v", got.Count, got.ByType, want)
	}
}
