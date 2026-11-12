package handler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClassifyMarker(t *testing.T) {
	m := func(kv ...any) map[string]any {
		out := map[string]any{}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	cases := []struct {
		name               string
		level, evName, msg string
		attrs              map[string]any
		want               string
	}{
		{"level error", "error", "x", "", nil, "error"},
		{"level fatal uppercase", "FATAL", "x", "", nil, "error"},
		{"name error", "", "js_error", "", nil, "error"},
		{"name exception", "", "UnhandledException", "", nil, "error"},
		{"name crash", "", "app_crash", "", nil, "error"},
		{"msg error", "", "log", "Something Error here", nil, "error"},
		{"msg uncaught", "", "log", "uncaught throw", nil, "error"},
		{"msg exception", "", "log", "an exception", nil, "error"},
		{"msg typeerror", "", "log", "TypeError: x", nil, "error"},
		{"anr by name", "error", "anr_detected", "", nil, "anr"},
		{"anr by msg", "error", "x", "ANR in main", nil, "anr"},
		{"hang by name", "error", "ui_hang", "", nil, "ui hang"},
		{"hang by msg", "error", "x", "main thread hang", nil, "ui hang"},
		{"anr beats hang", "error", "anr_hang", "", nil, "anr"},
		{"error beats rage", "", "rage_click_error", "", nil, "error"},
		{"rage_tap name", "", "rage_tap", "", nil, "rage click"},
		{"rage_click name", "", "rage_click", "", nil, "rage click"},
		{"rage tap msg", "", "x", "user did a Rage Tap", nil, "rage click"},
		{"rage click msg", "", "x", "rage click detected", nil, "rage click"},
		{"dead_click name", "", "dead_click", "", nil, "dead click"},
		{"dead click msg", "", "x", "Dead Click on btn", nil, "dead click"},
		{"click", "", "click", "", nil, "tap"},
		{"touch", "", "touch", "", nil, "tap"},
		{"gesture attr", "", "x", "", m("gesture", "swipe"), "tap"},
		{"empty gesture is not a tap", "", "x", "", m("gesture", ""), "x"},
		{"rage beats gesture", "", "rage_tap", "", m("gesture", "tap"), "rage click"},
		{"network name default", "", "network", "", nil, "HTTP 200"},
		{"http prefix", "", "http_request", "", nil, "HTTP 200"},
		{"method and status_code", "", "x", "", m("method", "POST", "status_code", float64(500)), "POST 500"},
		{"status fallback", "", "x", "", m("method", "GET", "status", float64(404)), "GET 404"},
		{"status_code wins over status", "", "x", "", m("endpoint", "/a", "status_code", float64(201), "status", float64(500)), "HTTP 201"},
		{"string status ignored", "", "x", "", m("method", "GET", "status_code", "500"), "GET 200"},
		{"zero status_code is falsy but numeric", "", "network", "", m("status_code", float64(0)), "HTTP 0"},
		{"endpoint only", "", "x", "", m("endpoint", "/a"), "HTTP 200"},
		{"warn level", "warn", "x", "", nil, "warn"},
		{"warning level", "Warning", "x", "", nil, "warn"},
		{"error beats network", "error", "network", "", m("method", "GET"), "error"},
		{"fallback to name", "", "screen_view", "Home", nil, "screen_view"},
		{"fallback empty name", "", "", "", nil, "event"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyMarker(c.level, c.evName, c.msg, c.attrs); got != c.want {
				t.Fatalf("classifyMarker(%q,%q,%q,%v) = %q, want %q", c.level, c.evName, c.msg, c.attrs, got, c.want)
			}
		})
	}
}

