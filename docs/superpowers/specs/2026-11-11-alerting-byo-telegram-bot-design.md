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
- No reading chat details from Telegram: `getChat` is not called (it returns pinned messages, descriptions
  and private-chat names). In tenant mode the only Telegram methods the platform calls are `sendMessage`
  and `getMe`.
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
- `alert_channels` (already per project, one row per destination chat) keeps its shape and gains two
  **additive** columns; nothing existing is dropped or rewritten. `origin` (`linked` | `manual`, existing
  rows are `linked`, §6.8) and `destination` (TEXT: a numeric chat id as a string, or a public
  `@channelusername`). `manual` channels use `destination`; `linked` channels keep using the existing
  `telegram_chat_id`, which becomes nullable. Uniqueness: keep `UNIQUE (project_id, telegram_chat_id)` and add a
  partial unique index on `(project_id, destination)` for `manual` rows. The `migrate_to_chat_id` handling
  (group upgraded to supergroup) must update whichever column the channel uses.
- Channels need a human label entered by the admin (the platform cannot learn the chat title without
  reading from Telegram). The platform does not call `getChat`: it returns chat content (pinned message,
  description, private-chat names), which contradicts decision 2. Manual channels are saved with an empty
  `chat_type`.
- The link-code table and flow (`alert_link_codes`, `/start`, `PollUpdates`, `HandleUpdate`, the
  "bot removed from chat" update handling) exist only for the shared-bot mode (§6.8).

### 6.2 Secret storage

- **Envelope encryption.** The token is encrypted with AES-256-GCM using a data key per organisation;
  data keys are wrapped by one **master key** supplied by the deployment (environment variable or file
  path, same convention as `TELEGRAM_BOT_TOKEN_FILE`). Ciphertext carries a `key_id` so the master key
  can be rotated. The organisation id is bound to the ciphertext as additional authenticated data, so
  a ciphertext copied to another organisation fails to decrypt.
- Put encryption behind a small interface so a cloud KMS can replace the local master key later.
- The master key is the one secret that stays outside the database. It is read from `ALERTS_MASTER_KEY`
  (base64 of exactly 32 random bytes) or from the file named by `ALERTS_MASTER_KEY_FILE`; setting both, or an
  unreadable/empty file, is a startup error, the same convention as `TELEGRAM_BOT_TOKEN_FILE`. It is required
  whenever the bot mode is `tenant` (§6.8). In production a missing or malformed key stops the process at
  boot (same fail-fast rule as `JWT_SECRET`). In development the process still starts, the alerts engine keeps
  running, and only the bot API answers "alerts master key is not configured". `.env.example` documents how
  to generate one (`openssl rand -base64 32`) and ships no usable value.
- Key rotation in this work is only the `key_id` column and the decrypt-by-`key_id` path; no rotation tool or
  admin endpoint. Each organisation's data key is generated randomly and wrapped by the master key (not
  derived from it).
- Decrypted tokens exist only in memory, only while a request or a dispatcher tick needs them.

### 6.3 API (organisation level, owner or admin)

- `PUT /orgs/:orgID/alerts/telegram-bot` with `{token, detail_level?}`: validates the token with
  `getMe`, stores it encrypted, returns `{bot_username, status, detail_level, updated_at}`. Replacing
  overwrites; there is no way to read the old token back.
- `GET /orgs/:orgID/alerts/telegram-bot`: returns only `{configured, bot_username, status,
  status_reason, detail_level, updated_at}`. Readable by **any organisation member** (the project-level Alerts
  page needs `configured` and `status` for every viewer); never the token.
- `PUT`, `PATCH` (for `detail_level`) and `DELETE` need `RequireOrgRole(auth.RoleAdmin)`, which already includes
  owners. `DELETE` removes the bot; the organisation's `manual` channels then count as paused because
  there is no working bot. Pausing is derived, not written (§6.4), so re-adding a bot resumes them with no
  re-enabling step.
