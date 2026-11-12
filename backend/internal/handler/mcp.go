package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/ingest"
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
	s.AddReceivingMiddleware(mcpRecover(h, mc))
	registerMCPTools(s, h, mc)
	return s
}

// registerMCPTools is the single place tools and prompts are attached to a
// per-request server. Each later task adds one registerXxx(s, h, mc) call.
func registerMCPTools(s *mcp.Server, h *Handler, mc mcpContext) {
	registerIssueTools(s, h, mc)
	registerTimelineTool(s, h, mc)
	registerFixTools(s, h, mc)
	if mcpTestRegister != nil {
		mcpTestRegister(s)
	}
}

// mcpTestRegister lets tests attach extra tools or prompts (for example one
// that panics). Always nil in production.
var mcpTestRegister func(s *mcp.Server)

// mcpPanicMessage is all a client sees when a tool or prompt handler panics.
const mcpPanicMessage = "tool failed; try again"

// mcpRecover keeps a panic in a tool or prompt handler from killing the
// process. The SDK runs every call in its own goroutine (jsonrpc2
// Connection.handleAsync starts `go func(){ c.handler.Handle(...) }`, which
// reaches ServerSession.handle -> handleReceive -> the receiving middleware
// chain), outside the reach of Fiber's recover middleware, so the recover has
// to sit in that chain. Only the panic's type (and a runtime error's message)
// and the stack are logged, never the call's arguments.
func mcpRecover(h *Handler, mc mcpContext) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (res mcp.Result, err error) {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				attrs := []any{"method", method, "project_id", mc.ProjectID, "key_id", mc.KeyID,
					"panic_type", fmt.Sprintf("%T", r), "stack", string(debug.Stack())}
				if re, ok := r.(runtime.Error); ok {
					attrs = append(attrs, "panic", re.Error())
				}
				var tool string
				switch rq := req.(type) {
				case *mcp.CallToolRequest:
					if rq.Params != nil {
						tool = rq.Params.Name
					}
				case *mcp.GetPromptRequest:
					if rq.Params != nil {
						tool = "prompt:" + rq.Params.Name
					}
				}
				tool = clip(tool, 64)
				slog.Error("mcp handler panic", append(attrs, "tool", tool)...)
				if tool != "" {
					h.logMCPCall(ctx, mc, tool, nil, false)
				}
				if method == "tools/call" {
					res, err = &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: mcpPanicMessage}}}, nil
					return
				}
				res, err = nil, errors.New(mcpPanicMessage)
			}()
			return next(ctx, method, req)
		}
	}
}

// Limits applied to /mcp before the request reaches the SDK.
const (
	// mcpMaxBodyBytes caps the request body; real tool calls are tiny.
	mcpMaxBodyBytes = 64 << 10
	// mcpMaxJSONDepth caps object/array nesting: the SDK's decoder is
	// quadratic in nesting depth.
	mcpMaxJSONDepth = 32
	// mcpRequestsPerSecond is the per-project request rate on /mcp. It has its
	// own limiter instance so MCP and ingest cannot starve each other.
	mcpRequestsPerSecond = 10.0
	// mcpBurst is the bucket size (ProjectRateLimiter uses 2x the rate).
	mcpBurst = int(2 * mcpRequestsPerSecond)
)

func newMCPLimiter() *ingest.ProjectRateLimiter {
	return ingest.NewProjectRateLimiter(mcpRequestsPerSecond, 0)
}

// jsonRPCInvalidRequest answers 400 with a JSON-RPC "Invalid Request" error.
func jsonRPCInvalidRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"jsonrpc": "2.0", "id": nil,
		"error": fiber.Map{"code": -32600, "message": msg},
	})
}

// GuardMCP runs after RequireMCPKey (so unauthenticated callers only ever see
// 401) and rejects requests that are too frequent, too large, batched or too
// deeply nested before the SDK parses them.
func (h *Handler) GuardMCP(c *fiber.Ctx) error {
	mc, ok := c.Locals("mcpCtx").(mcpContext)
	if !ok {
		return errInvalidMCPKey
	}
	if h.mcpLimiter != nil {
		if allowed, retryAfter := h.mcpLimiter.AllowRequest(mc.ProjectID); !allowed {
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(retryAfter))
			return fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded")
		}
	}
	body := c.Body()
	if len(body) > mcpMaxBodyBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "request body too large")
	}
	if b := bytes.TrimLeft(body, " \t\r\n"); len(b) > 0 && b[0] == '[' {
		return jsonRPCInvalidRequest(c, "JSON-RPC batch requests are not supported")
	}
	if jsonDepthExceeds(body, mcpMaxJSONDepth) {
		return jsonRPCInvalidRequest(c, fmt.Sprintf("JSON nesting deeper than %d levels", mcpMaxJSONDepth))
	}
	return c.Next()
}

// jsonDepthExceeds reports whether objects/arrays in b nest deeper than max.
// One linear pass; brackets inside strings (including after escapes) are
// skipped. It does not validate the JSON: the SDK does that.
func jsonDepthExceeds(b []byte, max int) bool {
	depth := 0
	inStr, esc := false, false
	for _, ch := range b {
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{', '[':
			depth++
			if depth > max {
				return true
			}
		case '}', ']':
			depth--
		}
	}
	return false
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
