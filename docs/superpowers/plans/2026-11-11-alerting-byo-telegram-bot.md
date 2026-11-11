# Per-tenant Telegram Bot (send-only) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let each organisation connect its own Telegram bot from the dashboard (token encrypted at rest, changeable at runtime) so the platform only ever *sends* alert messages and never receives anything from Telegram.

**Architecture:** A new deployment setting `ALERTS_BOT_MODE` (`tenant` default, or `shared`) selects one of two mutually exclusive delivery paths. `shared` keeps today's behaviour (one global bot, link codes, long polling). `tenant` resolves the organisation's bot per dispatch tick from a new `alert_bots` table (token sealed with AES-256-GCM envelope encryption under a master key from the environment), sends to manually entered chat destinations, and runs no poller. Channels carry an `origin` (`linked` | `manual`) and are active only in the mode that matches it, derived at read time without rewriting rows.

**Tech Stack:** Go 1.25 (Fiber, pgx, `crypto/aes` + `crypto/cipher`), PostgreSQL, React 19 + TypeScript + Vitest.

**Spec:** `docs/superpowers/specs/2026-11-11-alerting-byo-telegram-bot-design.md` (builds on `2026-11-11-alerting-telegram-design.md` and its plan; the code on branch `feat/alerting-telegram` is the starting point).

## Global Constraints

- Bot modes: `ALERTS_BOT_MODE` ∈ `tenant` (default) | `shared`; mutually exclusive; **no fallback** from a tenant without a bot to the shared bot.
- `tenant` mode never calls `getUpdates`, never registers a webhook, has no inbound Telegram endpoint, runs no poller. The only Telegram methods the platform calls in `tenant` mode: `sendMessage` and `getMe`. (The `getChat` lookup this plan originally described was removed after the final review: it returns chat content, not just metadata.)
- Telegram API host is fixed in code (`https://api.telegram.org`); a tenant can never supply a URL. The operator-level `TELEGRAM_API_BASE` stays for tests only.
- Token: AES-256-GCM envelope encryption, wrapped under a master key; additional authenticated data binds the organisation id; `key_id` stored with the ciphertext. Master key: `ALERTS_MASTER_KEY` (base64 of exactly 32 bytes) or `ALERTS_MASTER_KEY_FILE`; both set, unreadable, empty, bad base64 or wrong length is a startup panic; required in `tenant` mode in production (panic when missing); in development a missing key leaves the engine running and only the bot API answers `alerts master key is not configured`. Key rotation = `key_id` column + decrypt-by-`key_id` only; no tool.
- The token is write-only: never in any API response, log line, error, panic message, metric or URL; decrypted tokens exist only in memory for one request or one dispatcher tick.
- Detail level per organisation: `minimal` (**default**) or `full`. `minimal` sends kind, project name, environment, release and the issue link only, plus a spike alert's counts (count, baseline, window); no issue title, no session id, no replay link. Applies to alert, recovery, digest and test messages. The zero value of the in-code level behaves as `full` (shared mode and existing tests keep today's text).
- No working bot (none configured, or `invalid`): incidents stay `pending`; after **24 hours** a pending incident becomes `skipped`. Telegram 429 backoff is **per bot (organisation)**. Telegram 401 marks the bot `invalid` with a reason; other organisations are unaffected.
- Channel activity is derived: `linked` channels are active only in `shared` mode, `manual` channels only in `tenant` mode. Nothing is rewritten or deleted on a mode switch.
- Roles: org bot `GET` any organisation member; `PUT`/`PATCH`/`DELETE` org admin (owner included, `RequireOrgRole(auth.RoleAdmin)`). Channel create/test/update/delete keep project admin; reads keep project viewer.
- Existing behaviour of rules, incidents, the outbox, the spike evaluator, deep links and the sidebar badge must not change. Packages share one dev database: always run Go tests with `-p 1`.

## Plan decisions (Claude's calls; the spec leaves them to the builder)

- `Notifier.SendMessage` takes the chat as a **string** (decimal id or `@username`) so both modes share one interface; `NewService` still defaults to `shared` mode so existing constructors and tests keep working; `main.go` sets the configured mode.
- One sealed blob per bot in `alert_bots.token_ciphertext`: `version(1) | wrapNonce(12) | wrappedDEK(48) | dataNonce(12) | ciphertext+tag`, a fresh random 32-byte data key per write wrapped under the master key; AAD = `"org:<id>|key:<keyID>"` for both wraps.
- Telegram bot-token format check before any network call: `^[0-9]{5,15}:[A-Za-z0-9_-]{30,}$`.
- Chat destination format: `^-?[0-9]{5,20}$` or `^@[A-Za-z][A-Za-z0-9_]{4,31}$`.

## Review Focus

