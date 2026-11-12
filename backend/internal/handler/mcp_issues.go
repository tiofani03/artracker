package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tiofani03/artracker/backend/internal/issues"
)

const mcpDBTimeout = 5 * time.Second

type listIssuesIn struct {
	Status  string `json:"status,omitempty" jsonschema:"Issue status to list: open (default), resolved, ignored or all."`
	Sort    string `json:"sort,omitempty" jsonschema:"Ranking: users (most affected users, default), events (most events), trending (biggest rise in the last 24h vs the previous 24h) or newest (most recently first seen)."`
	Release string `json:"release,omitempty" jsonschema:"Only issues with at least one event in this app version (exact label, e.g. 3.1.0)."`
	Limit   int    `json:"limit,omitempty" jsonschema:"How many issues to return, 1-25 (default 10)."`
}

type getIssueIn struct {
	IssueID int64 `json:"issue_id" jsonschema:"The issue id (the #number from list_issues)."`
	EventID int64 `json:"event_id,omitempty" jsonschema:"Optional event id from get_issue_events; defaults to the latest event."`
}

type getIssueEventsIn struct {
	IssueID    int64  `json:"issue_id" jsonschema:"The issue id (the #number from list_issues)."`
	Limit      int    `json:"limit,omitempty" jsonschema:"How many events to return, newest first, 1-50 (default 20)."`
	OS         string `json:"os,omitempty" jsonschema:"Only events on this OS label, exactly as get_issue shows it (e.g. Android 14)."`
	Device     string `json:"device,omitempty" jsonschema:"Only events on this device label, exactly as get_issue shows it (e.g. Google Pixel 8)."`
	AppVersion string `json:"app_version,omitempty" jsonschema:"Only events from this app version label, exactly as get_issue shows it."`
}

func floatPtr(f float64) *float64 { return &f }

// mcpSchema infers T's input schema once and lets edit add enums and bounds
// the struct tags cannot express. The SDK validates arguments against it, so
// an out-of-range value comes back as a tool error. Do NOT set schema
// "default"s: go-sdk v1.8.0 applies them into a nil map and panics (crashing
// the process) when a client calls a tool with no arguments. Defaults are
// applied in the handlers instead.
func mcpSchema[T any](edit func(p map[string]*jsonschema.Schema)) func() *jsonschema.Schema {
	return sync.OnceValue(func() *jsonschema.Schema {
		s, err := jsonschema.For[T](nil)
		if err != nil {
			panic(err)
		}
		edit(s.Properties)
		return s
	})
}

var (
	listIssuesSchema = mcpSchema[listIssuesIn](func(p map[string]*jsonschema.Schema) {
		p["status"].Enum = []any{"open", "resolved", "ignored", "all"}
		p["sort"].Enum = []any{"users", "events", "trending", "newest"}
		p["limit"].Minimum, p["limit"].Maximum = floatPtr(1), floatPtr(25)
	})
	getIssueSchema = mcpSchema[getIssueIn](func(p map[string]*jsonschema.Schema) {
		p["issue_id"].Minimum = floatPtr(1)
		p["event_id"].Minimum = floatPtr(1)
	})
	getIssueEventsSchema = mcpSchema[getIssueEventsIn](func(p map[string]*jsonschema.Schema) {
		p["issue_id"].Minimum = floatPtr(1)
		p["limit"].Minimum, p["limit"].Maximum = floatPtr(1), floatPtr(50)
	})
)

var errMCPEventNotFound = errors.New("event not found for this issue")

