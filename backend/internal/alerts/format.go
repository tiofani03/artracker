package alerts

import (
	"fmt"
	"html"
	"math"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxTitleRunes   = 200
	maxDigestTitles = 5
)

// MessageContext carries what message rendering needs besides the incident.
type MessageContext struct {
	ProjectName string
	OrgID       int64
	ProjectID   int64
	PublicURL   string
	HasReplay   bool
	// Detail is how much of the issue the message carries. The zero value
	// formats as DetailFull.
	Detail DetailLevel
	// MCPServer is the project's MCP server name (artracker-<slug>); empty
	// means the project has no active MCP key and the message has no Fix line.
	MCPServer string
}

func (mc MessageContext) minimal() bool { return mc.Detail == DetailMinimal }

// genericPhrase replaces the issue title in minimal messages.
func genericPhrase(k RuleKind) string {
	switch k {
	case KindRegression:
		return "A resolved error occurred again"
	case KindIssueSpike:
		return "Error volume is unusually high"
	default:
		return "A new error was detected"
	}
}

// digestCounts is the one-line per-kind summary used by minimal digests.
func digestCounts(incs []Incident) string {
	var n, r, sp int
	for _, inc := range incs {
		switch inc.RuleKind {
		case KindRegression:
			r++
		case KindIssueSpike:
			sp++
		default:
			n++
		}
	}
	var parts []string
	add := func(c int, one, many string) {
		switch {
		case c == 1:
			parts = append(parts, "1 "+one)
		case c > 1:
			parts = append(parts, fmt.Sprintf("%d %s", c, many))
		}
	}
	add(n, "new issue", "new issues")
	add(r, "regression", "regressions")
	add(sp, "spike", "spikes")
	return strings.Join(parts, ", ")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func esc(s string) string { return html.EscapeString(s) }

func (mc MessageContext) workspaceQuery() string {
	return fmt.Sprintf("org=%d&project=%d", mc.OrgID, mc.ProjectID)
}

func (mc MessageContext) issueURL(issueID int64) string {
	return fmt.Sprintf("%s/dashboard/issues/%d?%s", mc.PublicURL, issueID, mc.workspaceQuery())
}

func (mc MessageContext) replayURL(sessionID string) string {
	return fmt.Sprintf("%s/dashboard/replays?session=%s&%s", mc.PublicURL, url.QueryEscape(sessionID), mc.workspaceQuery())
}

func (mc MessageContext) alertsURL() string {
	return fmt.Sprintf("%s/dashboard/alerts?%s", mc.PublicURL, mc.workspaceQuery())
}

// link renders an anchor; the URL is escaped for use inside the attribute.
func link(href, label string) string {
	return fmt.Sprintf(`<a href="%s">%s</a>`, esc(href), esc(label))
}

func envRelease(inc Incident) string {
	var parts []string
	if inc.Environment != "" {
		parts = append(parts, esc(inc.Environment))
	}
	if inc.ReleaseVersion != "" {
		parts = append(parts, "release "+esc(inc.ReleaseVersion))
	}
	return strings.Join(parts, " · ")
}

func kindHeader(k RuleKind) string {
	switch k {
	case KindRegression:
		return "🔁 Regression"
	case KindIssueSpike:
		return "📈 Spike"
	default:
		return "🆕 New issue"
	}
}

func formatBaseline(b float64) string {
	return strconv.FormatFloat(math.Round(b*10)/10, 'f', -1, 64)
}

// FormatFire renders the message sent when an incident fires (parse_mode=HTML).
func FormatFire(inc Incident, mc MessageContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n", kindHeader(inc.RuleKind), esc(mc.ProjectName))
	if mc.minimal() {
		b.WriteString(genericPhrase(inc.RuleKind) + "\n")
	} else {
		b.WriteString(esc(truncateRunes(inc.Title, maxTitleRunes)) + "\n")
	}
	if inc.RuleKind == KindIssueSpike && inc.Spike != nil {
		fmt.Fprintf(&b, "%d events in %d min, baseline %s\n", inc.Spike.Count, inc.Spike.WindowMinutes, formatBaseline(inc.Spike.Baseline))
	}
	if er := envRelease(inc); er != "" {
		b.WriteString(er + "\n")
	}
	b.WriteString(link(mc.issueURL(inc.IssueID), "Open issue"))
	if !mc.minimal() && mc.HasReplay && inc.SessionID != "" {
		b.WriteString(" · " + link(mc.replayURL(inc.SessionID), "Watch replay"))
	}
	if mc.MCPServer != "" && !mc.minimal() {
		fmt.Fprintf(&b, "\nFix with Claude: <code>/mcp__%s__fix_issue %d</code>", esc(mc.MCPServer), inc.IssueID)
	}
	return b.String()
}

// FormatRecovered renders the message sent when a spike incident resolves.
func FormatRecovered(inc Incident, mc MessageContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✅ Spike recovered · %s\n", esc(mc.ProjectName))
	if mc.minimal() {
		b.WriteString("The error volume is back to normal\n")
	} else {
		b.WriteString(esc(truncateRunes(inc.Title, maxTitleRunes)) + "\n")
	}
	if er := envRelease(inc); er != "" {
		b.WriteString(er + "\n")
	}
	b.WriteString(link(mc.issueURL(inc.IssueID), "Open issue"))
	return b.String()
}

// FormatDigest summarises a burst of incidents in one message.
func FormatDigest(incs []Incident, mc MessageContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🚨 %d new alerts · %s\n", len(incs), esc(mc.ProjectName))
	if mc.minimal() {
		b.WriteString(digestCounts(incs) + "\n")
		b.WriteString(link(mc.alertsURL(), "Open alerts"))
		return b.String()
	}
	for i, inc := range incs {
		if i == maxDigestTitles {
			break
		}
		b.WriteString("• " + esc(truncateRunes(inc.Title, maxTitleRunes)) + "\n")
	}
	if len(incs) > maxDigestTitles {
		fmt.Fprintf(&b, "+%d more\n", len(incs)-maxDigestTitles)
	}
	b.WriteString(link(mc.alertsURL(), "Open alerts"))
	return b.String()
}

// FormatTest is the message sent by the "test channel" action.
func FormatTest(mc MessageContext) string {
	return fmt.Sprintf("✅ Test alert · %s\nTelegram alerts are connected.\n%s",
		esc(mc.ProjectName), link(mc.alertsURL(), "Open alerts"))
}
