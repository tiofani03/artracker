package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type updateIssueIn struct {
	IssueID int64  `json:"issue_id" jsonschema:"The issue id (the #number from list_issues)."`
	Status  string `json:"status" jsonschema:"The new status: open, resolved or ignored."`
}

var updateIssueSchema = mcpSchema[updateIssueIn](func(p map[string]*jsonschema.Schema) {
	p["issue_id"].Minimum = floatPtr(1)
	p["status"].Enum = []any{"open", "resolved", "ignored"}
})

// registerFixTools attaches the update_issue tool and the fix_issue prompt.
func registerFixTools(s *mcp.Server, h *Handler, mc mcpContext) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "update_issue",
		Description: "Change an issue's status. Only call after the developer has explicitly agreed. " +
			"status is open, resolved or ignored; resolving or ignoring also closes the issue's firing new-issue and regression alerts (spike alerts are not touched).",
		InputSchema: updateIssueSchema(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateIssueIn) (*mcp.CallToolResult, any, error) {
		status := strings.ToLower(strings.TrimSpace(in.Status))
		if !validIssueStatus(status) {
			h.logMCPCall(ctx, mc, "update_issue", nil, false)
			return nil, nil, mcpToolError("update_issue", mcpBadInput("status must be open, resolved or ignored"))
		}
		dbctx, cancel := context.WithTimeout(ctx, mcpDBTimeout)
		defer cancel()
		it, err := h.setIssueStatus(dbctx, mc.ProjectID, in.IssueID, status)
		// A foreign or unknown id is never stored: only log ids that were updated.
		var logged *int64
		if err == nil {
			logged = &in.IssueID
		}
		h.logMCPCall(ctx, mc, "update_issue:"+status, logged, err == nil)
		if err != nil {
			return nil, nil, mcpToolError("update_issue", err)
		}
		return mcpText(fmt.Sprintf("Issue #%d is now %s.", it.ID, it.Status)), nil, nil
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "fix_issue",
		Title:       "Fix a crash",
		Description: "Walk through diagnosing and fixing a crash from this project. Without issue_id it picks the open issue with the most affected users.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "issue_id",
			Description: "Issue id to fix (the #number from list_issues). Optional.",
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		var arg string
		if req != nil && req.Params != nil {
			arg = req.Params.Arguments["issue_id"]
		}
		text, issueID, err := h.fixIssuePromptText(ctx, mc, arg)
		h.logMCPCall(ctx, mc, "prompt:fix_issue", issueID, err == nil)
		if err != nil {
			return nil, mcpToolError("fix_issue", err)
		}
		return &mcp.GetPromptResult{
			Description: "Fix a crash",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
		}, nil
	})
}

// fixIssuePromptText builds the prompt. The returned id is non-nil only for an
// issue that belongs to the key's project.
func (h *Handler) fixIssuePromptText(ctx context.Context, mc mcpContext, arg string) (string, *int64, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpDBTimeout)
	defer cancel()

	var id int64
	arg = strings.TrimSpace(arg)
	if arg != "" {
		// Prompt arguments arrive as strings. A given id that is not a positive
		// number is "not found", never a silent fallback to another issue.
		n, err := strconv.ParseInt(arg, 10, 64)
		if err != nil || n <= 0 {
			return fmt.Sprintf("Issue %q was not found in this project. Call list_issues to see the available issues.", clip(arg, 32)), nil, nil
		}
		err = h.db.QueryRow(ctx, `SELECT id FROM issues WHERE id = $1 AND project_id = $2`, n, mc.ProjectID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Sprintf("Issue #%d was not found in this project. Call list_issues to see the available issues.", n), nil, nil
		}
		if err != nil {
			return "", nil, err
		}
	} else {
		rows, err := h.listIssuesForMCP(ctx, mc.ProjectID, listIssuesIn{Status: "open", Sort: "users", Limit: 1})
		if err != nil {
			return "", nil, err
		}
		if len(rows) == 0 {
			return fmt.Sprintf("There are no open crashes in %s right now. Nothing to fix.", mc.ProjectName), nil, nil
		}
		id = rows[0].ID
	}
	return fixIssueText(mc, id), &id, nil
}

// fixIssueText never includes the issue title: it is end-user controlled
// (crash messages come from devices) and text in a user-role prompt carries
// the developer's authority. get_issue returns the title as tool data.
func fixIssueText(mc mcpContext, id int64) string {
	return fmt.Sprintf(`Fix the crash #%[1]d in %[2]s (%[3]s).

Work through these steps in order:
1. Call get_issue with issue_id %[1]d to read the stack trace, the breakdowns and the breadcrumbs. Note the event id and session id it shows.
2. Find the app frames (the ones marked as app code) in the local repository and read the code around them.
3. Call get_session_timeline with that event's session_id and until_event_id set to that event id, to see what the user did before the crash. Use get_issue_events to compare other occurrences if the cause is unclear.
4. Explain the root cause to the developer, citing the files and lines.
5. Propose a fix and show the change. Do not apply it before the developer agrees.
6. After the developer confirms the fix, ask the developer before calling update_issue to mark issue #%[1]d resolved. Never change the status without that explicit yes.`,
		id, mc.ProjectName, mc.Platform)
}
