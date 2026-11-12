# ARTracker MCP v1: crash to fix

Date: 2026-11-12
Status: design, awaiting approval

## Intent

Let a developer fix production crashes without leaving their AI coding tool
(Claude Code, Cursor, any MCP client). The dashboard can show a crash; the MCP
lets the assistant pull the crash, open the exact file and line in the repo it
is already working in, explain what the user did right before it, propose a fix,
and close the issue afterwards.

Why this and not a dashboard mirror: crashes are daily, urgent and owned by
developers, who already live in MCP-capable editors. Analytics questions are
weekly, owned by PMs who rarely install MCP servers. v1 optimises for something
people will actually use repeatedly.

## Users

- Primary: a developer on a team that ships an app (Android first, web and iOS
  also work) instrumented with the ARTracker SDK, using Claude Code or Cursor.
- Secondary (not served in v1): PMs asking analytics questions.

## Goals

1. Setup in under 60 seconds from the Settings page, with no docs reading.
2. The first question ("what are my top crashes?") returns something useful.
3. A guided "fix this crash" flow that ends with the issue closed.
4. Crash alerts in Telegram point people to that flow.
5. We can measure whether it is used: issues closed via MCP, and keys used in
   two or more distinct weeks.

## Non-goals (v1)

- Analytics tools (funnels, retention, event catalog, project stats).
- Release / symbolication status tools.
- Managing API keys, org members, alert channels or Telegram bots via MCP.
- A local stdio package (npm/binary). Remote HTTP only.
- OAuth login for MCP clients. Key-based only.
- Assigning issues via MCP (keys are not tied to a person yet).
- Any SDK change.

## Anti-goals (failure even if it "works")

- A key for project A can read anything from project B.
- Tools dump huge payloads (200 events, full raw traces, all device JSON) and
  blow the assistant's context. Every tool output is bounded and summarised.
- The assistant closes or mutes issues without the developer agreeing.
- An MCP key can do anything an admin key can (upload artifacts, ingest, manage
  keys). Least privilege.

## How it gets made (decided)

### Transport and hosting
- A remote MCP server using the Streamable HTTP transport, served by the
  existing Go API at `POST {API_BASE}/api/v1/mcp` (same deployment, no new
  service).
- Built with the official Go SDK, `github.com/modelcontextprotocol/go-sdk`.
- Stateless: each request authenticates on its own; plain JSON responses are
  fine (no server-initiated streaming is needed in v1).

### Auth
- New API key type `mcp` with prefix `art_mcp_`, project-scoped, created and
  revoked like other keys (stored hashed in `api_keys`; migration widens the
  `key_type` CHECK).
- Sent as `Authorization: Bearer art_mcp_...`. `X-Artracker-Key` also accepted.
- An `mcp` key can only call the MCP endpoint. Ingest, replay upload, artifact
  upload and `/sdk/validate-admin` must reject it. Public and secret keys must
  not work on `/api/v1/mcp`.
- Missing/invalid/revoked key: HTTP 401 before any MCP handling.
- Who may create and revoke: **Member and above** may create and revoke `mcp`
  keys (decided by the product owner, 2026-11-12), so a developer can connect
  without waiting for an admin. `public_client` and `secret_admin` keys stay
  Admin-only. Listing keys stays Viewer+. The role check therefore depends on
  the key type being created or revoked.

### Tools (all scoped to the key's project)

Outputs are compact text written for an LLM (headings plus short lines), with a
small structured JSON block where IDs are needed for follow-up calls.

1. `list_issues`
   - Inputs: `status` (open | resolved | ignored | all, default open),
     `sort` (users | events | trending | newest, default users),
     `release` (optional app version), `limit` (1-25, default 10).
   - Per issue: id, title, type, Fatal/Non-fatal (same rule as the dashboard,
     `frontend/src/issues/severity.ts`), status, total events, affected users,
     events in last 24h vs previous 24h, first/last seen, top OS version and
     top app version.
   - `trending` = largest increase last 24h vs previous 24h.
2. `get_issue`
   - Inputs: `issue_id`, optional `event_id` (default: latest event).
   - Output: header (title, type, status, counts, first/last seen, version
     range), breakdowns (top 5 each of OS version, device, app version with %),
     the chosen event's context (time, device, OS, app version, user, session),
     its deobfuscated stack trace with app frames marked and framework frames
     collapsed to a count, "Caused by" sections kept, and its last 30
     breadcrumbs. States whether the trace was deobfuscated; if not, says a
     mapping/source map for that version is missing.
   - Reuses the summary from `GET /projects/:id/issues/:id` (commit 8e0fe19)
     and the same symbolication path as `GetIssue`.
3. `get_issue_events`
   - Inputs: `issue_id`, `limit` (1-50, default 20), optional `os`, `device`,
     `app_version` filters (exact label match, labels as in the summary).
   - One line per event: event id, time, user, device, OS, app version,
     session id, first app frame.
