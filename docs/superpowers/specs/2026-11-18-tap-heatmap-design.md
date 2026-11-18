# Tap Heatmap per Screen — Design

Date: 2026-11-18
Status: approved

## Intent

A new dashboard tab that shows, for one screen of a mobile app, **where users tap most**. The goal is to
read behaviour patterns ("on Checkout, people tap the banner, not the CTA"), not to investigate single
sessions. Session replay is the only source of truth: taps are derived from the screenshot replay chunks
the SDKs already upload. **No SDK change.**

## Goals

- Pick a project + screen and see a heatmap of taps over a representative screenshot of that screen.
- Filter by time range, platform and app version (layouts change between versions).
- Show rage taps as a separate, toggleable layer.
- Exclude sensitive screens (PIN, password, OTP, login, payment …) from collection.
- Old replays are included through a one-off backfill.

## Non-goals (v1)

- Drill-down from a zone to the sessions/replays that tapped there.
- Comparing two versions / platforms side by side.
- Dead-tap detection ("tap that changed nothing"): not reliable from screenshots alone. Tap rows keep
  enough data (session, ts, position) to add it later without re-extracting.
- Element names / "most tapped button" ranking: needs a view tree, which the SDKs don't send.
- Web (rrweb) and `artracker-mobile-v1` (view-tree) replays.
- Real-time freshness.

## Anti-goals

- Leaking keystroke positions on PIN pads / keyboards through aggregated tap positions.
- One heavy user dominating a heatmap (counts must be per session, not raw taps).
- Opening a heatmap that reads R2 at request time.

## What the data looks like (checked on real data, 2026-11-18)

All chunks of the two test projects (`alfagift-ios-test`, `alfagift-android-test`; 6 sessions, ~190
chunks) are `artracker-screen-v1`, gzip JSON at `projects/{pid}/sessions/{sid}/chunk_{seq}.gz`:

- `frames[]`: `ts, screen, w, h, vw, vh, mime, masked (count only), data (base64 JPEG)`.
- `touches[]`: `ts, x, y, kind ∈ {down, move, up}`; `x/y` are in the frame's `vw/vh` logical space.
- Screen names are meaningful on Android (`android://ShoppingFragment`) and mostly on iOS
  (`GiftCardViewController`), but iOS often falls back to `App` (one session: 120/120 frames) and
  emits system windows such as `UITrackingElementWindowController`.
- Frame gaps: median 0.7–4.7 s, sometimes minutes while the screen is static.
- `masked` is a count, not rectangles; there is no keyboard indicator; iOS `masked` is always 0. So
  privacy cannot be enforced per region — only per screen.
- One `replay_chunks` row pointed at an object that is missing in R2 (404).

## Approaches considered

- **A. Extract per chunk on `chunk-ack`.** Near real-time, but a tap at the start of chunk N needs the
  last screen of chunk N-1, so per-session state must survive out-of-order / missing chunks.
- **B. Extract per session once the session is idle (chosen).** A background loop picks sessions with
  no new chunk for a while, reads all their chunks in order, writes taps, marks the session processed.
  Chunk boundaries and ordering stop mattering; missing chunks are skipped; backfill is the same path.
  Cost: ~10+ min delay, which is fine for "reading patterns".
- **C. Compute at request time from R2, cached.** No new tables, but slow and R2 cost grows with every
  view; only works at test-data scale.

The user picked **B**.

## Design

### Data model (new migration `0017_tap_heatmap.sql`)

- `replay_sessions` gains `taps_processed_at TIMESTAMPTZ NULL` and `taps_processed_chunks INT NOT NULL
  DEFAULT 0` (chunk_count seen at processing time; a session whose `chunk_count` later grows is
  reprocessed).
- `screen_taps`:
  `project_id, session_id, screen TEXT, ts TIMESTAMPTZ, x_norm REAL, y_norm REAL (0–1), vw REAL,
  vh REAL, aspect_bucket REAL, platform TEXT, app_version TEXT, landscape BOOL, rage BOOL`. FK to
  `replay_sessions` with `ON DELETE CASCADE`. Index on `(project_id, screen, ts)`.
