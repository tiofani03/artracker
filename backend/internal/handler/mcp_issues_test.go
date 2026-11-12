package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpTestEvent struct {
	user, session, os, osVer, maker, model, release, stack string
	crumbs                                                 []map[string]string
	ageMins                                                int
}

func (e *testEnv) mcpAddIssue(t *testing.T, pid int64, title, typ, status string, count int64) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), `
		INSERT INTO issues(project_id, fingerprint, title, type, status, count, first_seen, last_seen)
		VALUES ($1, $2, $2, $3, $4, $5, now() - interval '3 days', now()) RETURNING id`,
		pid, title, typ, status, count).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *testEnv) mcpAddEvent(t *testing.T, issueID int64, ev mcpTestEvent) int64 {
	t.Helper()
	dev, _ := json.Marshal(map[string]string{"os": ev.os, "os_version": ev.osVer, "manufacturer": ev.maker, "model": ev.model})
	crumbs := ev.crumbs
	if crumbs == nil {
		crumbs = []map[string]string{}
	}
	cb, _ := json.Marshal(crumbs)
	var id int64
	if err := e.pool.QueryRow(context.Background(), `
		INSERT INTO issue_events(issue_id, session_id, user_id, stack_trace, message, breadcrumbs, device_info, release_version, created_at)
		VALUES ($1, $2, $3, $4, 'boom', $5, $6, $7, now() - make_interval(mins => $8)) RETURNING id`,
		issueID, ev.session, ev.user, ev.stack, cb, dev, ev.release, ev.ageMins).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// callTool returns the text content and whether the result is a tool error.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func mcpSession(t *testing.T) (*testEnv, int64, int64, *mcp.ClientSession) {
	t.Helper()
	e, org, project, hdr := keyFixture(t)
	other := e.addProject(t, org, "other-app")
	url := e.serveMCP(t)
	k := e.createKey(t, project, "mcp", hdr)
	cs, err := connectMCP(t, url, k.Secret)
	if err != nil {
		t.Fatal(err)
	}
	return e, project, other, cs
}

func android(user, session, ver, release string, age int) mcpTestEvent {
	return mcpTestEvent{user: user, session: session, os: "Android", osVer: ver, maker: "Google", model: "Pixel 8",
		release: release, stack: mcpAndroidTrace, ageMins: age}
}

