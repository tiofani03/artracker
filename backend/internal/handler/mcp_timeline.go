package handler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type timelineIn struct {
	SessionID    string `json:"session_id" jsonschema:"The session id, as shown by get_issue_events or get_issue."`
	UntilEventID int64  `json:"until_event_id,omitempty" jsonschema:"Optional issue event id (from get_issue_events); the timeline stops at that crash."`
	Limit        int    `json:"limit,omitempty" jsonschema:"How many of the newest lines to keep, 1-200 (default 80)."`
}

var timelineSchema = mcpSchema[timelineIn](func(p map[string]*jsonschema.Schema) {
	p["until_event_id"].Minimum = floatPtr(1)
	p["limit"].Minimum, p["limit"].Maximum = floatPtr(1), floatPtr(200)
})

const (
	timelineDefaultLimit = 80
	timelineMaxLimit     = 200
	timelineMsgMax       = 160
)

var errTimelineUntilNotFound = mcpBadInput("until_event_id was not found in this session")

func registerTimelineTool(s *mcp.Server, h *Handler, mc mcpContext) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_session_timeline",
		Description: "Show what the user did in one session, to reproduce a crash. Chronological lines `+12.3s <label> <text>` " +
			"from the session's telemetry and crashes: tap, rage click, dead click, network (method, endpoint, status), " +
			"error/anr/ui hang, crash, or the raw event name (e.g. screen_view). Pass until_event_id (an event id from " +
			"get_issue_events) to stop at that crash. Keeps the newest `limit` lines (default 80, max 200); messages are truncated.",
		InputSchema: timelineSchema(),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in timelineIn) (*mcp.CallToolResult, any, error) {
		out, err := h.mcpSessionTimeline(ctx, mc, in)
		// The session or event may belong to another project, so no id is stored.
		h.logMCPCall(ctx, mc, "get_session_timeline", nil, err == nil)
		if err != nil {
			return nil, nil, mcpToolError("get_session_timeline", err)
		}
		return mcpText(out), nil, nil
	})
}

type timelineRow struct {
	OffsetMs         float64
	Name, Level, Msg string
	Attrs            map[string]any
	IsCrash          bool
}

func (h *Handler) mcpSessionTimeline(ctx context.Context, mc mcpContext, in timelineIn) (string, error) {
	if !sessionIDRegex.MatchString(in.SessionID) {
		return "", mcpBadInput("session_id must be 8-128 characters of letters, digits, _ or -")
	}
	if in.Limit == 0 {
		in.Limit = timelineDefaultLimit
	}
	if in.Limit < 1 || in.Limit > timelineMaxLimit {
		return "", mcpBadInput("limit must be between 1 and 200")
	}
	if in.UntilEventID < 0 {
		return "", errTimelineUntilNotFound
	}

	var cutTS *time.Time
	if in.UntilEventID > 0 {
		cctx, cancel := context.WithTimeout(ctx, mcpDBTimeout)
		var ts time.Time
		err := h.db.QueryRow(cctx, `
			SELECT e.created_at FROM issue_events e JOIN issues i ON i.id = e.issue_id
			WHERE e.id = $1 AND i.project_id = $2 AND e.session_id = $3`,
			in.UntilEventID, mc.ProjectID, in.SessionID).Scan(&ts)
		cancel()
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errTimelineUntilNotFound
		}
		if err != nil {
			return "", err
		}
		cutTS = &ts
	}

	qctx, cancel := context.WithTimeout(ctx, mcpDBTimeout)
	defer cancel()
	// ord/tie make the order total: same timestamp -> telemetry before crashes.
	rows, err := h.db.Query(qctx, `
		WITH ev AS (
			SELECT event_timestamp AS ts, 0 AS ord, id::text AS tie, name, level, message, attributes, false AS crash
			FROM telemetry_events
			WHERE project_id = $1 AND session_id = $2 AND name <> 'replay_chunk'
			UNION ALL
			SELECT e.created_at, 1, lpad(e.id::text, 20, '0'), i.type, 'error',
			       COALESCE(NULLIF(e.message, ''), i.title), '{}'::jsonb, true
			FROM issue_events e JOIN issues i ON i.id = e.issue_id
			WHERE i.project_id = $1 AND e.session_id = $2
		), t AS (
			SELECT *, EXTRACT(EPOCH FROM ts - min(ts) OVER ())::float8 * 1000 AS off_ms FROM ev
		)
		SELECT off_ms, name, level, message, attributes, crash FROM t
		WHERE $3::timestamptz IS NULL OR ts < $3 OR (ts = $3 AND ord = 0) OR (ts = $3 AND crash AND tie <= lpad($4::bigint::text, 20, '0'))
		ORDER BY ts DESC, ord DESC, tie DESC
		LIMIT $5`, mc.ProjectID, in.SessionID, cutTS, in.UntilEventID, in.Limit)
	if err != nil {
		return "", err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (timelineRow, error) {
		var x timelineRow
		return x, r.Scan(&x.OffsetMs, &x.Name, &x.Level, &x.Msg, &x.Attrs, &x.IsCrash)
	})
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No events recorded for this session.", nil
	}
	slices.Reverse(list)

	var b strings.Builder
	fmt.Fprintf(&b, "Session %s timeline, %d line(s), oldest first", in.SessionID, len(list))
	if in.UntilEventID > 0 {
		fmt.Fprintf(&b, ", up to crash event %d", in.UntilEventID)
	}
	b.WriteString(":\n")
	for _, x := range list {
		b.WriteString("\n" + formatTimelineLine(x))
	}
	return b.String(), nil
}

