# Alerting MVP (Telegram) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the browser-evaluated fake alert rules with server-side `new_issue` / `regression` / `issue_spike` alerts delivered to Telegram, plus an Alerts page, sidebar badge and overview cell backed by real data.

**Architecture:** A new Go package `backend/internal/alerts` owns the engine: ingest's `IssueService` emits an `IssueSignal` on a new or regressed issue, `Service.OnIssue` writes an incident into an outbox table, a 5 s dispatcher sends pending incidents through a small Telegram Bot API client, a 60 s evaluator detects spikes and closes incidents, and a long-poll loop binds Telegram chats via single-use deep-link codes. Handlers in `internal/handler/alerts.go` expose rules/channels/incidents. The React dashboard replaces `ALERT_DEFINITIONS` with an `AlertsView` component.

**Tech Stack:** Go 1.25, Fiber v2, pgx v5, PostgreSQL; React 19 + TypeScript + Vitest. Telegram Bot API over `net/http` (no SDK dependency).

**Spec:** `docs/superpowers/specs/2026-11-11-alerting-telegram-design.md`

## Global Constraints

- Rule kinds are exactly `new_issue`, `regression`, `issue_spike`. Channel kind is exactly `telegram`.
- Defaults: `enabled = true`, `environment = 'production'`, `spike_multiplier = 5`, `spike_min_count = 20`, `spike_window_minutes = 60`, `cooldown_minutes = 60`. `environment = ''` means every environment.
- Spike: `current ≥ spike_min_count AND current ≥ spike_multiplier × max(baseline, 1)`; `baseline = events in (now−W−24h, now−W] × W/1440`. Resolve when the condition is false and `fired_at ≤ now − W`.
- Link code: 16 chars from `[a-z0-9]` (crypto/rand), valid 15 minutes, single use. Links: `https://t.me/<bot>?startgroup=<code>` and `https://t.me/<bot>?start=<code>`.
- Dispatcher every 5 s; evaluator every 60 s; digest when > 3 pending fire notifications for one project in a tick (first 5 titles + "+N more"); `notify_state = 'failed'` after 5 attempts.
- Messages: `parse_mode=HTML`, `disable_web_page_preview=true`, links in the text (no inline keyboards), every user string HTML-escaped, titles cut to 200 characters (runes).
- Emoji: 🆕 new_issue, 🔁 regression, 📈 issue_spike, ✅ spike recovered, 🚨 digest.
- Deep links: `{PUBLIC_URL}/dashboard/issues/{issueID}?org={orgID}&project={projectID}`. Replay link `{PUBLIC_URL}/dashboard/replays?session={sid}&org={orgID}&project={projectID}` only when a `replay_sessions` row exists.
- Env: `TELEGRAM_BOT_TOKEN` (empty = Telegram off), `TELEGRAM_API_BASE` (default `https://api.telegram.org`), `ALERTS_WORKER` (default `true`).
- Ingest must never block on Telegram or fail because alerting failed. Log and continue.
- Role gates: reading rules, channels and incidents needs `viewer`; any write, link code or test message needs `admin`.

## Review Focus