// issueOrder returns issue ids in the order "#<id>" appears in a list output.
func issueOrder(out string) []string {
	var ids []string
	for _, m := range regexp.MustCompile(`(?m)^#(\d+) `).FindAllStringSubmatch(out, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

func TestMCPListIssues(t *testing.T) {
	e, project, other, cs := mcpSession(t)
	// many: 3 users, events 30h ago (previous 24h window).
	many := e.mcpAddIssue(t, project, "java.lang.NullPointerException: many users", "fatal_crash", "open", 3)
	for i, u := range []string{"u1", "u2", "u3"} {
		e.mcpAddEvent(t, many, android(u, "s"+u, "14", "1.0.0", 30*60+i))
	}
	// few: 1 user, 5 events in the last 24h, release 2.0.0.
	few := e.mcpAddIssue(t, project, "java.lang.IllegalStateException: few users", "IllegalStateException", "open", 5)
	for i := 0; i < 5; i++ {
		e.mcpAddEvent(t, few, android("u9", "s9", "13", "2.0.0", i))
	}
	resolved := e.mcpAddIssue(t, project, "resolved one", "error", "resolved", 1)
	foreign := e.mcpAddIssue(t, other, "foreign issue", "error", "open", 99)

	out, isErr := callTool(t, cs, "list_issues", nil)
	if isErr {
		t.Fatalf("default list errored: %s", out)
	}
	t.Logf("list_issues default:\n%s", out)
	if got := issueOrder(out); len(got) != 2 || got[0] != fmt.Sprint(many) || got[1] != fmt.Sprint(few) {
		t.Fatalf("default order = %v, want [%d %d]\n%s", got, many, few, out)
	}
	if strings.Contains(out, fmt.Sprintf("#%d ", resolved)) || strings.Contains(out, fmt.Sprintf("#%d ", foreign)) {
		t.Fatalf("resolved or foreign issue listed:\n%s", out)
	}
	for _, want := range []string{"Fatal", "Non-fatal", "3 users", "Android 14", "1.0.0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}

	out, _ = callTool(t, cs, "list_issues", map[string]any{"limit": 1})
	if got := issueOrder(out); len(got) != 1 {
		t.Fatalf("limit 1 = %v\n%s", got, out)
	}
	if out, isErr := callTool(t, cs, "list_issues", map[string]any{"limit": 26}); !isErr {
		t.Fatalf("limit 26 accepted:\n%s", out)
	}
	if out, isErr := callTool(t, cs, "list_issues", map[string]any{"status": "closed"}); !isErr {
		t.Fatalf("bad status accepted:\n%s", out)
	}

	out, _ = callTool(t, cs, "list_issues", map[string]any{"release": "2.0.0"})
	if got := issueOrder(out); len(got) != 1 || got[0] != fmt.Sprint(few) {
		t.Fatalf("release filter = %v\n%s", got, out)
	}

	out, _ = callTool(t, cs, "list_issues", map[string]any{"sort": "trending"})
	if got := issueOrder(out); len(got) != 2 || got[0] != fmt.Sprint(few) {
		t.Fatalf("trending = %v\n%s", got, out)
	}

	out, _ = callTool(t, cs, "list_issues", map[string]any{"status": "all"})
	if got := issueOrder(out); len(got) != 3 {
		t.Fatalf("status all = %v\n%s", got, out)
	}
}

func TestMCPGetIssue(t *testing.T) {
	e, project, _, cs := mcpSession(t)
	id := e.mcpAddIssue(t, project, "java.lang.NullPointerException: Attempt to invoke virtual method", "NullPointerException", "open", 2)
	older := e.mcpAddEvent(t, id, android("alice", "sess-old", "13", "3.0.0", 60))
	latest := android("bob", "sess-new", "14", "3.1.0", 1)
	for i := 0; i < 40; i++ {
		latest.crumbs = append(latest.crumbs, map[string]string{"category": "ui", "message": fmt.Sprintf("crumb-%02d", i)})
	}
	latestID := e.mcpAddEvent(t, id, latest)

	out, isErr := callTool(t, cs, "get_issue", map[string]any{"issue_id": id})
	if isErr {
		t.Fatalf("get_issue errored: %s", out)
	}
	t.Logf("get_issue:\n%s", out)
	for _, want := range []string{
		"Affected users: 2", "Android 14 — 50%", "Android 13 — 50%", "Google Pixel 8 — 100%",
		fmt.Sprintf("Event %d", latestID), "bob", "sess-new", "3.1.0",
		"→ com.gli.shop.checkout.CheckoutActivity.onPaymentResult (CheckoutActivity.kt:214)",
		"crumb-39", "crumb-10",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "crumb-09") {
		t.Fatalf("more than 30 breadcrumbs:\n%s", out)
	}

	out, _ = callTool(t, cs, "get_issue", map[string]any{"issue_id": id, "event_id": older})
	if !strings.Contains(out, fmt.Sprintf("Event %d", older)) || !strings.Contains(out, "sess-old") {
		t.Fatalf("event_id not honoured:\n%s", out)
	}
	if out, isErr := callTool(t, cs, "get_issue", map[string]any{"issue_id": id, "event_id": 99999999}); !isErr || !strings.Contains(out, "event not found for this issue") {
		t.Fatalf("unknown event: isErr=%v %s", isErr, out)
	}

	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM mcp_tool_calls WHERE tool='get_issue' AND issue_id=$1 AND ok`, id).Scan(&n)
	if n != 2 {
		t.Fatalf("logged ok get_issue calls = %d, want 2", n)
	}
}

func TestMCPGetIssueWithoutEvents(t *testing.T) {
	e, project, _, cs := mcpSession(t)
	id := e.mcpAddIssue(t, project, "empty", "error", "open", 1)
	out, isErr := callTool(t, cs, "get_issue", map[string]any{"issue_id": id})
	if isErr || !strings.Contains(out, "No events stored for this issue.") {
		t.Fatalf("isErr=%v:\n%s", isErr, out)
	}
}

func TestMCPGetIssueEvents(t *testing.T) {
	e, project, _, cs := mcpSession(t)
	id := e.mcpAddIssue(t, project, "java.lang.NullPointerException: x", "NullPointerException", "open", 3)
	a := e.mcpAddEvent(t, id, android("alice", "sess-a", "14", "3.1.0", 1))
	b := e.mcpAddEvent(t, id, android("bob", "sess-b", "13", "3.1.0", 2))
	c := e.mcpAddEvent(t, id, android("carol", "sess-c", "14", "3.0.0", 3))

	out, isErr := callTool(t, cs, "get_issue_events", map[string]any{"issue_id": id, "os": "Android 14"})
	if isErr {
		t.Fatalf("errored: %s", out)
	}
	t.Logf("get_issue_events:\n%s", out)
	if !strings.Contains(out, fmt.Sprint(a)) || !strings.Contains(out, fmt.Sprint(c)) || strings.Contains(out, "bob") {
		t.Fatalf("os filter wrong (a=%d b=%d c=%d):\n%s", a, b, c, out)
	}
	for _, want := range []string{"alice", "Google Pixel 8", "Android 14", "3.1.0", "sess-a",
		"com.gli.shop.checkout.CheckoutActivity.onPaymentResult (CheckoutActivity.kt:214)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	out, _ = callTool(t, cs, "get_issue_events", map[string]any{"issue_id": id, "app_version": "3.0.0"})
	if !strings.Contains(out, "carol") || strings.Contains(out, "alice") {
		t.Fatalf("app_version filter:\n%s", out)
	}
	if out, isErr := callTool(t, cs, "get_issue_events", map[string]any{"issue_id": id, "limit": 51}); !isErr {
		t.Fatalf("limit 51 accepted:\n%s", out)
	}
}

func TestMCPIssueToolsAreProjectScoped(t *testing.T) {
	e, _, other, cs := mcpSession(t)
	id := e.mcpAddIssue(t, other, "foreign", "error", "open", 1)
	ev := e.mcpAddEvent(t, id, android("x", "s", "14", "1", 1))
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"get_issue", map[string]any{"issue_id": id}},
		{"get_issue", map[string]any{"issue_id": id, "event_id": ev}},
		{"get_issue_events", map[string]any{"issue_id": id}},
	} {
		out, isErr := callTool(t, cs, tc.tool, tc.args)
		if !isErr || !strings.Contains(out, "issue not found") || strings.Contains(out, "foreign") {
			t.Fatalf("%s: isErr=%v %s", tc.tool, isErr, out)
		}
	}
	out, _ := callTool(t, cs, "list_issues", map[string]any{"status": "all"})
	if strings.Contains(out, "foreign") {
		t.Fatalf("list_issues leaked:\n%s", out)
	}
}
