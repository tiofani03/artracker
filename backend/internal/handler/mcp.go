package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

// mcpContext is the project an MCP key is bound to. Every tool reads its
// project from here, never from tool input, so a key cannot reach another
// project.
type mcpContext struct {
	KeyID, ProjectID            int64
	ProjectName, Slug, Platform string
}

var errInvalidMCPKey = fiber.NewError(fiber.StatusUnauthorized, "invalid mcp key")

// RequireMCPKey authenticates an mcp key from Authorization: Bearer or
// X-Artracker-Key. It reads api_keys directly (not the ingest key cache) so a
// revoked key is rejected on the very next request.
func (h *Handler) RequireMCPKey(c *fiber.Ctx) error {
	plain, ok := strings.CutPrefix(c.Get("Authorization"), "Bearer ")
	if !ok || plain == "" {
		plain = c.Get("X-Artracker-Key")
	}
	if plain == "" {
		return errInvalidMCPKey
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	var mc mcpContext
	err := h.db.QueryRow(ctx, `
		SELECT k.id, k.project_id, p.name, p.slug, p.platform
		FROM api_keys k JOIN projects p ON p.id = k.project_id
		WHERE k.key_hash = $1 AND k.is_active AND k.key_type = 'mcp'`,
		auth.HashKey(plain)).Scan(&mc.KeyID, &mc.ProjectID, &mc.ProjectName, &mc.Slug, &mc.Platform)
	if errors.Is(err, pgx.ErrNoRows) {
		return errInvalidMCPKey
	}
	if err != nil {
		return err
	}
	c.Locals("mcpCtx", mc)
	return c.Next()
}

// mcpHTTPOptions: stateless because every request authenticates on its own
// and the server is rebuilt per request for the key's project; JSON responses
// because nothing streams from server to client in v1 (and the fasthttp
// adaptor buffers the response anyway).
var mcpHTTPOptions = &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}

// MCP serves the Streamable HTTP transport for the authenticated key's project.
func (h *Handler) MCP(c *fiber.Ctx) error {
	mc, ok := c.Locals("mcpCtx").(mcpContext)
	if !ok {
		return errInvalidMCPKey
	}
	srv := newMCPServer(h, mc)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, mcpHTTPOptions)
	return adaptor.HTTPHandler(handler)(c)
}

// MCPMethodNotAllowed answers GET and DELETE on /mcp: the stateless transport
// has no standalone SSE stream and no session to delete.
func (h *Handler) MCPMethodNotAllowed(c *fiber.Ctx) error {
	c.Set(fiber.HeaderAllow, fiber.MethodPost)
	return fiber.NewError(fiber.StatusMethodNotAllowed, "method not allowed")
}

func newMCPServer(h *Handler, mc mcpContext) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "artracker-" + mc.Slug, Version: "1"}, &mcp.ServerOptions{
		Instructions: fmt.Sprintf(
			"ARTracker crash and error data for the project %q (slug %s, platform %s). "+
				"Every tool is scoped to this project only. Start with list_issues, open one with get_issue, "+
				"and mark it resolved with update_issue once a fix is in.",
			mc.ProjectName, mc.Slug, mc.Platform),
		// Advertise tools even before any are registered so tools/list always works.
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	registerMCPTools(s, h, mc)
	return s
}

// registerMCPTools is the single place tools and prompts are attached to a
// per-request server. Each later task adds one registerXxx(s, h, mc) call.
func registerMCPTools(s *mcp.Server, h *Handler, mc mcpContext) {
	registerIssueTools(s, h, mc)
	registerTimelineTool(s, h, mc)
	// Tasks 5-7 register list_issues, get_issue, get_issue_events,
	// get_session_timeline, update_issue and the fix_issue prompt here.
}

// logMCPCall records one tool call or prompt fetch. Tool names: the tool's own
// name, "update_issue:<status>" for status changes, "prompt:fix_issue" for the
// prompt. It never fails the call: errors are only logged, and the insert runs
// detached from the request's cancellation with its own short timeout.
func (h *Handler) logMCPCall(ctx context.Context, mc mcpContext, tool string, issueID *int64, ok bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if _, err := h.db.Exec(ctx,
		`INSERT INTO mcp_tool_calls(project_id, api_key_id, tool, issue_id, ok) VALUES ($1, $2, $3, $4, $5)`,
		mc.ProjectID, mc.KeyID, tool, issueID, ok); err != nil {
		slog.Warn("mcp call log failed", "err", err, "project_id", mc.ProjectID, "tool", tool)
	}
}
