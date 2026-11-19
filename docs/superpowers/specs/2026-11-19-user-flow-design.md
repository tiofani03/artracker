# User Flow (screen paths & time on screen) — Design

Date: 2026-11-19
Status: approved

## Intent

A new dashboard tab that shows, across all sessions of a mobile app, **how users move between screens
and how long they stay on each one**. The user asked for it to understand "flownya user tuh gimana, lama
di halaman apa". It is exploratory: you don't need to know the path in advance — pick a screen and see
where people go next (or came from), where they leave the app, and which screens hold them longest.

The existing **Funnel** (Analytics tab, `internal/analytics/funnel.go`) answers a different question:
conversion through a fixed list of *analytics event names*. It has no notion of screens or time on
screen and is not changed by this work.

## Goals

- Per screen: sessions that visited it, median and p75 time on screen, entry rate (first screen of the
  session) and exit rate (last screen of the session).
- Path explorer: pick a screen, see the next N steps (or the previous N steps) as a Sankey diagram, with
  "Exit" as an explicit destination so drop-off is visible.
- Filters: date range, platform, app version — same as the heatmap.
- Works for every existing screenshot-replay session through the same backfill path as the heatmap.

## Non-goals (v1)

- Screen-step funnels with conversion % (approach "B" in the conversation) — a later feature that can
  reuse the same `screen_views` table.
- Per-user/per-session flow views: the replay's existing Screens track already shows one session's path.
- Drill-down from a link in the diagram to the list of sessions/replays that took it. Rows keep
  `session_id`, so it can be added without re-extraction.
- `screen.view` telemetry as a source, and web (rrweb) sessions. The table carries a `source` column so
  `screen.view` can be added later without changing the API or the page.
- Real-time freshness (same ~10 min delay as the heatmap).

## Anti-goals

- One heavy user dominating the picture: everything is counted in **distinct sessions**, not raw views.
- Inflating paths with noise: SDK fallback names (`App`, `Screen`, empty) and iOS system windows must not
  appear as steps.
- Reading R2 at request time.

## Constraints (from the current code and data)

- Screen names come only from `frames[].screen` of `artracker-screen-v1` chunks. iOS often sends the
  fallback `App` (one real session: every frame). Such sessions produce no flow until the SDK sends real
  names; the page must say so instead of looking broken.
- Frames arrive several times per second while the screen changes, but can be minutes apart while it is
  static. Time on screen is therefore "until the next different screen", not "until the next frame".
- `screen_visits` (heatmap) keeps only the *first* visit per screen/layout per session — no order, no
  duration — so it cannot serve this feature.
- The heatmap worker (`internal/heatmap/worker.go`) already reads every chunk of every finished
  screenshot session, with backoff, retries and the exclusion lock.

## Approaches considered

1. **From replay frames, in the existing heatmap worker (chosen).** No SDK change; accurate durations;
   only covers recorded sessions (subject to replay sampling); ~10 min delay.
2. **From `screen.view` telemetry at ingest.** All sessions, real-time, but needs both SDKs to send the
   event reliably, and the last screen's duration has to be guessed.
3. **Both, telemetry preferred.** Widest coverage, roughly 1.5× the work and two write paths.

The user picked **1**. The table is source-neutral so 2 can be added later.

## Design

### Data model (new migration `0018_screen_flow.sql`)

`screen_views` — one row per contiguous stay on one screen within one session:

| column | notes |
|---|---|
| `project_id BIGINT`, `session_id TEXT` | FK to `replay_sessions (project_id, id)` `ON DELETE CASCADE` |
| `seq INT` | 0-based position in the session's path, after cleaning (see below) |
| `screen TEXT` | |
| `entered_at TIMESTAMPTZ` | ts of the first frame of the stay |
| `duration_ms INT` | see Extraction |
| `is_entry BOOL`, `is_exit BOOL` | first / last view of the session |
| `platform TEXT`, `app_version TEXT` | same values the heatmap stores for the session |
| `source TEXT NOT NULL DEFAULT 'replay'` | future-proofing for `screen.view` |

Primary key `(project_id, session_id, seq)`; index `(project_id, screen, entered_at)`.

No new processing-state columns: flow rows are written by the heatmap worker in the same transaction as
taps, so `taps_processed_at` / `taps_processed_chunks` already track them. *(Call: reusing the state keeps
one queue and one backfill; the column names stay tap-flavoured — acceptable, renaming is churn.)*

### Extraction (pure function in `internal/heatmap`, e.g. `ExtractFlow(chunks) []View`)

1. Take the same merged, ts-sorted frame stream `Extract` uses.
2. Drop frames whose screen is unrecognized (`isUnrecognized`: empty, `App`, `Screen`,
   `UI…WindowController`). They neither start nor end a stay. *(Call: an iOS system window flashing over
   a screen should not split it into two visits.)*
3. Collapse consecutive frames with the same screen into one stay. A → B → A yields three stays.
4. `duration_ms` = next stay's `entered_at` − this stay's `entered_at`. For the last stay: last frame/touch
   ts of the session − `entered_at`. Each stay is capped at `MaxStay` (30 min) so a backgrounded app does
   not produce absurd averages; the page shows medians/p75, never means. *(Call.)*
