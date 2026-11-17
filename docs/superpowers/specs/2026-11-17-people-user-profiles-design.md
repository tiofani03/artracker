# People (User Profiles) + Replay "User" Tab — Design

**Status:** draft · **Date:** 2026-11-17
**Repos:** artracker (backend + dashboard), artracker-android-sdk, artracker-ios-sdk
**Plans:**
- Web: `docs/superpowers/plans/2026-11-17-people-web.md`
- Android: `docs/superpowers/plans/2026-11-17-people-android-sdk.md`
- iOS: `docs/superpowers/plans/2026-11-17-people-ios-sdk.md`

## Problem

Someone on support gets "user X says checkout failed", and today they must type X into the replay filter by hand. A replay also shows only `user_id`, with no traits (plan, country) and no device/app context. `user_properties` sent to `/events` are merged into each event and never stored as a profile.

## Goals

1. Session replay gets a **User** tab (next to Events / Network / Performance) showing:
   - who the user is: user id and traits;
   - the device/app context of *this* session;
   - other sessions by the same user, each clickable.
2. A new **People** section in the dashboard: a searchable list of end users and a per-user detail page with traits and a session history.
3. Both SDKs expose the same identity API, `identify(userId, traits)` plus `reset()`, and send the same wire contract.

## Non-goals (later)

- Segment filters by trait (`plan = pro`) on sessions or funnels.
- GDPR/UU PDP "delete user X everywhere". This needs deletion across telemetry, replay and object storage, so it gets its own spec.
- Stitching anonymous sessions to a user who logs in later.
- Web/JS SDK changes. Web replay sessions still get a profile row through `chunk-url` `user_id`.

## Naming

The dashboard section is called **People**, not "Users". "Users" already means team members (`users` table, Settings → Members).

## Wire contract (shared by both SDKs)

All identity data rides the existing telemetry endpoint, `POST /api/v1/ingest`. **There is no new SDK endpoint.**

### 1. Identify item

Emitted on every `identify(userId, traits)` call that changes the user or the traits.

```json
{ "type": "event", "name": "$identify", "user_id": "u_123", "session_id": "…",
  "timestamp": "…", "attributes": { "plan": "pro", "country": "ID", "signup_at": "2026-01-02" } }
```

- `attributes` **is** the trait map. It is flat, with no `traits` wrapper. This matches what Android already sends.
- Trait limits, enforced by the SDKs (best effort) and again by the backend (authoritative):
  - at most **50 keys**;
  - keys are 1–**64** chars;
  - values are string (≤ **256** chars, truncated), number, bool or null;
  - nested objects and arrays are dropped.
- The backend drops any key whose words (split on separators and camelCase) include `pass`, `password`, `passwd`, `pwd`, `secret`, `token`, `apikey`/`api key`, `authorization`, `auth`, `cookie`, `card`, `cvv`, `cvc`, `pin`, `nik` or `ssn`, or whose text contains `password`, `secret`, `apikey`/`api_key`/`api-key` or `authorization`. Word matching keeps keys like `shipping` and `discard_reason`.
- Trait merge is shallow and last-write-wins per key. A `null` value deletes that key from the profile.

### 2. Session-start item carries device context

- The item name is `session_start` (Android) or `session.start` (iOS). **The backend accepts both.**
- Its `attributes` use these keys. Every key is optional, and unknown keys are kept as they are.

| key | example | notes |
|---|---|---|
| `platform` | `android` / `ios` | |
| `os_version` | `14` / `17.4.1` | |
| `manufacturer` | `Google` / `Apple` | |
| `model` | `Pixel 8` / `iPhone15,2` | |
| `app_version` | `2.3.1` | |
| `app_build` | `231` | |
| `locale` | `id-ID` | BCP-47 |
| `tz` | `Asia/Jakarta` | |
| `network` | `wifi`/`cellular`/`ethernet`/`none`/`unknown` | |
| `is_emulator` | `false` | |
| `sdk_version` | `1.0.3` | |

### 3. User switch and logout semantics (both SDKs)

