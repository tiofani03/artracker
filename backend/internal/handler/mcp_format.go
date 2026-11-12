package handler

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tiofani03/artracker/backend/internal/issues"
)

// Text formatting for the MCP issue tools. Output is read by an LLM: short
// lines, headings, no raw JSON, and every list bounded.

const (
	mcpTraceMaxLines  = 60
	mcpBreadcrumbsMax = 30
	mcpBreakdownTop   = 5
	mcpLineMax        = 200
)

// mcpIssueRow is one list_issues row: the issue plus the numbers an assistant
// needs to rank it.
type mcpIssueRow struct {
	Issue
	Fatal                           bool
	AffectedUsers, Last24h, Prev24h int64
	TopOS, TopApp                   string
}

// clip shortens s to one line of at most n runes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orUnknown(s string) string {
	if s == "" {
		return "Unknown"
	}
	return s
}

func mcpTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func severityLabel(fatal bool) string {
	if fatal {
		return "Fatal"
	}
	return "Non-fatal"
}

// formatIssueList renders list_issues rows, three or four short lines each.
func formatIssueList(rows []mcpIssueRow) string {
	if len(rows) == 0 {
		return "No issues match these filters."
	}
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "#%d %s · %s · %s\n", r.ID, severityLabel(r.Fatal), clip(orDash(r.Type), 60), r.Status)
		fmt.Fprintf(&b, "  %s\n", clip(r.Title, 140))
		fmt.Fprintf(&b, "  %s · %s · last 24h: %d (prev 24h: %d)\n",
			plural(r.AffectedUsers, "user"), plural(r.Count, "event"), r.Last24h, r.Prev24h)
		fmt.Fprintf(&b, "  first seen %s · last seen %s\n", mcpTime(r.FirstSeen), mcpTime(r.LastSeen))
		fmt.Fprintf(&b, "  top OS: %s · top app version: %s\n", clip(orUnknown(r.TopOS), 40), clip(orUnknown(r.TopApp), 40))
	}
	return strings.TrimRight(b.String(), "\n")
}

func plural(n int64, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.FormatInt(n, 10) + " " + word + "s"
}

// formatStackTrace renders parsed sections: app frames one per line, each run
// of framework frames collapsed to a count, causes kept. At most
// mcpTraceMaxLines lines, then "… truncated".
func formatStackTrace(sections []issues.StackSection, deobfuscated bool) string {
	if len(sections) == 0 {
		return "No stack trace captured for this event."
	}
	lines := []string{"Stack trace (deobfuscated):"}
	if !deobfuscated {
		lines[0] = "Stack trace (not deobfuscated):"
	}
	for _, s := range sections {
		title := clip(strings.SplitN(s.Title, "\n", 2)[0], mcpLineMax)
		if s.CausedBy {
			title = "Caused by: " + title
		}
		lines = append(lines, title)
		for _, g := range issues.GroupFrames(s.Frames) {
			if g.App == nil {
				n := len(g.Library)
				word := "frames"
				if n == 1 {
					word = "frame"
				}
				lines = append(lines, fmt.Sprintf("  … %d framework %s", n, word))
				continue
			}
			lines = append(lines, "  → "+frameText(*g.App))
		}
	}
	if len(lines) > mcpTraceMaxLines {
		lines = append(lines[:mcpTraceMaxLines], "… truncated")
	}
	return strings.Join(lines, "\n")
}

// firstAppFrame is the top app frame of a trace ("symbol (location)"), falling
// back to the first frame of any kind.
func firstAppFrame(trace string) string {
	sections := issues.ParseStackTrace(trace)
	var fallback *issues.StackFrame
	for _, s := range sections {
		for i := range s.Frames {
			f := s.Frames[i]
			if fallback == nil {
				fallback = &f
			}
			if f.App {
				return frameText(f)
			}
		}
	}
	if fallback != nil {
		return frameText(*fallback)
	}
	return "(no stack trace)"
}

func frameText(f issues.StackFrame) string {
	if f.Location == "" || f.Location == f.Symbol {
		return clip(f.Symbol, 160)
	}
	return clip(f.Symbol+" ("+f.Location+")", 160)
}