func mcpText(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// mcpToolError keeps user-facing errors (not found, bad input) and hides
// internal ones behind a generic message, logging the cause.
func mcpToolError(tool string, err error) error {
	if errors.Is(err, errIssueNotFound) {
		return errors.New("issue not found")
	}
	if errors.Is(err, errMCPEventNotFound) {
		return err
	}
	var bad mcpBadInput
	if errors.As(err, &bad) {
		return err
	}
	slog.Error("mcp tool failed", "tool", tool, "err", err)
	return fmt.Errorf("%s failed; try again", tool)
}

type mcpBadInput string

func (e mcpBadInput) Error() string { return string(e) }

func registerIssueTools(s *mcp.Server, h *Handler, mc mcpContext) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_issues",
		Description: "List this project's crash and error issues, ranked. Call this first to find what to fix. " +
			"Returns up to `limit` issues as short text blocks: #id, Fatal/Non-fatal, type, status, title, affected users, " +
			"total events, events in the last 24h vs the previous 24h, first/last seen, top OS and top app version. " +
			"Use sort=trending to find regressions, release=<version> to focus on one app version. " +
			"Pass an #id to get_issue for the stack trace.",
		InputSchema: listIssuesSchema(),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listIssuesIn) (*mcp.CallToolResult, any, error) {
		out, err := h.mcpListIssues(ctx, mc, in)
		h.logMCPCall(ctx, mc, "list_issues", nil, err == nil)
		if err != nil {
			return nil, nil, mcpToolError("list_issues", err)
		}
		return mcpText(out), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_issue",
		Description: "Get one issue in detail, to understand and fix it. Returns: header (title, type, Fatal/Non-fatal, " +
			"status, events, affected users, first/last seen, app version range), top-5 breakdowns of OS version, device " +
			"and app version with percentages, then one event (latest by default, or event_id): its time, user, session, " +
			"device, OS and app version, the deobfuscated stack trace with app frames marked → and framework frames " +
			"collapsed, and its last 30 breadcrumbs. Says when the trace could not be deobfuscated.",
		InputSchema: getIssueSchema(),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIssueIn) (*mcp.CallToolResult, any, error) {
		out, err := h.mcpGetIssue(ctx, mc, in)
		h.logMCPCall(ctx, mc, "get_issue", &in.IssueID, err == nil)
		if err != nil {
			return nil, nil, mcpToolError("get_issue", err)
		}
		return mcpText(out), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_issue_events",
		Description: "List individual occurrences of one issue, newest first, to compare them or pick one for get_issue " +
			"(event_id) or get_session_timeline (session). Optional exact filters os, device, app_version use the labels " +
			"get_issue shows. One entry per event: event id, time, user, device, OS, app version, session id and the " +
			"first app frame of its stack trace.",
		InputSchema: getIssueEventsSchema(),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getIssueEventsIn) (*mcp.CallToolResult, any, error) {
		out, err := h.mcpGetIssueEvents(ctx, mc, in)
		h.logMCPCall(ctx, mc, "get_issue_events", &in.IssueID, err == nil)
		if err != nil {
			return nil, nil, mcpToolError("get_issue_events", err)
		}
		return mcpText(out), nil, nil
	})
}

func (h *Handler) mcpListIssues(ctx context.Context, mc mcpContext, in listIssuesIn) (string, error) {
	if in.Status == "" {
		in.Status = "open"
	}
	if in.Sort == "" {
		in.Sort = "users"
	}
	if in.Limit == 0 {
		in.Limit = 10
	}
	if in.Limit < 1 || in.Limit > 25 {
		return "", mcpBadInput("limit must be between 1 and 25")
	}
	ctx, cancel := context.WithTimeout(ctx, mcpDBTimeout)
	defer cancel()
	rows, err := h.listIssuesForMCP(ctx, mc.ProjectID, in)
	if err != nil {
		return "", err
	}
	head := fmt.Sprintf("%s issues in %s (%s), sorted by %s", statusHeading(in.Status), mc.ProjectName, mc.Platform, in.Sort)
	if in.Release != "" {
		head += ", release " + clip(in.Release, 40)
	}
	head += fmt.Sprintf(": %d shown.\n\n", len(rows))
	return head + formatIssueList(rows), nil
}