1. **The Alerts service is absent (no DB alerts wiring, or the tests' `&IssueService{db}`)**: ingest must keep grouping errors and must not panic on a typed-nil interface. → Task 2 test `TestIssueServiceWithoutAlerterStillGroups`.
2. **A release burst creates 40 new issues in seconds**: the chat gets one 🚨 digest, not 40 messages. → Task 5 test `TestDispatchDigestsBursts`.
3. **The bot was kicked from one of two groups (403)**: that channel is disabled with a reason, the other still receives the message, and the incident counts as sent. → Task 5 test `TestDispatchForbiddenDisablesOnlyThatChannel`.
4. **A link code is reused, expired, or arrives as `/start@ArtBot code`**: it binds exactly once; an expired or used code gets a "link expired" reply and binds nothing. → Task 7 tests.
5. **An issue title contains `<`, `>`, `&` or is 5 000 chars**: it is escaped and truncated so Telegram does not reject the HTML (which would silently drop every alert). → Task 4 test `TestFormatEscapesAndTruncates`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `backend/internal/db/migrations/0011_alerts.sql` | tables, seeded default rules, `issue_events.environment` |
| `backend/internal/alerts/model.go` | `RuleKind`, `Rule`, `Incident`, `SpikeDetail`, `IssueSignal`, `SeedRulesSQL` |
| `backend/internal/alerts/telegram.go` | Bot API client + typed errors + `Update` types |
| `backend/internal/alerts/format.go` | HTML message builders |
| `backend/internal/alerts/service.go` | `Service`, `OnIssue` |
| `backend/internal/alerts/dispatch.go` | outbox dispatcher |
| `backend/internal/alerts/evaluate.go` | spike detection + incident closing |
| `backend/internal/alerts/link.go` | link codes, `HandleUpdate`, `PollUpdates` |
| `backend/internal/alerts/runner.go` | `Run` loops, `SendTest` |
| `backend/internal/alerts/testutil_test.go` | DB + fixtures for this package |
| `backend/internal/handler/issues.go` | upsert reports new/regression, reopen resolved |
| `backend/internal/handler/alerts.go` (+`_test.go`) | HTTP API |
| `backend/internal/handler/{project,nav_counts,routes,openapi,health}.go` | seed rules, badge count, routes, docs, `WithAlerts` |
| `backend/internal/config/config.go`, `backend/cmd/api/main.go`, `.env.example`, `README.md` | config + wiring |
| `frontend/src/api.ts` | alert types + `alertApi`, `NavCounts.firing_alerts` |
| `frontend/src/utils/deepLink.ts` (+test) | `?org=&project=` selection |
| `frontend/src/components/DashboardShell.tsx` | badge + deep-link selection |
| `frontend/src/components/alerts/alertFormat.ts` (+test) | pure labels/validation |
| `frontend/src/components/alerts/AlertsView.tsx` | Alerts page UI |
| `frontend/src/pages/DashboardPage.tsx` | remove fake rules, mount `AlertsView`, overview cell |

Test commands use `TDB=postgres://artracker:artracker@localhost:5433/artracker?sslmode=disable` (`make db-up` first).

---

### Task 1: Migration + default rules

**Files:**
- Create: `backend/internal/db/migrations/0011_alerts.sql`
- Create: `backend/internal/alerts/model.go`
- Modify: `backend/internal/handler/project.go` (`CreateProject`, after the INSERT)
- Test: `backend/internal/handler/alerts_test.go`

**Interfaces:**
- Produces: tables from spec §5; `alerts.RuleKind` with consts `KindNewIssue`, `KindRegression`, `KindIssueSpike`; `alerts.AllKinds []RuleKind`; `alerts.SeedRulesSQL` (one parameter `$1` = project id, `INSERT … ON CONFLICT DO NOTHING`); structs below (JSON snake_case tags).

```go
type Rule struct {
    ProjectID int64; Kind RuleKind; Enabled bool; Environment string
    SpikeMultiplier float64; SpikeMinCount, SpikeWindowMinutes, CooldownMinutes int; UpdatedAt time.Time
}
type SpikeDetail struct { Count int `json:"count"`; Baseline float64 `json:"baseline"`; WindowMinutes int `json:"window_minutes"` }
type Incident struct {
    ID, ProjectID, IssueID int64; RuleKind RuleKind; Status string // firing|resolved
    Title, Environment, ReleaseVersion, SessionID string
    Spike *SpikeDetail; FiredAt time.Time; ResolvedAt *time.Time; NotifyState string
}
type IssueSignal struct { ProjectID, IssueID int64; Kind RuleKind; Title, Environment, ReleaseVersion, SessionID string }
```

- [ ] **Step 1: Write the failing tests** in `alerts_test.go`:
  - `TestCreateProjectSeedsAlertRules`: an admin POSTs `/api/v1/orgs/{org}/projects`; then `count("SELECT count(*) FROM alert_rules WHERE project_id=$pid AND enabled AND environment='production'") == 3`.
  - `TestMigrationColumns`: `SELECT environment FROM issue_events LIMIT 0` succeeds; inserting two `alert_incidents` rows with `status='firing'` and the same `(project_id, rule_kind, issue_id)` fails with a unique violation (`pgconn.PgError.Code == "23505"`); the same pair with the first set to `resolved` succeeds.
- [ ] **Step 2: Run them to verify they fail.** `cd backend && TEST_DATABASE_URL=$TDB go test ./internal/handler -run 'TestCreateProjectSeedsAlertRules|TestMigrationColumns' -v`. Expected: FAIL (relation `alert_rules` does not exist).
- [ ] **Step 3: Write the migration.** Columns and checks as in spec §5. Column defaults hold the Global Constraints values. `kind CHECK IN (…)`, `status CHECK IN ('firing','resolved')`, `notify_state CHECK IN ('pending','sent','failed','skipped') DEFAULT 'pending'`, `recovery_notify_state CHECK IN ('none','pending','sent','failed','skipped') DEFAULT 'none'`, `notify_attempts INT DEFAULT 0`, `next_attempt_at TIMESTAMPTZ DEFAULT now()`, `detail JSONB DEFAULT '{}'`. Every FK to `projects` / `issues` uses `ON DELETE CASCADE`; `created_by` uses `ON DELETE SET NULL`. Indexes: the partial unique `alert_incidents_one_firing`, `(project_id, status, fired_at DESC)`, and `(next_attempt_at) WHERE notify_state='pending' OR recovery_notify_state='pending'`. Finish with `ALTER TABLE issue_events ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT ''` and a seed for existing projects: `INSERT INTO alert_rules(project_id, kind) SELECT p.id, k FROM projects p CROSS JOIN unnest(ARRAY['new_issue','regression','issue_spike']) k ON CONFLICT DO NOTHING`.
- [ ] **Step 4: Write `model.go`** and run `SeedRulesSQL` in `CreateProject` right after the project INSERT. A seeding error returns 500, because a project without rules would never alert.
- [ ] **Step 5: Run the tests to verify they pass**, then the whole suite: `TEST_DATABASE_URL=$TDB go test ./...`. Expected: PASS.
- [ ] **Step 6: Commit** `feat(alerts): schema and default alert rules per project`.

---

### Task 2: Ingest reports new and regressed issues

**Files:**
- Modify: `backend/internal/handler/issues.go` (`IssueService`, `recordError`, `IssueService()`)
- Modify: `backend/internal/handler/health.go` (`Handler.alertsSvc *alerts.Service`, `func (h *Handler) WithAlerts(s *alerts.Service) *Handler`)
- Test: `backend/internal/handler/issues_test.go`

**Interfaces:**
- Consumes: `alerts.IssueSignal`, `alerts.KindNewIssue`, `alerts.KindRegression` (Task 1).
- Produces: `type IssueAlerter interface { OnIssue(ctx context.Context, sig alerts.IssueSignal) error }`; `IssueService.alerts IssueAlerter`; `func (s *IssueService) WithAlerter(a IssueAlerter) *IssueService`. `h.IssueService()` calls `WithAlerter` **only when `h.alertsSvc != nil`**, so it never stores a typed nil. `issue_events.environment` is filled from `r.Environment`.

- [ ] **Step 1: Write the failing tests** with a `fakeAlerter{ sigs []alerts.IssueSignal }`:
  - `TestIssueSignalsNewOnce`: two identical error rows produce exactly one signal, `Kind == KindNewIssue`, carrying `Title`, `Environment == "production"`, `ReleaseVersion == "1.2.3"`, `SessionID == "sess_1"`.
  - `TestIssueRegressionReopens`: after the first row, `UPDATE issues SET status='resolved'`; the next row yields a `KindRegression` signal and `status == 'open'`.
  - `TestIgnoredIssueStaysSilent`: status `ignored` → no signal, and the status stays `ignored`.
  - `TestIssueServiceWithoutAlerterStillGroups`: `New(pool, cfg, …).IssueService().ProcessRows` (no `WithAlerts`) groups two rows into `count == 2` without a panic.
  - `TestIssueEventEnvironmentStored`: a row with `Environment: "staging"` → `issue_events.environment == 'staging'`.
- [ ] **Step 2: Run them to verify they fail.** `TEST_DATABASE_URL=$TDB go test ./internal/handler -run 'TestIssue' -v`. Expected: FAIL (`WithAlerter` undefined).
- [ ] **Step 3: Implement.** Replace the upsert with a statement that also reports what changed. The `prev` CTE reads the snapshot taken before the upsert:

```sql
WITH prev AS (SELECT status FROM issues WHERE project_id = $1 AND fingerprint = $2),
up AS (
  INSERT INTO issues(project_id, fingerprint, title, type, status, first_seen, last_seen, count)
  VALUES ($1, $2, $3, $4, 'open', now(), now(), 1)
  ON CONFLICT (project_id, fingerprint) DO UPDATE
    SET last_seen = now(), count = issues.count + 1,
        status = CASE WHEN issues.status = 'resolved' THEN 'open' ELSE issues.status END
  RETURNING id, (xmax = 0) AS inserted)
SELECT up.id, up.inserted, COALESCE((SELECT status FROM prev), '') FROM up
```

  Emit `KindNewIssue` when `inserted`, and `KindRegression` when `prev == "resolved"`. Call `OnIssue` after the `issue_events` insert. An `OnIssue` error is logged with `slog.Error` and does not change the return value.
- [ ] **Step 4: Run the tests to verify they pass**, including the existing `TestIssueServiceGroupsRepeatedErrors`.
- [ ] **Step 5: Commit** `feat(issues): reopen resolved issues and emit alert signals`.

---

### Task 3: Telegram Bot API client

**Files:**
- Create: `backend/internal/alerts/telegram.go`
- Test: `backend/internal/alerts/telegram_test.go` (an `httptest.Server` stands in for the Bot API)

**Interfaces:**
- Produces:

```go
type Notifier interface { SendMessage(ctx context.Context, chatID int64, html string) error }
func NewTelegram(token, apiBase string, hc *http.Client) *Telegram // hc nil → 40 s timeout client
func (t *Telegram) SendMessage(ctx context.Context, chatID int64, html string) error
func (t *Telegram) GetMe(ctx context.Context) (username string, err error)
func (t *Telegram) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error)
type RetryAfterError struct{ Seconds int }      // 429, parameters.retry_after
type MigratedError struct{ NewChatID int64 }    // 400 with parameters.migrate_to_chat_id
var ErrChatForbidden = errors.New(...)          // 403
var ErrConflict = errors.New(...)               // 409 (another poller)
type APIError struct{ Code int; Description string }
type Update struct { UpdateID int64 `json:"update_id"`; Message *Message `json:"message"`; MyChatMember *ChatMemberUpdated `json:"my_chat_member"` }
type Message struct { Chat Chat `json:"chat"`; Text string `json:"text"` }
type Chat struct { ID int64 `json:"id"`; Type, Title, Username string; FirstName string `json:"first_name"` }
type ChatMemberUpdated struct { Chat Chat `json:"chat"`; NewChatMember struct{ Status string `json:"status"` } `json:"new_chat_member"` }
```

- [ ] **Step 1: Write the failing tests**:
  - `TestSendMessagePostsHTML`: the server sees `POST /bot<token>/sendMessage` with JSON `chat_id`, `text`, `parse_mode:"HTML"`, `disable_web_page_preview:true`.
  - `TestSendMessageErrorMapping`: a table of server replies → `errors.As` matches: `{429, retry_after:7}` → `*RetryAfterError{7}`; `{400, migrate_to_chat_id:-100123}` → `*MigratedError{-100123}`; 403 → `ErrChatForbidden`; 409 → `ErrConflict`; `{400 "Bad Request: can't parse entities"}` → `*APIError{400, …}`.
  - `TestGetUpdatesPassesOffsetAndAllowedUpdates`: the query has `offset=42`, `timeout=30`, `allowed_updates=["message","my_chat_member"]`; the reply decodes into `[]Update`.
  - `TestGetMe`: it returns `result.username`.
- [ ] **Step 2: Run them to verify they fail.** `go test ./internal/alerts -run Telegram -v`.
- [ ] **Step 3: Implement.** Every call decodes the `{ok, result, error_code, description, parameters}` envelope. Never log the token or the full URL.
- [ ] **Step 4: Run the tests to verify they pass.**
- [ ] **Step 5: Commit** `feat(alerts): telegram bot api client`.

---

### Task 4: Message formatting + `OnIssue`

**Files:**
- Create: `backend/internal/alerts/format.go`, `backend/internal/alerts/service.go`, `backend/internal/alerts/testutil_test.go`
- Test: `backend/internal/alerts/format_test.go`, `backend/internal/alerts/service_test.go`

**Interfaces:**
- Consumes: Task 1 types, `Notifier` (Task 3).
- Produces:

```go
type MessageContext struct { ProjectName string; OrgID, ProjectID int64; PublicURL string; HasReplay bool }
func FormatFire(inc Incident, mc MessageContext) string
func FormatRecovered(inc Incident, mc MessageContext) string
func FormatDigest(incs []Incident, mc MessageContext) string
func FormatTest(mc MessageContext) string
func NewService(db *pgxpool.Pool, n Notifier, publicURL string) *Service // n may be nil (Telegram off)
func (s *Service) WithBotUsername(u string) *Service
func (s *Service) TelegramConfigured() bool // n != nil && botUsername != ""
func (s *Service) BotUsername() string
func (s *Service) OnIssue(ctx context.Context, sig IssueSignal) error
```

  `testutil_test.go`: `testDB(t)` (the same as the handler one; it truncates `users, organizations, projects … CASCADE`), `seedProject(t, pool) (orgID, projectID int64)`, `seedIssue(t, pool, projectID int64, status string) int64`, and `fakeNotifier{ sent []sentMsg; errFor map[int64]error }`.

- [ ] **Step 1: Write the failing tests**:
  - `TestFormatFireNewIssue`: the output contains `🆕 New issue · Checkout Web`, the title, `production · release 1.2.3`, and `href="http://app/dashboard/issues/9?org=1&amp;project=2"`. With `HasReplay` it also contains `/dashboard/replays?session=sess_1`; without it, no `replays` substring.
  - `TestFormatSpikeAndRecovered`: 📈 output contains `142 events in 60 min, baseline 6`; recovered starts with `✅`.
  - `TestFormatDigest`: 7 incidents → starts `🚨 7 new alerts · Checkout Web`, lists exactly 5 titles, contains `+2 more` and `/dashboard/alerts?org=1&amp;project=2`.
  - `TestFormatEscapesAndTruncates`: the title `<b>x</b> & ` + 5000×`a` → contains `&lt;b&gt;x&lt;/b&gt; &amp;`, no raw `<b>x`, and the title part is ≤ 200 runes (ending in `…`).
  - `TestOnIssueCreatesPendingIncident`: an enabled `new_issue` rule (env `production`) and a signal with env `production` → 1 incident, `status='firing'`, `notify_state='pending'`.
  - `TestOnIssueRespectsRule`: the rule disabled → 0 incidents; rule env `production` + signal env `staging` → 0; rule env `''` + `staging` → 1.
  - `TestOnIssueDedupsFiring`: the same regression signal twice → 1 incident and no error (the unique violation is swallowed via `ON CONFLICT DO NOTHING` on the partial index).
- [ ] **Step 2: Run them to verify they fail.** `TEST_DATABASE_URL=$TDB go test ./internal/alerts -run 'Format|OnIssue' -v`.
- [ ] **Step 3: Implement** with `html.EscapeString` for every interpolated value. `OnIssue` is a single `INSERT … SELECT … FROM alert_rules WHERE project_id=$1 AND kind=$2 AND enabled AND (environment='' OR environment=$env) ON CONFLICT (project_id, rule_kind, issue_id) WHERE status='firing' DO NOTHING`.
- [ ] **Step 4: Run the tests to verify they pass.**
- [ ] **Step 5: Commit** `feat(alerts): incident creation and telegram message format`.

---

### Task 5: Outbox dispatcher

**Files:**
- Create: `backend/internal/alerts/dispatch.go`
- Test: `backend/internal/alerts/dispatch_test.go`

**Interfaces:**
- Consumes: `FormatFire`, `FormatRecovered`, `FormatDigest`, `Notifier`, typed errors.
- Produces: `func (s *Service) DispatchPending(ctx context.Context, now time.Time) error`.

Algorithm: select incidents where (`notify_state='pending'` OR `recovery_notify_state='pending'`) AND `next_attempt_at ≤ now`, grouped by project, joined with `projects` (name, org_id). When the service has no notifier (Telegram off), mark everything pending → `skipped` and return. Per project, take enabled channels. With no channels, mark pending → `skipped`. With more than 3 pending fire notifications, send one `FormatDigest`; otherwise send one `FormatFire` per incident and `FormatRecovered` for pending recoveries. `HasReplay` = `EXISTS replay_sessions(project_id, id=session_id)`. Per send error: `*RetryAfterError` → keep pending, `next_attempt_at = now + Seconds`, stop this project for the tick; `*MigratedError` → `UPDATE alert_channels SET telegram_chat_id` and retry once; `ErrChatForbidden` → `enabled=false, disabled_reason='bot was removed from the chat'`; any other error → record `last_error`. An incident counts as `sent` when at least one channel succeeded. Otherwise `notify_attempts+1`, `next_attempt_at = now + 30s × attempts`, and `failed` once attempts reach 5. A successful send sets `last_sent_at`, `last_error=''`.

- [ ] **Step 1: Write the failing tests** (with `fakeNotifier`; insert channels and incidents directly):
  - `TestDispatchSendsAndMarksSent`: 1 incident, 1 channel → 1 message to that chat id; `notify_state='sent'`.
  - `TestDispatchDigestsBursts`: 40 pending → exactly 1 message per channel, starting `🚨 40 new alerts`; all 40 `sent`.
  - `TestDispatchForbiddenDisablesOnlyThatChannel`: channels A (403) and B (ok) → A `enabled=false` with a non-empty `disabled_reason`, B received the message, the incident is `sent`.
  - `TestDispatchRetryAfterKeepsPending`: 429 `{Seconds: 7}` → still `pending`, `next_attempt_at = now+7s`; a second call at `now+1s` sends nothing.
  - `TestDispatchMigratedChatUpdatesID`: the first send returns `MigratedError{-100999}` → the channel's chat id becomes `-100999` and the message was sent to it.
  - `TestDispatchNoChannelsSkips`: → `skipped`, 0 messages.
  - `TestDispatchWithoutNotifierSkips`: `NewService(pool, nil, …)` with 1 pending incident and 1 channel → `skipped`, no panic.
  - `TestDispatchGivesUpAfterFiveAttempts`: a generic error on every call, run 5 times with `now` advanced 1 h each → `failed`.
  - `TestDispatchSendsRecovery`: `recovery_notify_state='pending'` → the message starts `✅`, the state becomes `sent`.
- [ ] **Step 2: Run them to verify they fail.** `go test ./internal/alerts -run Dispatch -v`.
- [ ] **Step 3: Implement `DispatchPending`.**
- [ ] **Step 4: Run the tests to verify they pass.**
- [ ] **Step 5: Commit** `feat(alerts): outbox dispatcher with digest and telegram error handling`.

---

### Task 6: Spike evaluator + incident closing

**Files:**
- Create: `backend/internal/alerts/evaluate.go`
- Test: `backend/internal/alerts/evaluate_test.go`

**Interfaces:**
- Produces: `func (s *Service) Evaluate(ctx context.Context, now time.Time) error`. Every SQL statement takes `now` as a parameter (no `now()`) so the tests are deterministic.

Behaviour, per project with an enabled `issue_spike` rule:
1. For open issues with `last_seen > now − W`, count `issue_events` in both windows. The environment filter is `(rule.environment = '' OR e.environment = rule.environment OR e.environment = '')`.
2. Fire: `INSERT` an incident with `rule_kind='issue_spike'`, `detail = SpikeDetail`, and title / env / release / session from the latest event in the window, unless a firing incident exists (partial unique) or a resolved spike incident for that issue has `resolved_at > now − cooldown`.
3. Resolve: firing spike incidents whose condition is now false and `fired_at ≤ now − W` → `status='resolved', resolved_at=now, recovery_notify_state='pending'`.
4. For all projects: firing `new_issue` / `regression` incidents whose issue status ≠ `open` → `resolved`, with `recovery_notify_state` left `none`.

- [ ] **Step 1: Write the failing tests** (`addEvents(t, pool, issueID, n, at time.Time, env)` helper; fixed `now`):
  - `TestSpikeFires`: baseline 24 events spread over the previous day (avg 1/window), 25 in the last 60 min → 1 firing incident, `detail.count == 25`.
  - `TestSpikeBelowMinCount`: 0 baseline, 19 current → none.
  - `TestSpikeBelowMultiplier`: baseline avg 10/window (240 over the day), 40 current → none (40 < 50).
  - `TestSpikeEnvironmentFilter`: 25 current in `staging`, rule env `production` → none; legacy rows with env `''` count.
  - `TestSpikeResolvesAfterWindow`: it fires at `now`; at `now+30m` with 0 current it stays firing; at `now+61m` it is resolved with `recovery_notify_state='pending'`.
  - `TestSpikeCooldown`: resolved at T; the spike recurs at T+30m (cooldown 60) → no new incident; at T+61m → a new incident.
  - `TestEvaluateClosesResolvedIssueIncidents`: a firing `new_issue` incident on an issue set `resolved` → `resolved`, `recovery_notify_state='none'`.
  - `TestSpikeIgnoresIgnoredIssues`: issue `ignored` with 100 current → none.
- [ ] **Step 2: Run them to verify they fail.** `go test ./internal/alerts -run 'Spike|Evaluate' -v`.
- [ ] **Step 3: Implement `Evaluate`.**
- [ ] **Step 4: Run the tests to verify they pass.**
- [ ] **Step 5: Commit** `feat(alerts): issue spike evaluator and incident auto-resolve`.

---

### Task 7: Chat linking (link codes + update handling)

**Files:**
- Create: `backend/internal/alerts/link.go`
- Test: `backend/internal/alerts/link_test.go`

**Interfaces:**
- Produces:

```go
type LinkCode struct { Code string `json:"code"`; GroupLink string `json:"group_link"`; PrivateLink string `json:"private_link"`; ExpiresAt time.Time `json:"expires_at"` }
var ErrTelegramDisabled = errors.New("telegram is not configured")
func (s *Service) CreateLinkCode(ctx context.Context, projectID, userID int64) (LinkCode, error)
func (s *Service) HandleUpdate(ctx context.Context, u Update) error
type UpdateSource interface { GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) }
func (s *Service) PollUpdates(ctx context.Context, src UpdateSource) // returns when ctx is done
```

`HandleUpdate`: the message text matches `^/start(@\w+)?\s+([a-z0-9]{16})$`. When the code is found, unused and unexpired, mark it used (`UPDATE … WHERE used_at IS NULL AND expires_at > now RETURNING project_id`, which makes it single-use under races) and upsert `alert_channels(project_id, telegram_chat_id, title, chat_type)`. The title is the chat title, else `@username`, else the first name. Then reply `✅ Connected to <b>{project}</b>. Alerts for this project will arrive here.` Otherwise reply `⚠️ This link has expired or was already used. Generate a new one in Artracker → Alerts.` A plain `/start` without a code gets no reply. `my_chat_member` with status `left` or `kicked` → `enabled=false, disabled_reason='bot was removed from the chat'` on every channel with that chat id. `PollUpdates` keeps `offset = lastUpdateID+1` in memory, calls `GetUpdates(…, 30)`, and on any error (including `ErrConflict`) logs and sleeps 30 s (5 s for other errors), honouring ctx.

- [ ] **Step 1: Write the failing tests**:
  - `TestCreateLinkCode`: the code matches `^[a-z0-9]{16}$`, `GroupLink == "https://t.me/ArtBot?startgroup="+code`, `PrivateLink` uses `?start=`, `ExpiresAt ≈ now+15m`. With Telegram off → `ErrTelegramDisabled`.
  - `TestHandleUpdateBindsGroup`: `/start@ArtBot <code>` from a chat `{id:-100, type:"supergroup", title:"Eng"}` → 1 channel (title `Eng`), the code is used, and the reply was sent to `-100` containing the project name.
  - `TestHandleUpdateCodeSingleUse`: the same code from a second chat → no new channel; the reply contains `expired`.
  - `TestHandleUpdateExpiredCode`: `expires_at` in the past → no channel; the reply contains `expired`.
  - `TestHandleUpdateSameChatTwoProjects`: codes from projects P1 and P2 in the same chat → 2 channels.
  - `TestHandleUpdateKickedDisables`: `my_chat_member` `kicked` → the channel is disabled.
  - `TestPollUpdatesAdvancesOffset`: a fake source returns updates 5 and 6 and then cancels the ctx → the second call saw `offset == 7`.
- [ ] **Step 2: Run them to verify they fail.** `go test ./internal/alerts -run 'Link|HandleUpdate|PollUpdates' -v`.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the tests to verify they pass.**
- [ ] **Step 5: Commit** `feat(alerts): link telegram chats via deep-link codes`.

---

### Task 8: Runner, test message, config and boot wiring

**Files:**
- Create: `backend/internal/alerts/runner.go`
- Modify: `backend/internal/config/config.go`, `backend/internal/config/config_test.go`, `backend/cmd/api/main.go`, `.env.example`, `README.md`
- Test: `backend/internal/alerts/runner_test.go`

**Interfaces:**
- Produces: `func (s *Service) Run(ctx context.Context, src UpdateSource)`. It blocks. It ticks `DispatchPending` every 5 s and `Evaluate` every 60 s, and runs `PollUpdates(ctx, src)` when `src != nil && TelegramConfigured()`. A tick error is logged, never fatal. It returns after ctx is done and every goroutine has exited. `var ErrChannelNotFound = errors.New("channel not found")`; `func (s *Service) SendTest(ctx context.Context, projectID, channelID int64) error` sends `FormatTest` and returns `ErrTelegramDisabled`, `ErrChannelNotFound`, or the Telegram error. Config fields `TelegramBotToken`, `TelegramAPIBase`, `AlertsWorker bool`.

- [ ] **Step 1: Write the failing tests**:
  - `TestRunStopsOnCancel`: `Run` returns within 1 s of cancel.
  - `TestSendTest`: it sends a message containing `Test alert` to the channel's chat id; another project's channel id → `ErrChannelNotFound`.
  - In `config_test.go`, `TestAlertsConfigDefaults`: unset env → `TelegramAPIBase == "https://api.telegram.org"`, `AlertsWorker == true`; `ALERTS_WORKER=false` → false.
- [ ] **Step 2: Run them to verify they fail.**
- [ ] **Step 3: Implement**, then wire `main.go`. When the token is set: `tg := alerts.NewTelegram(...)`, `GetMe` with a 10 s timeout. On failure, log a warning and continue with Telegram off (notifier nil). Build `svc := alerts.NewService(pool, notifierOrNil, cfg.PublicURL).WithBotUsername(name)` and `h.WithAlerts(svc)` **before** `ingestSvc.WithIssueProcessor(h.IssueService())`. When `cfg.AlertsWorker` is set, run `go svc.Run(alertsCtx, tgOrNil)` under a WaitGroup; on shutdown cancel `alertsCtx` and wait, after the ingest drain. Skip everything when `pool == nil`. Add the three env vars to `.env.example` with comments. Add a README "Alerts (Telegram)" section: create a bot with @BotFather, set the token, `ALERTS_WORKER=false` on extra replicas, connect via Alerts page.
- [ ] **Step 4: Run** `TEST_DATABASE_URL=$TDB go test ./... && go vet ./...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(alerts): run alert workers at boot with telegram config`.

---

### Task 9: HTTP API + nav count

**Files:**
- Create: `backend/internal/handler/alerts.go`
- Modify: `backend/internal/handler/routes.go`, `backend/internal/handler/nav_counts.go`, `backend/internal/handler/openapi.go`
- Test: `backend/internal/handler/alerts_test.go`, `backend/internal/handler/nav_counts_test.go`

**Interfaces:**
- Consumes: `alerts.Rule`, `alerts.Incident`, `Service.CreateLinkCode`, `Service.SendTest`, `Service.TelegramConfigured`, `Service.BotUsername`, `ErrTelegramDisabled`, `ErrChannelNotFound`.
- Produces (all under `/api/v1/projects/:projectID/alerts`):

| Method | Path | Role | Response |
| --- | --- | --- | --- |
| GET | `/rules` | viewer | `{items: Rule[]}` ordered new_issue, regression, issue_spike |
| PATCH | `/rules/:kind` | admin | `Rule`. Body is partial: `enabled`, `environment` (≤64), `spike_multiplier` (1.5–100), `spike_min_count` (1–100000), `spike_window_minutes` ∈ {15,30,60,180}, `cooldown_minutes` (0–1440). Unknown kind → 404, invalid value → 400 |
| GET | `/channels` | viewer | `{telegram:{configured, bot_username}, items: Channel[]}`. A Channel is `{id, kind, title, chat_type, enabled, disabled_reason, last_sent_at, last_error, created_at}`. The chat id is never returned |
| POST | `/channels/link-code` | admin | 201 `LinkCode`; 503 when Telegram is off |
| PATCH | `/channels/:channelID` | admin | `{enabled}` → Channel. Enabling clears `disabled_reason` |
| DELETE | `/channels/:channelID` | admin | 204 / 404 |
| POST | `/channels/:channelID/test` | admin | 204; 404; 503 when off; 502 `{error}` on a Telegram failure |
| GET | `/incidents` | viewer | `?status=firing\|resolved\|all` (default all), `limit` (default 50, max 200), `{items: Incident[]}` ordered `fired_at DESC` |

`NavCounts` gains `FiringAlerts int \`json:"firing_alerts"\`` (`count(*) FROM alert_incidents WHERE project_id=$1 AND status='firing'`). Handlers return 503 `"alerts are not available"` when `h.alertsSvc == nil` only for the routes that need the service (link-code, test, and the `telegram` block of GET channels reports `configured:false`).

- [ ] **Step 1: Write the failing tests** (`newTestEnv`, then `e.h.WithAlerts(alerts.NewService(e.pool, &fakeNotifier{}, "http://app").WithBotUsername("ArtBot"))`):
  - `TestAlertRulesListAndPatch`: a viewer GETs 3 rules; the viewer's PATCH → 403; an admin PATCHes `issue_spike {"spike_min_count":50,"spike_window_minutes":30}` → 200 and the values persist; `{"spike_window_minutes":45}` → 400; kind `nope` → 404.
  - `TestAlertChannelsHideChatID`: the JSON body of GET channels does not contain the chat id string; `telegram.bot_username == "ArtBot"`.
  - `TestLinkCodeAdminOnly`: viewer → 403, admin → 201 with `group_link` prefix `https://t.me/ArtBot?startgroup=`; without `WithAlerts` → 503.
  - `TestChannelCrossProjectIsolation`: DELETE / PATCH / test on another project's channel id → 404.
  - `TestIncidentsFilter`: 2 firing + 1 resolved → `status=firing` gives 2, the default gives 3, ordered newest first.
  - Extend `TestNavCountsReflectRealData`: 2 firing and 1 resolved incidents in `pid` plus 1 firing in `other` → `firing_alerts == 2`.
- [ ] **Step 2: Run them to verify they fail.** `TEST_DATABASE_URL=$TDB go test ./internal/handler -run 'Alert|LinkCode|Channel|Incidents|NavCounts' -v`.
- [ ] **Step 3: Implement the handlers and routes. Add the paths to `openapi.go`** (`TestOpenAPI*` must stay green).
- [ ] **Step 4: Run** `TEST_DATABASE_URL=$TDB go test ./...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(alerts): rules, channels and incidents api`.

---

### Task 10: Frontend API, deep-link selection, sidebar badge

**Files:**
- Modify: `frontend/src/api.ts`, `frontend/src/components/DashboardShell.tsx`
- Create: `frontend/src/utils/deepLink.ts`, `frontend/src/utils/deepLink.test.ts`

**Interfaces:**
- Consumes: the Task 9 API.
- Produces in `api.ts`: `AlertRuleKind`, `AlertRule`, `AlertRulePatch`, `AlertChannel`, `AlertChannelsResponse`, `AlertIncident`, `AlertLinkCode` (fields as in the Task 9 JSON), `NavCounts.firing_alerts: number`, and

```ts
export const alertApi = {
  rules(projectId: number): Promise<{ items: AlertRule[] }>
  patchRule(projectId: number, kind: AlertRuleKind, body: AlertRulePatch): Promise<AlertRule>
  channels(projectId: number): Promise<AlertChannelsResponse>
  createLinkCode(projectId: number): Promise<AlertLinkCode>
  setChannelEnabled(projectId: number, channelId: number, enabled: boolean): Promise<AlertChannel>
  deleteChannel(projectId: number, channelId: number): Promise<void>
  testChannel(projectId: number, channelId: number): Promise<void>
  incidents(projectId: number, status?: 'firing' | 'resolved' | 'all'): Promise<{ items: AlertIncident[] }>
}
```

  `deepLink.ts`: `export function readWorkspaceFromSearch(search: string): { org: number | null; project: number | null }`.

- [ ] **Step 1: Write the failing test** `deepLink.test.ts`: `'?org=3&project=12'` → `{org:3, project:12}`; `'?project=abc'` → `{org:null, project:null}`; `''` → both null; `'?org=-1&project=0'` → both null (only positive integers).
- [ ] **Step 2: Run it to verify it fails.** `cd frontend && npx vitest run src/utils/deepLink.test.ts`.
- [ ] **Step 3: Implement.** In `DashboardShell`, initialise `storedOrg` / `projectId` from `readWorkspaceFromSearch(window.location.search)` when present, falling back to localStorage, and store them. The existing `orgs.find` / `items.some` fallbacks already ignore an org or project the user doesn't belong to. Add `badgeKey: 'alerts'` and `badgeTone: 'alert'` to the Alerts nav item (relabel it "Alerts"); `badgeFor` maps `'alerts'` → `firing_alerts`; extend the `badgeKey` union.
- [ ] **Step 4: Run** `npx vitest run && npm run build && npm run lint`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(dashboard): alert api client, firing badge and deep-link workspace selection`.

---

### Task 11: Alerts page + overview cell

**Files:**
- Create: `frontend/src/components/alerts/AlertsView.tsx`, `frontend/src/components/alerts/alertFormat.ts`, `frontend/src/components/alerts/alertFormat.test.ts`
- Modify: `frontend/src/pages/DashboardPage.tsx` (delete `AlertRule`, `ALERT_DEFINITIONS`, `mutedAlertIds`, the `alerts` memo and `toggleAlert`; replace the alerts section and the "Alert Rules" metric cell), `frontend/src/dashboard.css` (only if existing `dash-*` classes are insufficient)

**Interfaces:**
- Consumes: `alertApi`, types (Task 10), `WorkspaceContext` (`project`, `canManage`, `isDemo`, `activeOrg`).
- Produces: `export default function AlertsView(props: { projectId: number; canManage: boolean }): JSX.Element`; in `alertFormat.ts`: `ruleTitle(kind)`, `ruleDescription(rule)`, `validateRulePatch(kind, patch): string | null`, `incidentHeadline(incident)`.

UI (reuse the existing `dash-card`, `dash-list`, `badge`, `dash-btn` classes):
- **Telegram channels.** When `!telegram.configured`: "Telegram is not configured on this server. Set TELEGRAM_BOT_TOKEN." Otherwise list the channels (title, chat type, enabled / disabled reason, last error) with Test / Disable-Enable / Remove (admins). "Connect Telegram" → `createLinkCode` → show "Add to a group" (`group_link`) and "Private chat" (`private_link`) buttons opening a new tab, plus a countdown to `expires_at`. Poll `channels` every 3 s until the channel count grows (then show "Connected ✓") or the code expires.
- **Rules.** Three rows with `ruleTitle` / `ruleDescription`, an enabled toggle, an environment input (blank = all), and spike fields (multiplier, min count, window select 15/30/60/180, cooldown). A Save button per row is disabled while `validateRulePatch` returns a message (shown inline). Read-only for non-admins.
- **Incidents.** Firing / Resolved / All segment, rows with a kind badge, `incidentHeadline`, env, release, time ago, and a link to `/dashboard/issues/{issue_id}`. Empty state: "No alerts yet."
- **Demo mode:** `DashboardPage` renders a notice ("Alerts need a signed-in workspace") instead of `AlertsView`.
- **Overview cell:** the label becomes "Active alerts", the value `{firing_alerts} firing`, the sub-text `{n} Telegram chat(s)` or "No channel connected". Data comes from `projectApi.navCounts` and `alertApi.channels`, loaded once per project. Show "—" in demo mode.

- [ ] **Step 1: Write the failing test** `alertFormat.test.ts`:
  - `ruleTitle`: `'new_issue'` → `'New issue'`, `'regression'` → `'Regression'`, `'issue_spike'` → `'Issue spike'`.
  - `ruleDescription`: a spike rule with defaults → `'≥ 20 events and 5× the usual rate within 60 min · production'`; env `''` → it ends with `· all environments`.
  - `validateRulePatch('issue_spike', { spike_window_minutes: 45 })` → a non-null message; `{ spike_multiplier: 1 }` → non-null; `{ spike_min_count: 50 }` → `null`; `{ environment: 'x'.repeat(65) }` → non-null.
  - `incidentHeadline`: a spike incident with `spike {count:142, baseline:6, window_minutes:60}` → `'142 events in 60 min (usual 6)'`; new_issue → the title.
- [ ] **Step 2: Run it to verify it fails.** `npx vitest run src/components/alerts`.
- [ ] **Step 3: Implement `alertFormat.ts` and `AlertsView.tsx`, and edit `DashboardPage.tsx`.**
- [ ] **Step 4: Run** `npx vitest run && npm run build && npm run lint`. Expected: PASS. Then check by hand with `make db-up api web`: with a real bot token, connect a private chat, press Test, `curl` an error to `/api/v1/ingest` with a live key and confirm a 🆕 message arrives within about 5 s with a working "Open issue" link; resolve the issue, send the same error again, and confirm 🔁.
- [ ] **Step 5: Commit** `feat(dashboard): alerts page with telegram channels, rules and incidents`.
- [ ] **Step 6:** Run `graphify update .` from the repo root (project rule after code changes).