| Call | Session | Effect |
|---|---|---|
| `identify(A)` while anonymous | **keep** | The current session becomes A's. The backend fills the empty `user_id`. |
| `identify(A)` while already A | keep | Emits `$identify` only if the traits changed. |
| `identify(B)` while A | **rotate** | `session_end` is stamped with A, and the new `session_start` with B. One session never spans two users. |
| `reset()` | **rotate** | Clears the user id and traits. The new session is anonymous. |

The user id is persisted across app launches, so a relaunch keeps the user without calling `identify` again.

## Backend data model

`0016_people.sql`:

- `user_profiles(project_id, user_id, traits jsonb, first_seen, last_seen, last_session_id, last_context jsonb, created_at, updated_at)`
  - PK `(project_id, user_id)`;
  - index `(project_id, last_seen DESC)`.
- `session_contexts(project_id, session_id, user_id, first_seen, last_seen, has_error, context jsonb, updated_at)`
  - PK `(project_id, session_id)`;
  - partial index `(project_id, user_id, last_seen DESC) WHERE user_id <> ''`;
  - one row per session, **anonymous sessions included** (`user_id = ''`), so the User tab can still show device context.

Both tables are written by a row processor that runs after every successful telemetry flush. They are also touched from `POST /replay/chunk-url` when it carries a `user_id`.

- `session_contexts.user_id` is set only while it is still `''`, the same rule `replay_sessions` uses.
- `has_error` becomes true when any row in the session has level `error` or `fatal`.
- Counts are computed when read, not stored as counters.
- `user_profiles.traits_updated_at` holds the event time of the newest merged `$identify`. An older identify (e.g. one an offline device flushes late) does not overwrite newer traits.
- Stored traits are capped at 50 keys: once full, existing keys keep updating but new keys are ignored.
- Replay `start_time` comes from the client and is not skew-corrected, so it is clamped to `[now − 7 days, now + 5 min]` before it touches a profile.
- The migration backfills from `replay_sessions` and from the last 30 days of session-start and error telemetry (filtered on `received_at` so only recent partitions are read).

## Read API (dashboard, Bearer auth, project role ≥ viewer)

| Route | Response |
|---|---|
| `GET /api/v1/projects/:projectID/people?search=&limit=25&offset=0` | `{items: Person[], total, limit, offset}`, sorted by `last_seen DESC`. `search` is a case-insensitive prefix match on `user_id`, OR a substring match on any trait value. Max 128 chars. |
| `GET /api/v1/projects/:projectID/people/:userID` | `{person: Person, sessions: PersonSession[]}`, with the latest 50 sessions. Returns 404 if there is no profile. |
| `GET /api/v1/projects/:projectID/sessions/:sessionID/context` | `{session_id, user_id, context, person: Person \| null, other_sessions: PersonSession[]}`. `other_sessions` holds at most 10, newest first, excluding this session. Returns 200 with empty fields when unknown. |

```ts
Person        = { user_id, traits: Record<string, string|number|boolean>, first_seen, last_seen,
                  session_count: number, error_session_count: number, last_context: Record<string, unknown> }
PersonSession = { session_id, first_seen, last_seen, has_error: boolean, has_replay: boolean,
                  context: Record<string, unknown> }
```

## Dashboard UX

- **Replay → User tab:**
  - The identity card shows the user id with a copy button and a "View profile →" link that opens `/dashboard/people?user=<id>`.
  - Below it, in order: a Traits table, a "This session" device/app context list, and "Other sessions" rows (date, duration, error badge, replay badge). Clicking a row opens `/dashboard/replays?session=<id>`.
  - Anonymous sessions show the context plus an empty state with `identify` snippets for Android and iOS.
- **Session header:** the user id becomes a link to the People detail page.
- **People list** (`/dashboard/people`):
  - a search box;
  - a table with columns User, Key traits (first 3), Last seen, Sessions, Sessions with errors, Last device;
  - pagination.
- **People detail** (`/dashboard/people?user=<id>`): header with first and last seen, a traits table, the last device, and a sessions table with a replay link per row.
- **Navigation:** a "People" item under the Observability nav section, placed after Session Replay. It is not available in demo mode.
- **Docs page:** a new section titled "Identify users", with Android and iOS snippets and the trait limits.

## Success metrics

- The share of sessions with a non-empty `user_id`, measured per project in `session_contexts`.
- The time from receiving a support ticket to opening the relevant replay. Measured qualitatively with the support team before and after.