5. Stays shorter than `MinStay` (1 s) are dropped and their neighbours re-merged if they become equal
   (transition frames caught mid-animation). *(Call; validate on real data, keep as a named constant.)*
6. Renumber `seq`, set `is_entry` / `is_exit`.

The worker deletes the session's `screen_views` and inserts the new ones next to the existing
`screen_taps` / `screen_visits` writes, in the same transaction.

**Privacy.** Flow stores screen names and durations only — no positions, no images — so the built-in
sensitive-screen rule (which exists to protect tap positions on PIN pads) does **not** apply: seeing that
users drop off at `LoginViewController` is exactly what this feature is for. Screens in the project's
admin-managed `heatmap_excluded_screens` list are also kept in flow (user decision, 2026-11-19): adding a
screen to the list only deletes its heatmap rows, as today. The Settings "Heatmap" section says the list
applies to the heatmap only, so admins don't assume the screen is hidden everywhere.

### API (viewer role, same middleware as heatmap routes)

- `GET /projects/:id/flow/screens?from&to&platform&app_version` →
  `[{screen, platform, sessions, views, median_ms, p75_ms, entry_sessions, exit_sessions}]`, sorted by
  sessions; plus `app_versions` and `unrecognized_sessions` (sessions in range with zero views, for the
  "screen names missing" note).
- `GET /projects/:id/flow/paths?screen&platform&direction=next|prev&steps=1..5&from&to&app_version` →
  Sankey data: `nodes [{id, step, screen}]`, `links [{source, target, sessions}]`. Step 0 is the chosen
  screen (its first visit per session); for `next`, step k+1 is the view after step k, or `Exit` if the
  session ended. Per step only the top 6 screens are kept, the rest fold into `Other`. Default `steps=3`.
  Computed on read with SQL over `screen_views` (self-join on `seq`), 5 s timeout like the heatmap.
- `platform` is required on `paths` (screen names are platform-specific). Projects are single-platform
  (`projects.platform` check constraint), so in practice the UI passes the project's platform.
- `unrecognized_sessions` counts processed screenshot sessions in the date range with zero `screen_views`,
  excluding sessions the worker gave up on (`taps_attempts >= heatmap.MaxAttempts`); it ignores the app-version filter (such sessions have no row carrying a version).

### Dashboard

New nav item **User Flow** under *Observability*, after Heatmap, route `/dashboard/flow`, not allowed in
demo mode (like Heatmap).

```
┌ Filters: [Last 7 days ▾] [iOS ▾] [All versions ▾] ─────────────────────────────┐
│ Screens                         │  HomeViewController  ·  [Next ▸ | ◂ Previous]  │
│ ─────────────────────────────── │                                                │
│ HomeViewController  1.2k  0:42  │  Home ━━━━━━━┳━ Product  ━━┳━ Cart             │
│ ProductViewController 860 1:10  │              ┣━ Search   ━━┻━ Exit             │
│ CartViewController   310  0:25  │              ┗━ Exit (18%)                     │
│ …  (sessions · median time)     │                                                │
│                                 │  hover a link: "412 sessions (34%)"            │
└─────────────────────────────────┴────────────────────────────────────────────────┘
```

- Left: screen table (name, sessions, median time, exit rate) with search; clicking a row opens it in the
  path explorer. Default selection: the screen with the most entry sessions.
- Right: hand-written SVG Sankey (no chart library is in the project; heatmap also draws by hand). Columns
  are steps; link width = sessions; `Exit` styled as drop-off; clicking a node re-centres on that screen.
- Empty states: no screenshot replay; sessions exist but have no recognized screen names ("no recognizable
  screen names or replay data; if your SDK sends `App` as the screen name, send real screen names to see flows"); processing pending.

### Rollout / backfill

Sessions already processed for the heatmap have no `screen_views`. The migration resets
`taps_processed_at = NULL, taps_attempts = 0, taps_retry_at = NULL` for screenshot sessions so the worker
re-extracts them (taps are replaced idempotently). *(Call.)*

**Missing-chunk guard** (user decision, 2026-11-19). Whether R2 objects can disappear while their
`replay_sessions` row stays (lifecycle rule, manual cleanup) is unknown, and `loadChunks` silently skips
missing objects. A re-extraction must not destroy heatmap data it can no longer rebuild, so:

- `loadChunks` also reports how many chunks were missing.
- If at least one chunk was missing **and** the session already has `screen_taps` or `screen_visits`
  rows, the worker keeps the existing `screen_taps`, `screen_visits` and `screen_backgrounds` untouched and
  only replaces `screen_views` (built from the chunks that are still there). It logs a warning with the
  session and the missing count.
- Otherwise (nothing missing, or a session with no heatmap rows yet) it rewrites everything as today.
- `taps_processed_at` / `taps_processed_chunks` are set in both cases, so the session is not retried
  forever.

## Left to the builder

- `MinStay` / `MaxStay` / top-N values — start with the numbers above, check on real sessions.
- SQL shape for the paths query (recursive vs. fixed self-joins for ≤5 steps).
- SVG Sankey layout details.
- Tests: `ExtractFlow` is pure and gets table-driven unit tests (merge, unrecognized drop, short-stay
  re-merge, cap, entry/exit); handler tests on `artracker_test`, never the dev DB.