1. The token leaking anywhere: API responses, logs, error strings, panic messages, `last_error` (a Telegram error text must never carry it). → Task 4 (`TestBotViewNeverContainsToken`, `TestSetBotNeverLogsOrReturnsToken`), Task 7 (`TestBotEndpointsNeverEchoToken`).
2. Cross-tenant: organisation A's ciphertext opened as organisation B, or A's bot used for B's project. → Task 2 (AAD test), Task 4 (`TestCopiedCiphertextFailsForOtherOrg`), Task 5 (`TestTenantDispatchUsesOrgBot`).
3. Mode switch round trip: `linked` channels inactive and untouched in `tenant`, `manual` inactive in `shared`, flipping back resumes with no data changes. → Task 5 (`TestModeDerivedActivity`).
4. A bot that is removed or invalid: incidents stay `pending` (not `skipped`) for under 24 h, become `skipped` after, and re-adding the bot delivers the rest (as a digest when more than three). → Task 5.
5. Master key problems: missing in production (fatal), missing in development (engine runs, API says so), malformed (fatal everywhere). → Task 1 and Task 7 (`TestBotEndpointsWithoutMasterKey`).
6. A 401 or 429 for one organisation delays no other organisation in the same tick. → Task 5.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `backend/internal/config/config.go` (+ `alerts_mode_test.go`) | `ALERTS_BOT_MODE`, master key parsing and validation |
| `backend/internal/alerts/seal.go` (+test) | `Sealer`, `LocalSealer` envelope encryption |
| `backend/internal/alerts/telegram.go` (+test) | string chat addressing, `GetBot`, `GetChat`, `ErrUnauthorized` |
| `backend/internal/db/migrations/0012_alert_bots.sql` | `alert_bots`, channel `origin`/`destination` |
| `backend/internal/alerts/bot.go` (+test) | modes, options, bot store and resolution |
| `backend/internal/alerts/dispatch.go`, `runner.go`, `link.go` | mode-aware delivery, per-org backoff, expiry, send test |
| `backend/internal/alerts/format.go` | detail level |
| `backend/internal/handler/alerts_bot.go`, `alerts.go`, `routes.go`, `openapi.go` | org bot API, manual channels, listing |
| `backend/cmd/api/main.go`, `.env.example`, `README.md` | wiring and docs |
| `frontend/src/api.ts`, `frontend/src/components/alerts/alertFormat.ts` (+tests) | client and pure helpers |
| `frontend/src/components/TelegramBotCard.tsx`, `pages/SettingsPage.tsx`, `components/alerts/AlertsView.tsx`, `pages/DashboardPage.tsx` | dashboard |

Test command (backend): `cd backend && TEST_DATABASE_URL='postgres://artracker:artracker@localhost:5433/artracker?sslmode=disable' go test -p 1 ./...` (with `export PATH=$PATH:$HOME/go/bin:$HOME/sdk/go1.26.4/bin`).

---

### Task 1: Config for bot mode and master key

**Files:** Modify `backend/internal/config/config.go`, `.env.example`, `README.md`. Test: create `backend/internal/config/alerts_mode_test.go`.

**Interfaces:** Produces `Config.AlertsBotMode string` (`"tenant"`|`"shared"`), `Config.AlertsMasterKey []byte` (32 bytes or nil), `Config.AlertsMasterKeyID string` (`ALERTS_MASTER_KEY_ID`, default `"v1"`). Existing `loadTelegramToken` (committed in `2c725e1`) is unchanged.

- [ ] **Step 1: Write failing tests** (use `t.Setenv`, `t.TempDir`; a helper that runs `Load()` and returns the panic text): `TestAlertsBotModeDefaultsToTenant` (unset → `tenant`), `TestAlertsBotModeValues` (`shared`, ` TENANT ` normalise; `bogus` panics naming `ALERTS_BOT_MODE`), `TestMasterKeyFromEnv` (base64 of 32 bytes → 32-byte slice), `TestMasterKeyFromFile` (trailing newline trimmed), `TestMasterKeyBothSetPanics`, `TestMasterKeyMalformedPanics` (bad base64; 31 bytes; 33 bytes; empty file — panics in development too, and the panic text never contains the key), `TestMasterKeyRequiredInProductionTenantMode` (production + tenant + unset → panic; production + shared + unset → no panic), `TestMasterKeyOptionalInDevelopment` (development + tenant + unset → nil key, no panic).
- [ ] **Step 2: Run to verify failure.** `go test ./internal/config -run 'AlertsBotMode|MasterKey' -count=1`. Expected: FAIL (fields undefined).
- [ ] **Step 3: Implement** `loadAlertsMasterKey() []byte` and the mode parsing in `Load()`; panic messages name the variable and never include the value. The production rule needs the mode, so evaluate it after `cfg.Env`/mode are known.
- [ ] **Step 4: Run tests** (config package, then full suite). Expected: PASS.
- [ ] **Step 5: Docs.** `.env.example`: `ALERTS_BOT_MODE=tenant`, `ALERTS_MASTER_KEY=` (comment: `openssl rand -base64 32`, ships no value), `ALERTS_MASTER_KEY_FILE=`, `TELEGRAM_BOT_TOKEN_FILE=`. `README.md` "Alerts (Telegram)": the two modes, that `tenant` is the SaaS default and each organisation connects its own bot in Settings, `shared` is for self-hosting, and where the master key comes from. Add an **"Upgrading"** note: `ALERTS_BOT_MODE` defaults to `tenant`, and in production that mode refuses to start without `ALERTS_MASTER_KEY`, so an existing deployment must set either `ALERTS_MASTER_KEY` (generate with `openssl rand -base64 32`) or `ALERTS_BOT_MODE=shared` **before** deploying this version; `docker-compose.prod.yml` already forwards the variables through `env_file: .env`. Also fix the README **"Tests"** section, which currently tells people to run DB-backed tests against the dev database `artracker`; the test helpers `TRUNCATE users, organizations, projects, api_keys, ... CASCADE`, so that wipes real dev data. Change the example DSN to a separate database `artracker_test` (`.../artracker_test?sslmode=disable`), add the one-line `docker exec <db container> psql -U artracker -d postgres -c "CREATE DATABASE artracker_test"` step, and a bold warning that these tests delete all users, organisations and projects in whatever database the DSN names.
- [ ] **Step 6: Commit** `feat(config): alerts bot mode and master key`.