func (e *testEnv) tlAddTelemetry(t *testing.T, pid int64, session, kind, name, level, msg, attrs string, offsetMs int) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `
		INSERT INTO telemetry_events(id, project_id, kind, name, level, message, session_id, event_timestamp, attributes)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, '2026-01-01T10:00:00Z'::timestamptz + make_interval(secs => $7::float8 / 1000), $8::jsonb)`,
		pid, kind, name, level, msg, session, offsetMs, attrs); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) tlAddCrash(t *testing.T, issueID int64, session, msg string, offsetMs int) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), `
		INSERT INTO issue_events(issue_id, session_id, message, created_at)
		VALUES ($1, $2, $3, '2026-01-01T10:00:00Z'::timestamptz + make_interval(secs => $4::float8 / 1000)) RETURNING id`,
		issueID, session, msg, offsetMs).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMCPSessionTimeline(t *testing.T) {
	e, project, other, cs := mcpSession(t)
	// telemetry_events is not truncated between tests, so use unique session ids and clean up.
	sid := fmt.Sprintf("sess-tl-%d", time.Now().UnixNano())
	foreign := sid + "-foreign"
	t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM telemetry_events WHERE session_id IN ($1, $2)`, sid, foreign)
	})
	e.tlAddTelemetry(t, project, sid, "event", "click", "", "btn_checkout", `{}`, 0)
	e.tlAddTelemetry(t, project, sid, "event", "network", "", "", `{"method":"POST","endpoint":"/api/v1/charge","status_code":200}`, 12300)
	e.tlAddTelemetry(t, project, sid, "event", "replay_chunk", "", "chunk", `{}`, 13000)
	issue := e.mcpAddIssue(t, project, "java.lang.NullPointerException: boom", "fatal_crash", "open", 1)
	crash := e.tlAddCrash(t, issue, sid, "java.lang.NullPointerException", 15000)
	e.tlAddTelemetry(t, project, sid, "event", "screen_view", "", "Home "+strings.Repeat("x", 300), `{}`, 20000)

	out, isErr := callTool(t, cs, "get_session_timeline", map[string]any{"session_id": sid})
	if isErr {
		t.Fatalf("errored: %s", out)
	}
	t.Logf("timeline:\n%s", out)
	order := []string{"+0.0s tap btn_checkout", "+12.3s network POST /api/v1/charge 200", "+15.0s crash java.lang.NullPointerException", "+20.0s screen_view Home "}
	pos := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i < 0 || i < pos {
			t.Fatalf("missing or out of order %q in:\n%s", want, out)
		}
		pos = i
	}
	if strings.Contains(out, "replay_chunk") {
		t.Fatalf("replay_chunk leaked:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if len([]rune(l)) > 220 {
			t.Fatalf("line not truncated (%d runes): %s", len([]rune(l)), l)
		}
	}

	// until_event_id stops at the crash (inclusive).
	out, isErr = callTool(t, cs, "get_session_timeline", map[string]any{"session_id": sid, "until_event_id": crash})
	if isErr || strings.Contains(out, "screen_view") || !strings.Contains(out, "crash java.lang.NullPointerException") {
		t.Fatalf("until_event_id: err=%v\n%s", isErr, out)
	}

	// limit keeps the newest N, chronological.
	out, _ = callTool(t, cs, "get_session_timeline", map[string]any{"session_id": sid, "limit": 2})
	if strings.Contains(out, "btn_checkout") || strings.Contains(out, "POST") ||
		!strings.Contains(out, "crash") || !strings.Contains(out, "screen_view") ||
		strings.Index(out, "crash") > strings.Index(out, "screen_view") {
		t.Fatalf("limit=2:\n%s", out)
	}
	if out, isErr = callTool(t, cs, "get_session_timeline", map[string]any{"session_id": sid, "limit": 201}); !isErr {
		t.Fatalf("limit 201 accepted:\n%s", out)
	}

	// unknown session
	out, isErr = callTool(t, cs, "get_session_timeline", map[string]any{"session_id": "no-such-session"})
	if isErr || !strings.Contains(out, "No events recorded for this session.") {
		t.Fatalf("unknown session: err=%v %q", isErr, out)
	}
	// invalid id
	if out, isErr = callTool(t, cs, "get_session_timeline", map[string]any{"session_id": "bad id!"}); !isErr {
		t.Fatalf("invalid id accepted: %s", out)
	}
	// no arguments must not panic the server
	if _, isErr = callTool(t, cs, "get_session_timeline", nil); !isErr {
		t.Fatal("no-argument call should be a tool error")
	}

	// tenant isolation: another project's session and event look unknown.
	e.tlAddTelemetry(t, other, foreign, "event", "click", "", "secret_button", `{}`, 0)
	fIssue := e.mcpAddIssue(t, other, "foreign crash", "fatal_crash", "open", 1)
	fCrash := e.tlAddCrash(t, fIssue, foreign, "foreign boom", 1000)
	out, isErr = callTool(t, cs, "get_session_timeline", map[string]any{"session_id": foreign})
	if isErr || !strings.Contains(out, "No events recorded for this session.") || strings.Contains(out, "secret") {
		t.Fatalf("foreign session: err=%v %q", isErr, out)
	}
	// foreign until_event_id on our own session behaves like an unknown id.
	outUnknown, errUnknown := callTool(t, cs, "get_session_timeline", map[string]any{"session_id": sid, "until_event_id": 999999999})
	outForeign, errForeign := callTool(t, cs, "get_session_timeline", map[string]any{"session_id": sid, "until_event_id": fCrash})
	if outUnknown != outForeign || errUnknown != errForeign {
		t.Fatalf("foreign until differs from unknown: %q/%v vs %q/%v", outForeign, errForeign, outUnknown, errUnknown)
	}
	if strings.Contains(outForeign, "foreign") {
		t.Fatalf("leak: %s", outForeign)
	}

	// audit log: no foreign issue ids, arguments not stored.
	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM mcp_tool_calls WHERE tool='get_session_timeline' AND issue_id IS NOT NULL`)); n != 0 {
		t.Fatalf("timeline calls stored an issue id: %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM mcp_tool_calls WHERE tool='get_session_timeline' AND ok`); n == 0 {
		t.Fatal("successful call not logged")
	}
	if n := e.count(t, `SELECT count(*) FROM mcp_tool_calls WHERE tool='get_session_timeline' AND NOT ok`); n == 0 {
		t.Fatal("failed call not logged")
	}
}

// Client-controlled attributes (method, endpoint, name, message) can be 32 KB
// and contain newlines: every rendered line must stay single-line and bounded.
func TestFormatTimelineLineClipsClientInput(t *testing.T) {
	huge := strings.Repeat("A\n+9.9s crash forged line\r\n", 1300) // ~32 KB
	rows := []timelineRow{
		{OffsetMs: 1000, Name: "network", Attrs: map[string]any{"method": huge, "endpoint": huge, "status_code": 500.0}},
		{OffsetMs: 1000, Name: "network", Attrs: map[string]any{"method": huge}},
		{OffsetMs: 1000, Name: huge, Msg: huge, Attrs: map[string]any{}},
		{OffsetMs: 1000, Name: huge, Attrs: map[string]any{}},
		{OffsetMs: 1000, Msg: huge, IsCrash: true},
	}
	for i, x := range rows {
		l := formatTimelineLine(x)
		if strings.ContainsAny(l, "\r\n") {
			t.Fatalf("row %d: line contains a newline: %.120q", i, l)
		}
		if n := len([]rune(l)); n > 300 {
			t.Fatalf("row %d: line is %d runes: %.120q", i, n, l)
		}
	}
}