- **Aspect bucket** (user decision): taps from different screen shapes are not pooled.
  `aspect_bucket = round(max(vw,vh) / min(vw,vh), 1)` (e.g. 402×874 → 2.2, 320×716 → 2.2, 360×640 → 1.8).
  Raw `vw/vh` are stored too, so the bucketing rule can change later without re-extracting.
- `screen_visits`: one row per (session, screen, aspect_bucket, landscape) the session showed, with the
  same `platform, app_version` and first-seen `ts`. This is the heatmap's denominator ("sessions that opened
  this screen"); sessions that visit without tapping are otherwise invisible.
- `screen_backgrounds`: one reference screenshot per `(project_id, screen, platform, app_version,
  landscape, aspect_bucket)`: `session_id, chunk_seq, frame_index, vw, vh, masked, captured_at`. The image itself stays
  in R2; the dashboard fetches it through the existing chunk download path.
- `projects.heatmap_excluded_screens TEXT[]` (admin-managed, see Privacy).

### Extraction worker (backend, new package `internal/heatmap`)

- Runs in the API process like the alerts worker, guarded by a config flag (default on) so only one
  instance runs it in multi-instance deployments.
- Every minute: select sessions where `updated_at < now() - idle` (idle = 10 min, configurable) and
  (`taps_processed_at IS NULL` or `chunk_count > taps_processed_chunks`), oldest first, small batch.
