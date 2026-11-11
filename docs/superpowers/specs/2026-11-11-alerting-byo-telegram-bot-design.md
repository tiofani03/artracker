# Per-tenant Telegram bot (send-only) — Design

Date: 2026-11-11 · Repo: `artracker` (backend + dashboard)
Builds on: `docs/superpowers/specs/2026-11-11-alerting-telegram-design.md` and its plan
(`docs/superpowers/plans/2026-11-11-alerting-telegram.md`), implemented on branch `feat/alerting-telegram`
(head `a81d68b`). Read those first: the rules, incidents, outbox dispatcher and spike evaluator are
unchanged by this design. Only the **channel and delivery layer** changes.

## 1. Intent

The platform will be sold as a SaaS to several companies, and each company has its own regulations.
The alerting MVP uses **one Telegram bot owned by the operator** and links chats with a `/start <code>`
message that the operator's server receives by long polling. That has two problems for this product:

1. **Privacy law (Indonesia's PDP law).** With the operator's bot, the operator's server receives
   messages from every tenant's chats and could read them. A company cannot accept that.
2. **Control.** Tenants want to customise their own bot (name, avatar, which groups it joins).

So each tenant brings **its own bot**. The platform only ever **sends** messages through it.

Decided by the product owner, in their words: tenants make their own bot; the server must
**only send** and never receive anything from Telegram; nothing hardcoded; the token is entered in
the dashboard and must be changeable at runtime without a restart or redeploy.

## 2. Goals

- An organisation admin connects the organisation's own bot from the dashboard, and can replace or
  remove it at any time. The change takes effect immediately, with no restart.
- **The platform never reads Telegram messages.** This is enforced by construction: the code never
  calls `getUpdates`, never registers a webhook, and has no endpoint Telegram could call.
- The bot token is a secret: encrypted at rest, write-only through the API, never in logs, errors,
  metrics, URLs or API responses after it is saved.
- One tenant's problem (revoked token, Telegram rate limit, deleted chat) never delays or hides
  another tenant's alerts.
- Nothing is lost silently: when an alert cannot be delivered, the dashboard shows why.
- Alert rules, incidents, the outbox dispatcher and the spike evaluator keep working unchanged.

## 3. Non-goals

- No shared operator bot offered to tenants in SaaS mode (see §6.8 for what happens to it).
- No inbound features of any kind: no `/start` linking, no replying to the bot, no inline-button
  callbacks, no acknowledging an alert from Telegram. All of these need inbound updates.
- No other channels (Slack, Discord, email) in this work.
- No tenant-managed encryption keys (bring-your-own-key) in this work.
- This design is not legal advice and does not certify PDP compliance. Whether it satisfies a
  specific company's obligations is for that company's lawyer or data protection officer.

## 4. Anti-goals (a failure even if it technically works)

- Any code path where the platform receives Telegram updates for a tenant's bot.
- The token readable through any API response, log line, error message, metric, URL, or stored in
  plaintext (database, backups, caches that outlive a request).
- A tenant able to point the platform at an arbitrary URL (the Telegram host is fixed).
- An alert dropped with no visible reason.

## 5. Decisions made by the product owner

1. Each tenant uses its own bot; the operator does not provide the bot.
2. The server is **send-only**: it never receives anything from Telegram.
3. No hardcoding; the token is entered in the dashboard and changes apply at runtime.

## 6. Design (every item below is Claude's call unless it repeats §5; check these)

### 6.1 Ownership and data model

- **One bot per organisation** (not per project). Projects of the organisation share it. A per-project
  override is out of scope.
- New table `alert_bots`: `org_id` (primary key, FK to organisations, `ON DELETE CASCADE`),
  `token_ciphertext`, `key_id`, `bot_id`, `bot_username`, `status` (`active` | `invalid`),
  `status_reason`, `detail_level` (`minimal` | `full`, §6.5), `created_by`, `created_at`, `updated_at`,
  `last_validated_at`.
- `alert_channels` (already per project, one row per destination chat) keeps its shape. A channel now
  means "send to this chat id through the organisation's bot". Its destination must be stored so that
  both a numeric chat id and a public `@channelusername` are possible. The migration from the current
  numeric column is the builder's to design.
- Channels need a human label entered by the admin (the platform cannot learn the chat title without
  reading from Telegram). Optionally the platform may call `getChat` once at save time to show the
  title and type; that returns metadata only, never messages. Treat `getChat` as optional.
- The link-code table and flow (`alert_link_codes`, `/start`, `PollUpdates`, `HandleUpdate`, the
  "bot removed from chat" update handling) exist only for the shared-bot mode (§6.8).

### 6.2 Secret storage

- **Envelope encryption.** The token is encrypted with AES-256-GCM using a data key per organisation;
  data keys are wrapped by one **master key** supplied by the deployment (environment variable or file
  path, same convention as `TELEGRAM_BOT_TOKEN_FILE`). Ciphertext carries a `key_id` so the master key
  can be rotated. The organisation id is bound to the ciphertext as additional authenticated data, so
  a ciphertext copied to another organisation fails to decrypt.
- Put encryption behind a small interface so a cloud KMS can replace the local master key later.
- The master key is the one secret that stays outside the database. If tenant-bot mode is on and the
  master key is missing or malformed, the process refuses to start in production (same fail-fast rule as
  `JWT_SECRET`); in development it must be set explicitly.
- Decrypted tokens exist only in memory, only while a request or a dispatcher tick needs them.

### 6.3 API (organisation level, owner or admin)

- `PUT /orgs/:orgID/alerts/telegram-bot` with `{token, detail_level?}`: validates the token with
  `getMe`, stores it encrypted, returns `{bot_username, status, detail_level, updated_at}`. Replacing
  overwrites; there is no way to read the old token back.
- `GET /orgs/:orgID/alerts/telegram-bot`: returns only `{configured, bot_username, status,
  status_reason, detail_level, updated_at}`. Readable by any member; never the token.
- `PATCH` for `detail_level`; `DELETE` removes the bot and pauses the organisation's channels with the
  reason "no bot configured".
- Project-level channel endpoints keep their routes and roles from the MVP. Creating a channel now takes
  `{chat, label}` instead of using a link code. `POST .../channels/:id/test` sends a test message through
  the organisation's bot. The link-code endpoint returns 404/disabled outside shared-bot mode.

### 6.4 Delivery

- The dispatcher resolves project → organisation → bot, decrypts the token for the tick, and sends with a
  Telegram client built for that token. The Telegram API host is fixed in code; a tenant can never supply
  a URL. (`TELEGRAM_API_BASE` remains for tests only.)
- Failure handling per bot and per channel, isolated across tenants:
  - Telegram 401 on a bot: mark the bot `invalid` with a reason; pause that organisation's channels; show
    a banner in the dashboard. Other organisations are unaffected.
  - 403 / "chat not found" / "bot was kicked" on a channel: disable that channel with the reason.
  - 429: keep pending and retry after the delay Telegram gives (the MVP's existing handling), per bot.
  - A group upgraded to a supergroup (`migrate_to_chat_id`): update the stored chat and retry once, as in
    the MVP.
- Respect Telegram's limits per bot and per chat so one noisy tenant does not exhaust its own bot's
  budget and starve its own critical alerts; exact numbers are the builder's.

### 6.5 What a message contains (data minimisation)

Alert text can contain personal data, for example an error message that includes an email address.
Each organisation chooses a **detail level**:

- `minimal` (**default**, safe by default): kind of alert, project name, environment, release, and the link to
  the issue. No error title, no session id, no replay link text from user data.
- `full`: today's message, including the issue title.

This setting matters for privacy at least as much as who owns the bot, because the content still passes
through Telegram.

### 6.6 Send-only, enforced

- Remove the poller from the tenant path; no inbound HTTP endpoint for Telegram exists.
- The only Telegram methods the platform may call are `sendMessage`, `getMe`, and optionally `getChat`.
  Tests run against a fake Telegram server that **fails the test if any other method is called**.
- The "only one instance may poll Telegram" constraint disappears. The dispatcher and the evaluator still
  need a single worker (`ALERTS_WORKER`), unchanged.

### 6.7 Dashboard

- **Organisation settings, "Telegram bot" card (admins):** a write-only token field, the bot's name and
  status once connected, replace and remove actions, the detail level, and a short how-to (create a bot
  with @BotFather, paste the token). The token field is cleared after saving and never shown again.
- **Alerts page, Channels card (project admins):** "Add chat" takes a chat id (or `@channelusername`) and a
  label, with short instructions for finding a chat id without involving the platform; "Test" sends a test
  message; each channel shows its status and, when disabled or failing, the reason. When the organisation
  has no bot: admins see a prompt to connect one; other members see that an admin must.
- A visible banner on the Alerts page when the bot is `invalid` or missing.

### 6.8 The existing shared bot and link flow

- Keep the code, but put it behind `ALERTS_SHARED_BOT` (default **off**), intended for self-hosted or
  internal use where the operator owns the data. In SaaS mode it is off, so no code path receives tenant
  messages. `TELEGRAM_BOT_TOKEN` and `TELEGRAM_BOT_TOKEN_FILE` (added in an uncommitted change on
  `feat/alerting-telegram`) apply only in shared-bot mode.
- Channels already created through the link flow keep working only when the flag is on. When it is off they
  are disabled with the reason "bot not configured", and nothing is deleted.
- Alternative the product owner may prefer: delete the shared-bot code outright.

## 7. Constraints

- Same stack as the MVP: Go (Fiber, pgx), PostgreSQL, React + TypeScript; extend `backend/internal/alerts`.
- No new external services. Encryption uses the Go standard library.
- Logs must never contain tokens, message bodies, or decrypted values.
- Existing behaviour of rules, incidents, the outbox, the evaluator, deep links in messages, and the sidebar
  badge must not change.

## 8. Left to the builder

Crypto package layout and key-id format; whether to cache decrypted tokens within a tick; the exact rate-limit
numbers; the migration for the channel destination column; UI copy and the wording of the chat-id instructions;
how the master key is generated and documented; test layout.