func statusHeading(s string) string {
	if s == "all" {
		return "All"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var mcpIssueOrder = map[string]string{
	"users":    "affected_users DESC, count DESC, last_seen DESC, id DESC",
	"events":   "count DESC, last_seen DESC, id DESC",
	"trending": "(last24 - prev24) DESC, last24 DESC, last_seen DESC, id DESC",
	"newest":   "first_seen DESC, id DESC",
}

// listIssuesForMCP loads ranked issues with their per-issue numbers in one
// query. Top OS / app labels come from issueEventLabels, so they match the
// dashboard breakdowns.
func (h *Handler) listIssuesForMCP(ctx context.Context, projectID int64, in listIssuesIn) ([]mcpIssueRow, error) {
	if in.Status != "all" && !validIssueStatus(in.Status) {
		return nil, mcpBadInput("status must be open, resolved, ignored or all")
	}
	order, ok := mcpIssueOrder[in.Sort]
	if !ok {
		return nil, mcpBadInput("sort must be users, events, trending or newest")
	}
	topLabel := func(col string) string {
		return `LEFT JOIN LATERAL (
			SELECT l.` + col + ` AS v FROM (` + issueEventLabels + ` WHERE e.issue_id = t.id) l
			GROUP BY 1 ORDER BY count(*) DESC, 1 LIMIT 1) top_` + col + ` ON true`
	}
	q := `
		WITH cand AS (
			SELECT i.id, i.title, i.type, i.status, i.first_seen, i.last_seen, i.count
			FROM issues i
			WHERE i.project_id = $1
			  AND ($2 = 'all' OR i.status = $2)
			  AND ($3 = '' OR EXISTS (
			      SELECT 1 FROM (` + issueEventLabels + ` WHERE e.issue_id = i.id) l WHERE l.app = $3))
		), stats AS (
			SELECT e.issue_id,
			       count(DISTINCT NULLIF(e.user_id, '')) AS affected_users,
			       count(*) FILTER (WHERE e.created_at >= now() - interval '24 hours') AS last24,
			       count(*) FILTER (WHERE e.created_at >= now() - interval '48 hours'
			                          AND e.created_at < now() - interval '24 hours') AS prev24
			FROM issue_events e
			WHERE e.issue_id IN (SELECT id FROM cand)
			GROUP BY e.issue_id
		), ranked AS (
			SELECT c.*, COALESCE(s.affected_users, 0) AS affected_users,
			       COALESCE(s.last24, 0) AS last24, COALESCE(s.prev24, 0) AS prev24
			FROM cand c LEFT JOIN stats s ON s.issue_id = c.id
		), t AS (
			SELECT * FROM ranked ORDER BY ` + order + ` LIMIT $4
		)
		SELECT t.id, t.title, t.type, t.status, t.first_seen, t.last_seen, t.count,
		       t.affected_users, t.last24, t.prev24,
		       COALESCE(top_os.v, ''), COALESCE(top_app.v, '')
		FROM t
		` + topLabel("os") + `
		` + topLabel("app") + `
		ORDER BY ` + order
	rows, err := h.db.Query(ctx, q, projectID, in.Status, in.Release, in.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (mcpIssueRow, error) {
		var it mcpIssueRow
		err := r.Scan(&it.ID, &it.Title, &it.Type, &it.Status, &it.FirstSeen, &it.LastSeen, &it.Count,
			&it.AffectedUsers, &it.Last24h, &it.Prev24h, &it.TopOS, &it.TopApp)
		it.ProjectID = projectID
		it.Fatal = issues.IsFatal(it.Type, it.Title)
		return it, err
	})
}

func (h *Handler) mcpGetIssue(ctx context.Context, mc mcpContext, in getIssueIn) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpDBTimeout)
	defer cancel()
	// Event limit 0: only the header and summary; the one event shown is
	// loaded below, so an older event_id works too.
	d, err := h.loadIssueDetail(ctx, mc.ProjectID, in.IssueID, 0)
	if err != nil {
		return "", err
	}
	ev, err := h.loadMCPEvent(ctx, mc.ProjectID, in.IssueID, in.EventID)
	if errors.Is(err, pgx.ErrNoRows) {
		if in.EventID != 0 {
			return "", errMCPEventNotFound
		}
		return formatIssueDetail(d, IssueEvent{}), nil
	}
	if err != nil {
		return "", err
	}
	return formatIssueDetail(d, ev), nil
}