- Per session, in one transaction after reading:
  1. Read all chunks in `chunk_seq` order from the `ReplayStore`; skip non-`artracker-screen-v1`
     payloads and missing objects (log, don't fail).
  2. Merge frames and touches across chunks, sort by `ts`.
  3. Pair `down → up` into gestures. A **tap** is a gesture with `down→up` distance < 10 dp and
     duration < 500 ms (to be validated on real data). Everything else is dropped.
  4. Screen of a tap = the latest frame with `ts <= tap.ts` (same rule as the frontend's
     `activeScreenFrameAt`). Taps before the first frame are dropped. If the latest frame is older than
     a staleness limit (e.g. 30 s), drop the tap: the screen may have changed without a frame.
  5. Normalize `x/vw`, `y/vh` with that frame's `vw/vh`; clamp to 0–1; `landscape = vw > vh`.
  6. **Rage**: ≥3 taps within 1 s whose points are within ~24 dp of each other; all of them get
     `rage = true`.
  7. Skip taps whose screen is excluded (see Privacy) or is unrecognized (`App`, `Screen`, empty, or
     a known system window). Unrecognized screens are counted per session for a "N taps on unrecognized
     screens" note in the UI, but not stored as taps.
  8. `platform` from the chunk payload; `app_version` = the session's most frequent non-empty
     `telemetry_events.release_version` (empty if none). Checked on real data: every telemetry event of
     all 6 replay sessions carries it under the same `session_id` (iOS `4.57.0 (4570001)`, Android
     `4.57.0`); the value is used verbatim, no cross-platform normalization.
  9. Delete existing `screen_taps` of the session, insert the new rows, upsert `screen_backgrounds`
     (prefer the most recent frame with the fewest `masked`), set `taps_processed_at`,
     `taps_processed_chunks`.
- Backfill: nothing special — existing sessions have `taps_processed_at IS NULL` and are picked up.
  A `heatmap` admin CLI flag/command can reset sessions for a project to force reprocessing.

### Privacy

- A tap is never stored if its screen matches the built-in sensitive rule, or if it is listed in
  `projects.heatmap_excluded_screens`. The rule is **word-based** (the screen name is split into words at
  CamelCase / `_` / `-` / `/` / `:` / `.`): a word equal to `pin`, `otp`, `cvv`; a word starting with
  `password`, `passcode`, `login`, `signin`, `payment`, `secur`; or the word pairs `sign in`,
  `card number`. A plain substring pattern was rejected while planning because `pin` matches
  `ShoppingFragment`. The rule is deliberately narrow (no bare `card`, which would hide
  `GiftCardViewController`); admins add what it misses.
- Exclusion is applied at extraction: an excluded screen gets no `screen_taps`, no `screen_visits` and
  no `screen_backgrounds` row, so it never appears in the screen list and its screenshot is never shown
  as a reference. Adding a screen to the list deletes all three kinds of rows for it. Removing a screen
  only affects future extraction; reprocess the project to fill it in. *(Builder-check call.)*
- Only coordinates are stored; images stay in R2 as today.

### API

- `GET /projects/:id/heatmap/screens?from&to&platform&app_version` → screens with session count and
  tap count, sorted by sessions; plus the unrecognized-screen tap count.
- `GET /projects/:id/heatmap?screen&from&to&platform&app_version&orientation&aspect&grid=…`:
  `screen`, `platform` and `orientation` are required (screen names are platform-specific anyway, and
  portrait/landscape taps on one grid are meaningless); the UI defaults them to the selected screen's
  platform and `portrait`. `aspect` is optional; without it the bucket with the most sessions (in
  `screen_visits` for the filters) is used, and the response lists all buckets with their session
  counts for the "Screen size" dropdown. `app_version` is optional; without it the background is the one of the
  latest version (by most recent `captured_at`) in the filtered range. *(Builder-check call.)* Returns:
  - `grid`: cols × rows cells (default 40 × 80, scaled to aspect), each with `sessions` (distinct
    sessions that tapped in the cell) and `taps`;
  - `rage`: list of rage tap points (capped);
  - `screen_sessions`: distinct sessions in `screen_visits` for this screen and filters (denominator);
  - `background`: reference to the screenshot (session, chunk, frame index, vw, vh).
- `GET/PATCH /projects/:id/heatmap-settings` (admin) for the exclusion list.
- All viewer-level except settings (admin), same middleware as other project routes.

### Dashboard

- New nav item **Heatmap** under *Observability*, route `/dashboard/heatmap`.
- Left: screen list (name, sessions, taps) with search; filters for date range, platform, app version,
  orientation, and a "Screen size" dropdown (aspect buckets with session counts, most common selected).
  A bucket with few sessions (< 20) shows a "too few sessions for a reliable pattern" note.
- Main: the background screenshot with a blurred heat overlay (canvas), legend in "% of sessions that
  opened this screen and tapped here"; toggle "Rage taps" (red dots); hover a spot → percentage + taps.
- Notes in the UI: "Positions follow the screen, not scrolled content"; "N taps on unrecognized screens
  hidden"; data delay ("updated ~10 min after a session ends").
- Settings page: a "Heatmap" section listing excluded screens (built-in pattern shown read-only, custom
  list editable).
- Empty states for: no screenshot replay, screen without taps, processing pending.

## Left to the builder

- Exact tap thresholds (distance, duration, staleness, rage window/radius) — start with the values
  above, validate against the test projects, keep them as named constants.
- Heat rendering library vs. a small hand-written canvas blur.
- Worker batch size and pacing against R2.
- Test strategy: extraction logic is pure (chunks in → taps out) and gets table-driven unit tests;
  DB tests use `artracker_test`, never the dev DB.

## Implementation notes

Where the text above is outdated, the shipped behavior is:

- The sensitive-screen rule is word-based (CamelCase/digit/separator split; `pin`, `otp`, `cvv` also match a
  plural), not a substring regexp.
- `GET /heatmap/screens` also returns `app_versions`, the versions seen in the data.
- The worker backs failing sessions off (`taps_attempts`, `taps_retry_at`; 1 minute doubling, capped at 6 hours)
  and marks a session processed after 8 failed attempts. Only missing objects are skipped; other storage errors
  fail the session. Web projects are skipped, newest sessions are processed first, and a tick drains batches
  within a 45 s budget.
- Exclusions are re-read inside the write transaction (`FOR SHARE` on the project row) so a concurrent settings
  change cannot be overtaken.
- Reprocessing a project is documented in the README (reset `taps_processed_at`).