4. `get_session_timeline`
   - Inputs: `session_id`, optional `until_event_id` (an issue event; the
     timeline stops at that crash), `limit` (default 80, max 200, newest kept).
   - Chronological lines from `telemetry_events` and `issue_events` for that
     session: time offset, a category label, short text. Long messages
     truncated.
   - Category labels come from a Go port of the dashboard's `classifyMarker`
     (`frontend/src/components/ReplayPlayer.tsx`), kept rule-for-rule
     identical: error / anr / ui hang, rage click, dead click, tap, network
     (with method, endpoint, status), other. `issue_events` rows are labelled
     `crash`. Anything else shows its event `name`, so screen views appear
     under whatever name the SDK sends (e.g. `screen_view`); no new "screen"
     rule is invented in v1. *(Builder-check decision: match the dashboard
     rather than invent categories.)*
5. `update_issue`
   - Inputs: `issue_id`, `status` (open | resolved | ignored).
   - Same effects as the dashboard PATCH (alert incidents get closed exactly as
     `UpdateIssueStatus` does today).
   - Tool description tells the assistant to ask the developer before calling.

### Prompt
- `fix_issue` with optional `issue_id`. In Claude Code it shows up as a slash
  command (`/mcp__artracker-<slug>__fix_issue`). With no id it picks the top open issue
  by affected users. The prompt text walks the assistant through: `get_issue`
  → find the app frames in the local repo → `get_session_timeline` for the
  representative event → explain root cause → propose a fix → after the
  developer confirms the fix, ask before calling `update_issue` to close it.

### Server name: one connection per project
- Keys stay project-scoped (one key = one project). A team with an Android and
  an iOS project adds two MCP servers.
- Each project's server name is `artracker-<project slug>` (e.g.
  `artracker-shop-android`). Snippets, the "Try it" copy and the Telegram line
  all use that name, so the slash command is
  `/mcp__artracker-<slug>__fix_issue`. *(Builder-check decision: per-project
  names instead of multi-project keys; keeps the auth model and the
  "key for A cannot read B" guarantee simple.)*
- The server's MCP `initialize` response reports the project name and platform
  so the assistant knows which app it is looking at.

### Setup UX (Settings → API Keys tab)
- A "Connect to Claude Code" panel: one button creates an `mcp` key and shows,
  once, copy-ready snippets:
  - Claude Code: `claude mcp add --transport http artracker-<slug> <API_URL>/mcp --header "Authorization: Bearer <key>"`
  - Cursor / generic JSON config (`mcpServers` entry with url + headers).
- `<API_URL>` is `VITE_API_BASE` when it is absolute, otherwise
  `window.location.origin + '/api/v1'`. Production serves the API on the same
  origin (nginx proxies `/api/` in `frontend/nginx.conf`), so no new config is
  needed. *(Builder-check decision.)*
- Then a "Try it" line: ask "What are my top crashes?" or run
  `/mcp__artracker-<slug>__fix_issue`.
- Existing MCP keys appear in the key list with type "MCP" and can be revoked.
- English copy, existing `dash-*` components, consistent with the Settings
  revamp (commit 342e3ad).

### Telegram alert hook
- When a project has at least one active `mcp` key, issue alert messages
  (`FormatFire` for new issue / regression / spike) get one extra line:
  `Fix with Claude: /mcp__artracker-<slug>__fix_issue <issue_id>`.
- No line when the project has no active MCP key, and none in "minimal" mode.

### Usage measurement
- New table `mcp_tool_calls(id, project_id, api_key_id, tool, issue_id NULL,
  ok bool, created_at)`, one row per tool call (and per prompt fetch). No
  arguments or outputs stored.
- "Issues closed via MCP" = `update_issue` calls with status resolved and ok.
- No dashboard UI for this in v1; a SQL query in the plan's verification is
  enough.

## Constraints

- Go 1.25, Fiber v2 (fasthttp). The SDK handler is `net/http`; bridging via
  Fiber's adaptor or mounting differently is the builder's call, as long as the
  endpoint lives on the existing API process and port.
- DB tests run only against `artracker_test`
  (`TEST_DATABASE_URL='postgres://artracker:artracker@localhost:5433/artracker_test?sslmode=disable' go test -p 1 ./...`),
  never the dev DB.
- Device labels and severity must match the dashboard exactly (same fallbacks
  as `issueEventContext` in `backend/internal/handler/issues.go`).
- Tool output budget: aim for under ~4k tokens per call at default limits.

## Left to the builder

- Package layout (e.g. `backend/internal/mcp`), how query code is shared with
  the existing handlers (extract shared query functions rather than calling
  HTTP handlers internally).
- Exact text formatting of tool outputs, within the budget above.
- How trending is computed efficiently.
- Rate limiting for the MCP endpoint (reuse an existing limiter if simple).
- Whether the key-creation endpoint gets a new body value (`key_type: "mcp"`)
  or a dedicated route.