---

### Task 2: Envelope sealer

**Files:** Create `backend/internal/alerts/seal.go`, test `seal_test.go`.

**Interfaces:** Produces
```go
type Sealer interface {
    Seal(orgID int64, plaintext []byte) (blob []byte, keyID string, err error)
    Open(orgID int64, keyID string, blob []byte) ([]byte, error)
}
func NewLocalSealer(masterKey []byte, keyID string) (*LocalSealer, error) // key must be exactly 32 bytes; keyID non-empty
var ErrSealOpen = errors.New("alerts: cannot open sealed value")
```
Blob layout is in "Plan decisions". Every `Open` failure returns `ErrSealOpen` (no cause text that could leak material).

- [ ] **Step 1: Write failing tests:** `TestSealOpenRoundTrip`; `TestSealRandomised` (same plaintext twice → different blobs); `TestOpenRejectsOtherOrg` (seal for org 1, open as org 2 → `ErrSealOpen`); `TestOpenRejectsTamper` (flip one byte in each of the four regions → `ErrSealOpen`); `TestOpenRejectsWrongKeyID`; `TestOpenRejectsTruncated` (0, 1, and len-1 bytes; no panic); `TestBlobDoesNotContainPlaintext`; `TestNewLocalSealerRejectsBadKey` (0, 16, 31, 33 bytes; empty keyID).
- [ ] **Step 2: Run to verify failure.** `go test ./internal/alerts -run 'Seal|Open' -count=1`.
- [ ] **Step 3: Implement `LocalSealer`** with `crypto/rand`, AES-256-GCM twice (wrap the data key, seal the plaintext), AAD `"org:<id>|key:<keyID>"`.
- [ ] **Step 4: Run tests.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(alerts): envelope sealer for bot tokens`.

---

### Task 3: Telegram client — string chats, bot info, chat info, 401

**Files:** Modify `backend/internal/alerts/telegram.go`, `dispatch.go` (`channel` struct, `deliver`), `link.go` (replies), `runner.go` (`SendTest`), `testutil_test.go`, and every test that builds a `fakeNotifier` or reads `sentMsg`. Test: `telegram_test.go`.

**Interfaces:** Produces
```go
type Notifier interface { SendMessage(ctx context.Context, chat string, html string) error }
func (t *Telegram) SendMessage(ctx context.Context, chat, html string) error // numeric chat → JSON number, "@name" → JSON string
type BotInfo struct{ ID int64; Username string }
func (t *Telegram) GetBot(ctx context.Context) (BotInfo, error)  // GetMe keeps its signature and wraps it
type ChatInfo struct{ Title, Type string }
func (t *Telegram) GetChat(ctx context.Context, chat string) (ChatInfo, error)
var ErrUnauthorized = errors.New("telegram: bot token rejected") // Telegram error_code 401 or HTTP 401
```
Behaviour of `shared` mode stays byte-identical: linked channels pass `strconv.FormatInt(chatID, 10)`.

- [ ] **Step 1: Write failing tests** against an `httptest` Bot API: `TestSendMessageNumericChatIsJSONNumber` (`chat_id` decodes as number), `TestSendMessageUsernameChatIsJSONString` (`"@news"`), `TestGetBotReturnsIDAndUsername`, `TestGetChatReturnsTitleAndType` (sends `chat_id`), `TestUnauthorizedMapsToErrUnauthorized` (both an error envelope `{error_code:401}` and a bare HTTP 401 with an HTML body; `errors.Is`), and the error text never contains the token. Add the shared test helper `newStrictTelegramServer(t) *strictTelegram` in `testutil_test.go`: serves `sendMessage`, `getMe`, `getChat` with canned replies, records `(token, method)`, and **calls `t.Errorf` for any other method** (this enforces send-only in later tasks).
- [ ] **Step 2: Run to verify failure.** `go test ./internal/alerts -run 'Telegram|SendMessage|GetBot|GetChat|Unauthorized' -count=1`.
- [ ] **Step 3: Implement** the interface change and update every call site and fake (`fakeNotifier.SendMessage(ctx, chat string, …)`, `sentMsg.chat string`; tests that compared int64 chat ids compare the decimal string). No behaviour change elsewhere.
- [ ] **Step 4: Run the whole alerts and handler suites.** Expected: PASS with the same counts as before plus the new tests.
- [ ] **Step 5: Commit** `refactor(alerts): address chats as strings and add getBot, getChat and 401`.

---

### Task 4: Migration, modes and bot store

**Files:** Create `backend/internal/db/migrations/0012_alert_bots.sql`, `backend/internal/alerts/bot.go`, test `bot_test.go`.

**Interfaces:** Consumes `Sealer` (Task 2), `Telegram.GetBot`, `ErrUnauthorized` (Task 3). Produces
```go
type BotMode string
const ( ModeTenant BotMode = "tenant"; ModeShared BotMode = "shared" )
type DetailLevel string
const ( DetailMinimal DetailLevel = "minimal"; DetailFull DetailLevel = "full" )
func (s *Service) WithMode(m BotMode) *Service;  func (s *Service) Mode() BotMode      // NewService defaults to ModeShared
func (s *Service) WithSealer(sl Sealer) *Service
func (s *Service) WithAPIBase(base string) *Service                                   // default "https://api.telegram.org"
type BotView struct {
    Configured  bool        `json:"configured"`
    BotUsername string      `json:"bot_username"`
    Status      string      `json:"status"`        // "active" | "invalid" | "" when not configured
    StatusReason string     `json:"status_reason"`
    DetailLevel DetailLevel `json:"detail_level"`
    UpdatedAt   *time.Time  `json:"updated_at"`
}
func (s *Service) BotStatus(ctx context.Context, orgID int64) (BotView, error)
func (s *Service) SetBot(ctx context.Context, orgID, userID int64, token string, level DetailLevel) (BotView, error) // level "" keeps the existing level, default minimal
func (s *Service) SetDetailLevel(ctx context.Context, orgID int64, level DetailLevel) (BotView, error)                 // ErrBotNotFound when none
func (s *Service) DeleteBot(ctx context.Context, orgID int64) error                                                    // idempotent
var ErrNoSealer = errors.New("alerts master key is not configured")
var ErrWrongMode = errors.New("not available in this bot mode")
var ErrInvalidToken = errors.New("telegram rejected the bot token")
var ErrBotNotFound = errors.New("no bot configured")
// internal, used by Tasks 5-7:
type orgBot struct { tg *Telegram; level DetailLevel; username string } // *Telegram satisfies Notifier; built with s.apiBase, never a tenant URL
func (s *Service) workingBot(ctx context.Context, orgID int64) (*orgBot, error) // nil, nil when none, invalid, or no sealer; error only for database/decrypt-infra failures
func (s *Service) LookupChat(ctx context.Context, orgID int64, chat string) (ChatInfo, error) // getChat through the org's working bot; ErrBotNotFound when there is none
```
Migration: `alert_bots(org_id BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE, token_ciphertext BYTEA NOT NULL, key_id TEXT NOT NULL, bot_id BIGINT NOT NULL DEFAULT 0, bot_username TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','invalid')), status_reason TEXT NOT NULL DEFAULT '', detail_level TEXT NOT NULL DEFAULT 'minimal' CHECK (detail_level IN ('minimal','full')), created_by BIGINT REFERENCES users(id) ON DELETE SET NULL, created_at/updated_at/last_validated_at TIMESTAMPTZ)`; `ALTER TABLE alert_channels ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'linked' CHECK (origin IN ('linked','manual'))`, `ADD COLUMN IF NOT EXISTS destination TEXT NOT NULL DEFAULT ''`, `ALTER COLUMN telegram_chat_id DROP NOT NULL`, partial unique index on `(project_id, destination) WHERE origin = 'manual'`. Re-runnable; existing rows keep working untouched.

- [ ] **Step 1: Write failing tests.** Migration: `TestMigration0012`: existing-style insert gets `origin='linked'`; a `manual` channel with NULL `telegram_chat_id` inserts; a duplicate `manual` `(project_id, destination)` fails with 23505 while the same destination on a `linked` row is unconstrained; `alert_bots` rejects `status='x'` and cascades on organisation delete. Service (use `newStrictTelegramServer` as the Bot API and `WithAPIBase`): `TestSetBotValidatesWithGetMeOnly`; `TestSetBotStoresCiphertext` (row bytes do not contain the token; `key_id` equals the sealer's); `TestSetBotRejectsBadFormatWithoutNetwork` (zero requests); `TestSetBotUnauthorizedIsInvalidToken` (server answers 401); `TestSetBotWithoutSealer` → `ErrNoSealer`; `TestSharedModeRejectsBotCalls` → `ErrWrongMode`; `TestSetBotReplaceOverwritesAndReactivates` (an `invalid` bot becomes `active`, `status_reason` cleared); `TestSetDetailLevel`; `TestDeleteBotIsIdempotent`; `TestBotViewNeverContainsToken` (marshal `BotView` to JSON, assert no `token` key and no token substring); `TestSetBotNeverLogsOrReturnsToken` (capture `slog` output and all error strings across success, 401 and network failure); `TestCopiedCiphertextFailsForOtherOrg` (copy org A's row bytes into an org B row; `workingBot(B)` returns an error or `nil` and no HTTP request is made with that token).
- [ ] **Step 2: Run to verify failure.** `go test -p 1 ./internal/alerts -run 'Migration0012|SetBot|BotView|DeleteBot|SharedMode|Copied' -count=1` (needs `TEST_DATABASE_URL`).
- [ ] **Step 3: Implement** the migration, `bot.go`, and the `Service` option fields. `SetBot` order: mode check → sealer check → format check → `GetBot` with a 10 s timeout → seal → upsert. A network failure to Telegram returns a wrapped error that does not contain the token or URL.
- [ ] **Step 4: Run the alerts suite and the migration tests.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(alerts): bot store, modes and migration 0012`.

---

### Task 5: Mode-aware dispatcher

**Files:** Modify `backend/internal/alerts/dispatch.go` (and `deliver`'s callers), `runner.go` (`Run` only: never poll in tenant mode). Test: `dispatch_tenant_test.go`.

**Interfaces:** Consumes `workingBot`, `Mode`, `Notifier` (Tasks 3-4). Produces constants `pendingMaxAge = 24 * time.Hour`, `noBotRecheck = time.Minute`. `Run(ctx, src)` polls only when `s.Mode() == ModeShared && src != nil`.

Behaviour to implement:
- `shared` mode: exactly today's behaviour; channels loaded are `origin='linked'` and enabled; `s.n == nil` still skips everything pending.
- `tenant` mode, per project group: resolve the organisation's bot once per tick (cache per org for the tick). No working bot → do not touch incident states except: incidents with `fired_at <= now - pendingMaxAge` become `skipped` (both stages); every other incident is deferred to `now + noBotRecheck` so the loop does not spin. Working bot → channels loaded are `origin='manual'` and enabled; none → `skipped` as today; else send through that bot's notifier using the channel's destination (`destination` for `manual`, decimal `telegram_chat_id` for `linked`).
- 401 (`ErrUnauthorized`) on a send: `UPDATE alert_bots SET status='invalid', status_reason='token rejected by Telegram'`, leave the incidents pending (deferred `noBotRecheck`), stop **that organisation** for the tick.
- 429: defer every pending incident of every project of **that organisation** to `now + clampDelay(retry)` and skip the organisation's remaining projects this tick; other organisations proceed.
- `MigratedError`: update `destination` (manual) or `telegram_chat_id` (linked) and retry once; a unique-violation merge deletes the stale row as today.
- Message detail level comes from the bot row (Task 6 consumes it); in `shared` mode it is `full`.

- [ ] **Step 1: Write failing tests** with `newStrictTelegramServer` (records `(token, method)`) and a `Service` built with `WithMode(ModeTenant)`, `WithSealer(testSealer)`, `WithAPIBase(server.URL)`; helper `seedOrgWithBot(t, token)` seals via `SetBot`. `TestTenantDispatchUsesOrgBot` (two organisations with different tokens: each project's message hits only its own token's path); `TestTenantNoBotKeepsPendingThenExpires` (incident 1 h old stays `pending` and `next_attempt_at ≈ now+1m`; incident 25 h old becomes `skipped` in both stages; zero HTTP requests); `TestTenantBotAddedDeliversPending` (5 pending incidents + a later `SetBot` → one digest message, all `sent`); `TestTenantUnauthorizedMarksBotInvalid` (401 → bot `invalid` with reason, incident still `pending`, and a second organisation in the same tick is delivered); `TestTenantRateLimitIsPerOrg` (429 for org A: every pending incident of org A's two projects is deferred, org B delivered in the same tick); `TestModeDerivedActivity` (tenant mode: a `linked` channel receives nothing and its row is byte-identical afterwards; shared mode: a `manual` channel receives nothing; flipping the service back and forth resumes sending, with `enabled`/`disabled_reason` untouched); `TestManualChannelMigratedUpdatesDestination` and `TestLinkedChannelMigratedUpdatesChatID`; `TestTenantModeNeverPolls` (`Run` with a non-nil fake `UpdateSource` never calls `GetUpdates` in tenant mode). Keep every existing dispatch test passing in shared mode unchanged.
- [ ] **Step 2: Run to verify failure.** `go test -p 1 ./internal/alerts -run 'Tenant|ModeDerived|Migrated' -count=1`.
- [ ] **Step 3: Implement** in `dispatch.go`; keep `dispatchProject` readable by extracting the per-organisation resolution (`orgState` map for the tick) rather than growing the function.
- [ ] **Step 4: Run the full alerts suite.** Expected: PASS (all earlier dispatch tests included).
- [ ] **Step 5: Commit** `feat(alerts): tenant-mode dispatcher with per-org bots`.

---

### Task 6: Detail level in messages

**Files:** Modify `backend/internal/alerts/format.go`, `dispatch.go` (set `MessageContext.Detail` from the resolved bot's level; `full` in shared mode). Test: `format_detail_test.go`. (`SendTest` is changed only in Task 8.)

**Interfaces:** `MessageContext` gains `Detail DetailLevel`; the zero value `""` formats as `full`. `FormatFire`, `FormatRecovered`, `FormatDigest`, `FormatTest` honour it.

- [ ] **Step 1: Write failing tests:** `TestMinimalFireOmitsTitleSessionAndReplay` (a title like `Cannot read email a@b.c` and a session id are absent from the output; kind header, project name, `env · release`, and the issue link present, replay link absent even when `HasReplay`); `TestMinimalSpikeKeepsCounts` (`142 events in 60 min, baseline 6` still present, title absent); `TestMinimalRecoveredOmitsTitle`; `TestMinimalDigestListsCountsNotTitles` (per-kind counts and the Alerts-page link, no titles); `TestMinimalTestMessage`; `TestZeroDetailBehavesAsFull` (output identical to `DetailFull`; all existing format tests unchanged and passing); `TestMinimalEscapesProjectName` (HTML-escaped as before); `TestDispatchTenantRespectsDetailLevel` (strict fake server: organisation set to `minimal` → the request body text lacks the title; `full` → contains it).
- [ ] **Step 2: Run to verify failure.** `go test -p 1 ./internal/alerts -run 'Minimal|Detail' -count=1`.
- [ ] **Step 3: Implement.** Minimal wording per kind: a short fixed phrase replacing the title line (for example `A new error was detected`); keep the existing header/emoji per kind.
- [ ] **Step 4: Run the alerts suite.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(alerts): minimal and full message detail levels`.

---

### Task 7: HTTP API

**Files:** Create `backend/internal/handler/alerts_bot.go`; modify `alerts.go` (list/create channel, link-code gate), `routes.go`, `openapi.go`. Test: `alerts_bot_test.go`.

**Interfaces:** Consumes `BotStatus`, `SetBot`, `SetDetailLevel`, `DeleteBot`, `LookupChat`, `Mode`, `SendTest`, errors (Task 4; `getChat` goes through `LookupChat`, a 5 s context). Produces:

| Method | Path | Role | Notes |
| --- | --- | --- | --- |
| GET | `/orgs/:orgID/alerts/telegram-bot` | org viewer | `BotView` JSON plus `mode` (`"tenant"` \| `"shared"`); never the token; in shared mode `configured:false` and the write endpoints below answer 404 |
| PUT | `/orgs/:orgID/alerts/telegram-bot` | org admin | body `{token, detail_level?}` → `BotView`; 400 `invalid bot token` (`ErrInvalidToken`); 503 `alerts master key is not configured`; 404 in shared mode; 502 `could not reach Telegram` for network failures |
| PATCH | same | org admin | body `{detail_level}` → `BotView`; 404 when no bot |
| DELETE | same | org admin | 204, idempotent |
| POST | `/projects/:projectID/alerts/channels` | project admin | body `{chat, label}`; tenant mode only; 409 `connect a Telegram bot first` when no working bot; 400 invalid chat or empty label (≤80 chars); 409 `chat already added`; best-effort `getChat` (5 s) fills `chat_type` and sets `warning` on failure; returns the channel JSON (+`warning`) with 201 |

Changes to existing handlers: `GET .../channels` adds `mode`, `bot` (`BotView` of the project's organisation; `configured:false` in shared mode) and per channel `origin`, `destination` (empty for `linked`), `active` (`origin` matches the mode); the existing `telegram` block stays. `POST .../channels/link-code` → 404 `link flow is disabled in tenant mode`. `POST .../test`: `ErrBotNotFound` → 409 `connect a Telegram bot first`; tenant `SendTest` resolves the organisation's bot.

- [ ] **Step 1: Write failing tests** (handler `newTestEnv` + `addOrg`/`addMember`/`addProject`; service built as in Task 5 with a strict fake Telegram): `TestBotEndpointsRoleMatrix` (org viewer/member can GET, only admin and owner can PUT/PATCH/DELETE; outsider 403/404); `TestPutBotStoresAndHidesToken` (PUT then GET: username shown, no `token` key); `TestBotEndpointsNeverEchoToken` (the token string appears in no response body, success or error, and not in captured logs); `TestPutBotErrors` (bad format 400, 401 from Telegram 400, unreachable 502, no sealer 503, shared mode 404); `TestBotEndpointsWithoutMasterKey` (GET works and reports configured; PUT 503); `TestPatchAndDeleteBot`; `TestCreateManualChannel` (numeric and `@name` chats accepted, junk rejected, duplicate 409, no bot 409, `getChat` failure still 201 with `warning`, `getChat` success fills `chat_type`); `TestChannelListShowsOriginAndActive` (a `linked` row is `active:false` in tenant mode, `manual` is `active:true`); `TestLinkCodeDisabledInTenantMode`; `TestTestChannelUsesOrgBot`; `TestCreateChannelCrossProjectIsolation`. Existing handler alert tests keep passing (shared-mode service).
- [ ] **Step 2: Run to verify failure.** `go test -p 1 ./internal/handler -run 'Bot|ManualChannel|ChannelList|LinkCode' -count=1`.
- [ ] **Step 3: Implement handlers, routes, OpenAPI entries** (`TestOpenAPI*` stays green). Responses use handler-local JSON structs; errors use `fiber.NewError` with the messages above and never include upstream text.
- [ ] **Step 4: Run the full backend suite.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(alerts): org bot and manual channel API`.

---

### Task 8: Wiring in `main.go`

**Files:** Modify `backend/cmd/api/main.go`, `backend/internal/alerts/runner.go` (only if needed for wiring). Test: `backend/internal/alerts/runner_test.go` additions.

**Interfaces:** Consumes `Config.AlertsBotMode`, `AlertsMasterKey`, `AlertsMasterKeyID`, `WithMode`, `WithSealer`, `WithAPIBase`.

Wiring: build `svc := alerts.NewService(pool, n, cfg.PublicURL)`; `WithMode(alerts.BotMode(cfg.AlertsBotMode))`; `WithAPIBase(cfg.TelegramAPIBase)`. **Tenant mode:** `n` and `src` stay untyped nil, no global Telegram client, no `GetMe`, no `RetryBotUsername`; if `cfg.AlertsMasterKey != nil` build `alerts.NewLocalSealer(key, cfg.AlertsMasterKeyID)` and `WithSealer` (a constructor error is fatal at boot); with no key log one INFO line `alerts master key not configured: org bot API disabled`. **Shared mode:** today's code path unchanged (token, `GetMe`, retry loop, poller). `h.WithAlerts(svc)` stays before `WithIssueProcessor`; `ALERTS_WORKER` still gates `Run`.

- [ ] **Step 1: Write failing tests:** `TestRunTenantModeNeverPolls` already exists from Task 5; add `TestSendTestTenantUsesOrgBot` (strict fake; the message goes to the organisation's token path and the channel's `destination`), `TestSendTestTenantWithoutBotIsErrBotNotFound`, `TestSendTestSharedModeUnchanged`.
- [ ] **Step 2: Run to verify failure.** `go test -p 1 ./internal/alerts -run 'SendTest' -count=1`.
- [ ] **Step 3: Implement** `SendTest` for both modes (reuse the Task 3 chat addressing and the existing bookkeeping helpers; in tenant mode set `MessageContext.Detail` from the organisation's bot level) and the `main.go` wiring.
- [ ] **Step 4: Run** `go build ./... && go vet ./... && go test -p 1 ./...`. Expected: PASS. Manual smoke (record the result, do not fake it): start the API with `ALERTS_BOT_MODE=tenant` and a generated master key, `GET /healthz` is 200 and the log shows no Telegram polling; with `ALERTS_MASTER_KEY` unset and `APP_ENV=production` the process exits at boot.
- [ ] **Step 5: Commit** `feat(alerts): wire bot modes and master key at boot`.

---

### Task 9: Frontend client and pure helpers

**Files:** Modify `frontend/src/api.ts`, `frontend/src/components/alerts/alertFormat.ts`, `alertFormat.test.ts`.

**Interfaces:** Produces in `api.ts`: `AlertBot = { mode: 'tenant' | 'shared'; configured: boolean; bot_username: string; status: '' | 'active' | 'invalid'; status_reason: string; detail_level: 'minimal' | 'full'; updated_at: string | null }` (`alertApi.bot` and the `bot` block of the channels response both carry `mode`); `AlertChannel` gains `origin: 'linked' | 'manual'`, `destination: string`, `active: boolean`; `AlertChannelsResponse` gains `mode: 'tenant' | 'shared'` and `bot: AlertBot`; and
```ts
alertApi.bot(orgId: number): Promise<AlertBot>
alertApi.putBot(orgId: number, body: { token: string; detail_level?: 'minimal' | 'full' }): Promise<AlertBot>
alertApi.patchBot(orgId: number, body: { detail_level: 'minimal' | 'full' }): Promise<AlertBot>
alertApi.deleteBot(orgId: number): Promise<void>
alertApi.createChannel(projectId: number, body: { chat: string; label: string }): Promise<AlertChannel & { warning?: string }>
```
In `alertFormat.ts`: `validateChat(input: string): string | null`, `validateLabel(input: string): string | null`, `validateBotToken(input: string): string | null` (same regexes as the backend; labels 1–80 chars), `botBannerText(bot: AlertBot, canManage: boolean): string | null`, `channelActivityLabel(c: AlertChannel, mode: 'tenant' | 'shared'): string | null` (`belongs to the shared bot` for an inactive `linked` channel in tenant mode, `inactive in shared mode` for the reverse, `null` when active), `detailLevelLabel(level): string`.

- [ ] **Step 1: Write failing tests** with exact strings: `validateChat` accepts `-1001234567890`, `123456`, `@my_channel`; rejects `''`, `abc`, `@a`, `12`, ` 123456 ` with a non-null message; `validateLabel('')` non-null, 80 chars null, 81 non-null; `validateBotToken` accepts `123456789:AAEhBP0av28kPoKy2VfQ3CwR4aFhEXAMPLE1` shape, rejects `''`, `abc`, `123:short`; `botBannerText`: not configured + admin → `Connect a Telegram bot in Settings to receive alerts.`; not configured + member → `An organisation admin needs to connect a Telegram bot.`; `invalid` → text containing the `status_reason`; active → `null`; `channelActivityLabel` for the four combinations; `detailLevelLabel('minimal')` → `Minimal (no error text)`, `('full')` → `Full (includes error text)`.
- [ ] **Step 2: Run to verify failure.** `cd frontend && npx vitest run src/components/alerts/alertFormat.test.ts`.
- [ ] **Step 3: Implement** the types, client methods (`/orgs/${orgId}/alerts/telegram-bot`, `/projects/${projectId}/alerts/channels`) and helpers.
- [ ] **Step 4: Run** `npx vitest run && npm run build && npm run lint`. Expected: PASS, no new lint warnings.
- [ ] **Step 5: Commit** `feat(dashboard): alert bot client and helpers`.

---

### Task 10: Frontend UI

**Files:** Create `frontend/src/components/TelegramBotCard.tsx`; modify `frontend/src/pages/SettingsPage.tsx`, `frontend/src/components/alerts/AlertsView.tsx`, `frontend/src/pages/DashboardPage.tsx`, `frontend/src/dashboard.css` (append one delimited block at the end only if needed).

**Interfaces:** `TelegramBotCard(props: { orgId: number; canManage: boolean })`; `AlertsView` props gain `orgId: number`. Consumes Task 9 exports.

Behaviour: **TelegramBotCard** (on `SettingsPage` beside the team members panel): loads `alertApi.bot(orgId)`; `mode === 'shared'` → the single line `The Telegram bot is managed by the server in this deployment.` and no form; otherwise not configured → short how-to (create a bot with @BotFather, paste the token) and, for admins, a **password-type** token input (`autoComplete="off"`), a detail-level select, a Connect button disabled until `validateBotToken` passes; configured → bot name, status badge (`badge--active` / `badge--revoked` with `status_reason`), detail level select (admins), Replace and Remove (`window.confirm`) for admins; the token field is cleared after every attempt and never repopulated; errors in a `dash-error` banner with `role="alert"`; members see the status read-only. **AlertsView Channels card:** in `tenant` mode hide Connect Telegram and the link flow; show the `botBannerText` banner when applicable (admins get a link to `/dashboard/settings`); admins get an **Add chat** form (chat input, label input, instructions: add your bot to the chat and send a message, then find the chat id; the helper text must not ask the user to paste the bot token anywhere), calling `createChannel`, showing `warning` inline; list rows show the label, `destination`, `channelActivityLabel`; Test/Disable/Remove as today; an inactive row shows no Test button. In `shared` mode the card behaves exactly as today (link flow, `linked` channels). **DashboardPage** overview cell counts only `active && enabled` channels. Timers and request-race rules from the earlier alerts page stay.

- [ ] **Step 1: Write failing tests** only for pure logic added to `alertFormat.ts` in this task (if any); components have no RTL in this repo — keep decisions in pure functions.
- [ ] **Step 2: Implement** the components and edits.
- [ ] **Step 3: Run** `npx vitest run && npm run build && npm run lint`. Expected: PASS, no new warnings in touched files. State in the report that no browser check and no real-Telegram check were done.
- [ ] **Step 4: Commit** `feat(dashboard): telegram bot settings card and manual chat channels`.