- Adding a channel asks Telegram nothing: the chat is not verified, `chat_type` stays empty and the response
  carries no warning. "Test" is how an admin checks that the bot can post there. The admin-typed label is
  always required and always shown.
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
  - 429: keep pending and retry after the delay Telegram gives (the MVP's existing handling). The backoff
    scope is the **bot**, so it defers every pending incident of every project of that organisation, and
    nobody else's.
  - No working bot (none configured, or `invalid`): incidents stay `pending` instead of becoming `skipped`,
    so "nothing lost silently" holds and fixing the token delivers them. They expire after **24 hours**:
    older pending incidents become `skipped` with a reason. After a fix, the dispatcher's existing digest rule
    (more than 3 pending fires for one project in a tick become one digest message) prevents a flood.
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

The level applies to **every message the platform sends**: alert, recovery and the test message. `minimal`
keeps the aggregate numbers of a spike alert (event count, baseline, window), which are not personal data, so
spike alerts stay useful; it drops only the free text and identifiers that come from users' data. Incident rows
in the platform's own database still store the title; this setting governs what leaves for Telegram.

This setting matters for privacy at least as much as who owns the bot, because the content still passes
through Telegram.

### 6.6 Send-only, enforced

- Remove the poller from the tenant path; no inbound HTTP endpoint for Telegram exists.
- The only Telegram methods the platform calls in tenant mode are `sendMessage` and `getMe`. It never calls
  `getChat` or `getUpdates` for a tenant's bot.
  Tests run against a fake Telegram server that **fails the test if any other method is called**.
- The "only one instance may poll Telegram" constraint disappears. The dispatcher and the evaluator still
  need a single worker (`ALERTS_WORKER`), unchanged.

### 6.7 Dashboard

- **Organisation settings, "Telegram bot" card (admins)** on the existing `SettingsPage` next to the team
  members panel (the Alerts-page banner links there): a write-only token field, the bot's name and
  status once connected, replace and remove actions, the detail level, and a short how-to (create a bot
  with @BotFather, paste the token). The token field is cleared after saving and never shown again.
- **Alerts page, Channels card (project admins):** "Add chat" takes a chat id (or `@channelusername`) and a
  label, with short instructions for finding a chat id without involving the platform; "Test" sends a test
  message; each channel shows its status and, when disabled or failing, the reason. When the organisation
  has no bot: admins see a prompt to connect one; other members see that an admin must.
- A visible banner on the Alerts page when the bot is `invalid` or missing.

### 6.8 The existing shared bot and link flow

- A deployment runs in exactly one **bot mode**, set by `ALERTS_BOT_MODE`: `tenant` (the default, for SaaS) or
  `shared` (self-hosted or internal use where the operator owns the data). The modes are **mutually
  exclusive and never mixed**: there is no fallback from a tenant without a bot to the shared bot, because
  that fallback is exactly the path that would let the operator receive a tenant's messages.
- In `tenant` mode no code path receives Telegram updates: the poller, link codes and `/start` handling do
  not run. In `shared` mode the MVP behaves exactly as today, and `TELEGRAM_BOT_TOKEN` /
  `TELEGRAM_BOT_TOKEN_FILE` (the latter added in an uncommitted change on `feat/alerting-telegram`) apply only
  there. The master key (§6.2) is not needed in `shared` mode.
- Channels carry an `origin`: `linked` (created by the link flow, destination is a numeric chat id of a chat
  where the shared bot was added) or `manual` (created by an admin typing a chat id, delivered through the
  organisation's bot). A channel is **active only in the mode that matches its origin**: `linked` in
  `shared` mode, `manual` in `tenant` mode. This is derived at read time. **No rows are rewritten and nothing
  is deleted**, so switching the mode back and forth is lossless and a channel disabled for a real reason
  (for example a 403) is never confused with one that is merely inactive in the current mode. The dashboard
  shows an inactive channel as "belongs to the shared bot".
- Old `linked` chat ids are not assumed valid for a tenant's own bot (a different bot was added to those
  chats); they stay inactive in `tenant` mode rather than being tried.
- Alternative the product owner may prefer: delete the shared-bot code outright.

## 7. Constraints

- Same stack as the MVP: Go (Fiber, pgx), PostgreSQL, React + TypeScript; extend `backend/internal/alerts`.
- No new external services. Encryption uses the Go standard library.
- Logs must never contain tokens, message bodies, or decrypted values.
- Existing behaviour of rules, incidents, the outbox, the evaluator, deep links in messages, and the sidebar
  badge must not change.

## 8. Left to the builder

Crypto package layout and key-id format; whether to cache decrypted tokens within a tick; the exact rate-limit
numbers; the exact SQL of the additive channel columns (§6.1); UI copy and the wording of the chat-id
instructions; test layout.

## 9. Calls made after the builder check (Claude's calls, listed for a quick check)

1. The two bot modes (`tenant`, `shared`) are mutually exclusive, chosen by `ALERTS_BOT_MODE`, default `tenant`;
   no fallback from a tenant without a bot to the shared bot (§6.8). This replaces the earlier `ALERTS_SHARED_BOT` flag.
2. Channels get an `origin`; activity is derived from `origin` and the mode at read time. No rows are rewritten,
   so nothing is lost and mode switches are reversible (§6.8).
3. The channel destination is an additive TEXT column; the old numeric column stays for `linked` channels (§6.1).
4. Org settings card lives on the existing `SettingsPage`; `GET` for any member, `PUT`/`PATCH`/`DELETE` for org admins
   (owners included) (§6.3, §6.7).
5. The detail level applies to alert, recovery and test messages; `minimal` keeps spike counts (§6.5).
6. Rate limiting and the 429 backoff are per bot, that is per organisation (§6.4).
7. With no working bot, incidents stay `pending` for up to 24 hours, then become `skipped` (§6.4).
8. `getChat` is not called at all; manual channels are saved with an empty `chat_type` (§6.1, §6.3, §6.6).
9. Master key: `ALERTS_MASTER_KEY` (base64, 32 bytes) or `_FILE`; required in `tenant` mode; fatal at boot in production,
   an API error in development; rotation is schema only (§6.2).