// compareVersions mirrors compareVersions in frontend/src/issues/breakdown.ts.
func compareVersions(a, b string) int {
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '-' })
	}
	pa, pb := split(a), split(b)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		// Number('') is 0 in JS, so a missing segment compares as 0.
		num := func(s string) (float64, error) {
			if s == "" {
				return 0, nil
			}
			return strconv.ParseFloat(s, 64)
		}
		nx, ex := num(x)
		ny, ey := num(y)
		if ex != nil || ey != nil {
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
			continue
		}
		if nx != ny {
			if nx < ny {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionRange mirrors versionRange in frontend/src/issues/breakdown.ts.
func versionRange(buckets []IssueBucket) string {
	var vs []string
	for _, b := range buckets {
		if b.Value != "" && b.Value != "Unknown" {
			vs = append(vs, b.Value)
		}
	}
	if len(vs) == 0 {
		return "unknown"
	}
	sort.SliceStable(vs, func(i, j int) bool { return compareVersions(vs[i], vs[j]) < 0 })
	if vs[0] == vs[len(vs)-1] {
		return vs[0]
	}
	return vs[0] + " – " + vs[len(vs)-1]
}

// shareLines mirrors toShareRows in frontend/src/issues/breakdown.ts: top 5
// with rounded percentages, the remainder folded into "Other".
func shareLines(b *strings.Builder, heading string, buckets []IssueBucket, total int64) {
	if total <= 0 || len(buckets) == 0 {
		return
	}
	pct := func(n int64) int { return int(math.Round(float64(n) / float64(total) * 100)) }
	fmt.Fprintf(b, "%s:\n", heading)
	top := buckets[:min(len(buckets), mcpBreakdownTop)]
	rest := total
	for _, bk := range top {
		fmt.Fprintf(b, "  %s — %d%% (%d)\n", clip(bk.Value, 60), pct(bk.Count), bk.Count)
		rest -= bk.Count
	}
	if rest > 0 {
		fmt.Fprintf(b, "  Other — %d%% (%d)\n", pct(rest), rest)
	}
}

// formatIssueDetail renders get_issue: header, breakdowns, then one event's
// context, stack trace and last breadcrumbs. ev.ID == 0 means no event stored.
func formatIssueDetail(d IssueDetail, ev IssueEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Issue #%d: %s\n", d.ID, clip(d.Title, mcpLineMax))
	fmt.Fprintf(&b, "%s · %s · status %s\n", severityLabel(issues.IsFatal(d.Type, d.Title)), clip(orDash(d.Type), 60), d.Status)
	fmt.Fprintf(&b, "Events: %d · Affected users: %d\n", d.Count, d.Summary.AffectedUsers)
	fmt.Fprintf(&b, "First seen: %s · Last seen: %s\n", mcpTime(d.FirstSeen), mcpTime(d.LastSeen))
	fmt.Fprintf(&b, "App versions: %s\n", versionRange(d.Summary.AppVersions))

	if ev.ID == 0 {
		b.WriteString("\nNo events stored for this issue.")
		return b.String()
	}

	b.WriteString("\n")
	shareLines(&b, "OS versions", d.Summary.OSVersions, d.Summary.TotalEvents)
	shareLines(&b, "Devices", d.Summary.Devices, d.Summary.TotalEvents)
	shareLines(&b, "App versions", d.Summary.AppVersions, d.Summary.TotalEvents)

	fmt.Fprintf(&b, "\nEvent %d · %s\n", ev.ID, ev.CreatedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&b, "  User: %s · Session: %s\n", clip(orDash(ev.UserID), 80), clip(orDash(ev.SessionID), 80))
	fmt.Fprintf(&b, "  Device: %s · OS: %s · App: %s\n",
		clip(orUnknown(ev.DeviceLabel), 60), clip(orUnknown(ev.OSLabel), 60), clip(orUnknown(ev.AppLabel), 40))
	if ev.Message != "" {
		fmt.Fprintf(&b, "  Message: %s\n", clip(ev.Message, 300))
	}

	b.WriteString("\n")
	deobfuscated := ev.StackTraceResolved != ev.StackTrace
	b.WriteString(formatStackTrace(issues.ParseStackTrace(ev.StackTraceResolved), deobfuscated))
	b.WriteString("\n")
	if !deobfuscated && strings.TrimSpace(ev.StackTrace) != "" {
		if ev.ReleaseVersion == "" {
			b.WriteString("Note: not deobfuscated: this event has no release version, so no mapping or source map could be applied.\n")
		} else {
			fmt.Fprintf(&b, "Note: not deobfuscated: no uploaded mapping/source map/dSYM for version %s changed this trace. "+
				"If the frames look obfuscated, the mapping for %s is missing.\n", clip(ev.ReleaseVersion, 40), clip(ev.ReleaseVersion, 40))
		}
	}

	b.WriteString("\n")
	b.WriteString(formatBreadcrumbs(ev.Breadcrumbs))
	return b.String()
}

// formatBreadcrumbs renders the last mcpBreadcrumbsMax breadcrumbs, oldest
// first so the crash comes right after the final line.
func formatBreadcrumbs(raw json.RawMessage) string {
	var crumbs []map[string]any
	if err := json.Unmarshal(raw, &crumbs); err != nil || len(crumbs) == 0 {
		return "No breadcrumbs recorded for this event."
	}
	total := len(crumbs)
	if total > mcpBreadcrumbsMax {
		crumbs = crumbs[total-mcpBreadcrumbsMax:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Breadcrumbs (last %d of %d, oldest first):\n", len(crumbs), total)
	str := func(m map[string]any, k string) string {
		if s, ok := m[k].(string); ok {
			return s
		}
		return ""
	}
	for _, c := range crumbs {
		kind := str(c, "category")
		if kind == "" {
			kind = str(c, "type")
		}
		msg := str(c, "message")
		if msg == "" {
			msg = kind
		}
		if kind == "" {
			kind = "event"
		}
		line := "  "
		if ts := crumbTime(c["timestamp"]); ts != "" {
			line += ts + " "
		}
		fmt.Fprintf(&b, "%s%s: %s\n", line, clip(kind, 30), clip(orDash(msg), 160))
	}
	return strings.TrimRight(b.String(), "\n")
}

// crumbTime accepts an RFC 3339 string or epoch milliseconds.
func crumbTime(v any) string {
	switch t := v.(type) {
	case string:
		if p, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return p.UTC().Format("15:04:05")
		}
	case float64:
		if t > 1e11 {
			return time.UnixMilli(int64(t)).UTC().Format("15:04:05")
		}
		return time.Unix(int64(t), 0).UTC().Format("15:04:05")
	}
	return ""
}

// formatEventLine renders one get_issue_events row on two short lines.
func formatEventLine(ev IssueEvent) string {
	return fmt.Sprintf("Event %d · %s · user %s · %s · %s · app %s · session %s\n    at %s",
		ev.ID, mcpTime(ev.CreatedAt), clip(orDash(ev.UserID), 60),
		clip(orUnknown(ev.DeviceLabel), 50), clip(orUnknown(ev.OSLabel), 40), clip(orUnknown(ev.AppLabel), 30),
		clip(orDash(ev.SessionID), 60), firstAppFrame(ev.StackTraceResolved))
}
