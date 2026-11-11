# Alerting MVP (Telegram) — Design

Date: 2026-11-11 · Repo: `artracker` (backend + dashboard)
Plan: `docs/superpowers/plans/2026-11-11-alerting-telegram.md`

## 1. Intent

The "Custom Alerts" page today evaluates four hardcoded rules **in the browser** over whatever data the page
already loaded (`DashboardPage.tsx`, `ALERT_DEFINITIONS`). Nothing runs when nobody has the dashboard open, the
channels ("Slack · #ops-alerts") are labels, mute lives in React state, and rules like "open issues > 0" are
permanently red. This replaces it with server-side alerting that tells a developer **something changed and needs a
human now**, delivered to **Telegram** first.

Principle: an alert is a *change*. Standing conditions ("there are open issues") stay as badges.

## 2. Scope (MVP)

Three rule kinds, one row per project per kind (no free-form rule builder):

| Kind | Fires when | Resolves when |
| --- | --- | --- |
| `new_issue` | an error fingerprint is seen for the first time | the issue is set `resolved` or `ignored` |
| `regression` | a new event arrives for an issue whose status is `resolved`; the issue is reopened (`open`) | the issue is set `resolved` or `ignored` again |
| `issue_spike` | `current ≥ min_count` **and** `current ≥ multiplier × max(baseline, 1)` | the fire condition is false **and** the incident has been firing for at least one window |

Spike terms, for one open issue, evaluated every 60 s with `W = window_minutes`:
- `current` = events in `(now − W, now]`
- `baseline` = events in `(now − W − 24h, now − W]` × `W / 1440` (the average per window over the previous day)

Channel: **Telegram only**. Out of scope: Slack/Discord/email/webhook, crash-free-rate and 5xx rules, funnel
alerts, perf alerts, per-user routing, on-call schedules.

## 3. Decisions made by Claude (check these)

- **One bot per deployment.** The operator sets `TELEGRAM_BOT_TOKEN`. Without it, alert rules and incidents still
  work (visible in the dashboard), but channels cannot be connected and the UI says "Telegram is not configured".
- **Connecting a chat uses a deep link, never a typed chat id.** The dashboard asks for a single-use link code
  (16 chars `[a-z0-9]`, valid 15 minutes) and shows `https://t.me/<bot>?startgroup=<code>` (add to a group) and
  `https://t.me/<bot>?start=<code>` (private chat). Telegram then delivers `/start <code>` (or
  `/start@<bot> <code>`) to the bot. The API long-polls `getUpdates`, binds that chat to the project and replies
  "✅ Connected to <project>". A project may have several chats; a chat may serve several projects.
- **Only one API instance polls Telegram and runs the evaluator.** `ALERTS_WORKER=true` (default). Extra
  replicas must set it to `false`. Two pollers get HTTP 409 from Telegram.
- **Defaults** are seeded for every project (existing and new): all three rules enabled, `environment = 'production'`,
  `spike_multiplier = 5`, `spike_min_count = 20`, `spike_window_minutes = 60`, `cooldown_minutes = 60`.
  `environment = ''` means every environment.
- **Dedup:** at most one *firing* incident per (project, kind, issue). `new_issue` therefore fires once per issue.
  A spike does not refire within `cooldown_minutes` after its previous incident resolved.
- **Delivery is an outbox.** Incidents are written with `notify_state = 'pending'`. A dispatcher every 5 s sends
  them. Ingest never waits for Telegram.
- **Flood guard:** when more than 3 fire notifications are pending for one project in a dispatch tick, the
  dispatcher sends **one digest** (first 5 titles + "+N more" + a link to the Alerts page) instead.
- **Recovery messages only for spikes** ("✅ Spike recovered"). Closing a `new_issue` or `regression` incident by
  resolving the issue sends nothing, because the person who resolved it already knows.
- **Messages use `parse_mode=HTML` with links in the text, no inline-keyboard buttons.** Telegram rejects URL buttons
  that point at `localhost`, and links in the text work in every environment. All user data is HTML-escaped.
  Titles are cut to 200 characters.
- **Deep links carry the workspace:** `…/dashboard/issues/<id>?org=<orgId>&project=<projectId>`. The dashboard
  shell selects that org/project on load, when the user is a member. The replay link
  (`/dashboard/replays?session=<sid>&org=…&project=…`) is added only when a `replay_sessions` row exists for the
  session.
- **Spike counts per environment** use a new `issue_events.environment` column. Rows written before this change have
  `''`, which counts as matching any environment, so the first day's baseline is not artificially low.
- **Telegram failures:** a 429 leaves the incident pending and the dispatcher retries after `retry_after`. A 403
  (the bot was removed or blocked) disables the channel and sets `disabled_reason`. A 400 carrying
  `migrate_to_chat_id` (a group upgraded to a supergroup) updates the chat id and retries once. After 5 failed
  attempts an incident becomes `notify_state = 'failed'`.

## 4. Where alerts surface

- **Telegram** (primary). Example:
  ```
  🆕 New issue · Checkout Web
  TypeError: Cannot read properties of undefined (reading 'charge_id')
  production · release 1.2.3
  Open issue · Watch replay
  ```
  Emoji per kind: 🆕 new_issue, 🔁 regression, 📈 issue_spike (adds "142 events in 60 min, baseline 6"),
  ✅ spike recovered, 🚨 digest.
- **Alerts page** (`/dashboard/alerts`), three cards: *Telegram channels* (connect / test / disable / remove),
  *Rules* (toggle, environment, spike thresholds, cooldown; admins only), *Incidents* (firing and resolved, linked to
  the issue).
- **Sidebar badge** on "Alerts": the count of firing incidents (`nav-counts.firing_alerts`, tone `alert`).
- **Overview metric cell** "Alert Rules" becomes "Active alerts": the firing count plus the channel count.

## 5. Data model (migration `0011_alerts.sql`)

- `alert_rules(project_id, kind, enabled, environment, spike_multiplier, spike_min_count, spike_window_minutes,
  cooldown_minutes, updated_at)`, PK `(project_id, kind)`.
- `alert_channels(id, project_id, kind='telegram', telegram_chat_id, title, chat_type, enabled, disabled_reason,
  last_sent_at, last_error, created_by, created_at)`, `UNIQUE(project_id, telegram_chat_id)`.
- `alert_link_codes(code PK, project_id, created_by, expires_at, used_at)`.
- `alert_incidents(id, project_id, rule_kind, issue_id, status, title, environment, release_version, session_id,
  detail jsonb, fired_at, resolved_at, notify_state, notify_attempts, next_attempt_at, recovery_notify_state)`, with a
  partial unique index on `(project_id, rule_kind, issue_id) WHERE status = 'firing'`.
- `issue_events.environment TEXT NOT NULL DEFAULT ''`.
- `issues.status` is unchanged. A regression moves `resolved` → `open` during ingest; `ignored` stays ignored and
  never alerts.