// loadMCPEvent loads one event of an issue (the latest when eventID is 0)
// with its display labels and symbolicated trace. The caller has already
// checked the issue belongs to projectID.
func (h *Handler) loadMCPEvent(ctx context.Context, projectID, issueID, eventID int64) (IssueEvent, error) {
	var ev IssueEvent
	var breadcrumbs, deviceInfo []byte
	err := h.db.QueryRow(ctx, `
		SELECT x.id, x.session_id, x.user_id, x.message, x.stack_trace, x.breadcrumbs, x.device_info,
		       x.release_version, x.created_at, l.os, l.device, l.app
		FROM (`+issueEventLabels+` WHERE e.issue_id = $1 AND ($2 = 0 OR e.id = $2)) l
		JOIN issue_events x ON x.id = l.id
		ORDER BY x.created_at DESC, x.id DESC
		LIMIT 1`, issueID, eventID).Scan(
		&ev.ID, &ev.SessionID, &ev.UserID, &ev.Message, &ev.StackTrace, &breadcrumbs, &deviceInfo,
		&ev.ReleaseVersion, &ev.CreatedAt, &ev.OSLabel, &ev.DeviceLabel, &ev.AppLabel)
	if err != nil {
		return ev, err
	}
	ev.Breadcrumbs, ev.DeviceInfo = breadcrumbs, deviceInfo
	ev.StackTraceResolved = h.newTraceResolver(projectID).resolve(ctx, ev.ReleaseVersion, ev.StackTrace)
	return ev, nil
}

func (h *Handler) mcpGetIssueEvents(ctx context.Context, mc mcpContext, in getIssueEventsIn) (string, error) {
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 50 {
		return "", mcpBadInput("limit must be between 1 and 50")
	}
	ctx, cancel := context.WithTimeout(ctx, mcpDBTimeout)
	defer cancel()
	var exists bool
	if err := h.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issues WHERE id = $1 AND project_id = $2)`,
		in.IssueID, mc.ProjectID).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		return "", errIssueNotFound
	}
	rows, err := h.db.Query(ctx, issueEventContext+`
		SELECT ev.id, ev.created_at, ev.user_id, ev.session_id, ev.device, ev.os, ev.app,
		       x.stack_trace, x.release_version
		FROM ev JOIN issue_events x ON x.id = ev.id
		WHERE ($2 = '' OR ev.os = $2) AND ($3 = '' OR ev.device = $3) AND ($4 = '' OR ev.app = $4)
		ORDER BY ev.created_at DESC, ev.id DESC
		LIMIT $5`, in.IssueID, in.OS, in.Device, in.AppVersion, in.Limit)
	if err != nil {
		return "", err
	}
	events, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (IssueEvent, error) {
		var ev IssueEvent
		return ev, r.Scan(&ev.ID, &ev.CreatedAt, &ev.UserID, &ev.SessionID, &ev.DeviceLabel, &ev.OSLabel, &ev.AppLabel,
			&ev.StackTrace, &ev.ReleaseVersion)
	})
	if err != nil {
		return "", err
	}

	var filters []string
	for _, f := range []struct{ k, v string }{{"os", in.OS}, {"device", in.Device}, {"app_version", in.AppVersion}} {
		if f.v != "" {
			filters = append(filters, f.k+"="+clip(f.v, 40))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Events for issue #%d, newest first", in.IssueID)
	if len(filters) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(filters, ", "))
	}
	fmt.Fprintf(&b, ": %d shown.\n", len(events))
	if len(events) == 0 {
		b.WriteString("\nNo events match these filters.")
		return b.String(), nil
	}
	resolver := h.newTraceResolver(mc.ProjectID)
	for _, ev := range events {
		ev.StackTraceResolved = resolver.resolve(ctx, ev.ReleaseVersion, ev.StackTrace)
		b.WriteString("\n" + formatEventLine(ev))
	}
	return b.String(), nil
}