func formatTimelineLine(x timelineRow) string {
	off := fmt.Sprintf("+%.1fs", math.Max(0, x.OffsetMs)/1000)
	if x.IsCrash {
		return strings.TrimSpace(off + " crash " + clip(x.Msg, timelineMsgMax))
	}
	label, category := classifyMarkerFull(x.Level, x.Name, x.Msg, x.Attrs)
	if category == "network" {
		i := strings.LastIndex(label, " ")
		method, status := label[:i], label[i+1:]
		parts := []string{off, "network", method}
		if ep := jsString(x.Attrs["endpoint"]); ep != "" {
			parts = append(parts, clip(ep, timelineMsgMax))
		}
		parts = append(parts, status)
		return strings.Join(parts, " ")
	}
	text := clip(x.Msg, timelineMsgMax)
	if text == "" && label != x.Name {
		text = clip(x.Name, timelineMsgMax)
	}
	return strings.TrimSpace(off + " " + clip(label, 40) + " " + text)
}

// jsTruthy mirrors JavaScript truthiness for JSON-decoded values.
func jsTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0 && !math.IsNaN(t)
	}
	return true
}

// jsString mirrors String(v) for the JSON scalars attribute values can hold.
func jsString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// classifyMarker is a rule-for-rule Go port of classifyMarker in
// frontend/src/components/ReplayPlayer.tsx (the dashboard's replay markers),
// returning its badgeLabel. Keep both in sync. Differences: the final
// fallback returns the event name (the TS code returns 'event' / 'log' from
// the event kind, which the timeline does not carry).
func classifyMarker(level, name, message string, attrs map[string]any) string {
	label, _ := classifyMarkerFull(level, name, message, attrs)
	return label
}

// classifyMarkerFull also returns the TS `category` ("error", "network",
// "gesture", "other"), used to format network lines.
func classifyMarkerFull(level, name, message string, attrs map[string]any) (label, category string) {
	lvl := strings.ToLower(level)
	n := strings.ToLower(name)
	msg := strings.ToLower(message)
	has := strings.Contains

	if lvl == "error" || lvl == "fatal" ||
		has(n, "error") || has(n, "exception") || has(n, "crash") ||
		has(msg, "error") || has(msg, "uncaught") || has(msg, "exception") || has(msg, "typeerror") {
		if has(n, "anr") || has(msg, "anr") {
			return "anr", "error"
		}
		if has(n, "hang") || has(msg, "hang") {
			return "ui hang", "error"
		}
		return "error", "error"
	}
	if n == "rage_tap" || has(msg, "rage tap") || n == "rage_click" || has(msg, "rage click") {
		return "rage click", "gesture"
	}
	if n == "dead_click" || has(msg, "dead click") {
		return "dead click", "gesture"
	}
	if n == "click" || n == "touch" || jsTruthy(attrs["gesture"]) {
		return "tap", "gesture"
	}
	if jsTruthy(attrs["method"]) || jsTruthy(attrs["endpoint"]) || jsTruthy(attrs["status_code"]) ||
		n == "network" || strings.HasPrefix(n, "http") {
		status := 200.0
		if f, ok := attrs["status_code"].(float64); ok {
			status = f
		} else if f, ok := attrs["status"].(float64); ok {
			status = f
		}
		method := "HTTP"
		if jsTruthy(attrs["method"]) {
			method = jsString(attrs["method"])
		}
		return method + " " + strconv.FormatFloat(status, 'f', -1, 64), "network"
	}
	if lvl == "warn" || lvl == "warning" {
		return "warn", "other"
	}
	if name == "" {
		return "event", "other"
	}
	return name, "other"
}
