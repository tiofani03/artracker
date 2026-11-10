# Mobile Replay Screenshot Mode — Web (Frontend + Backend) Design

Date: 2026-11-10 · Repo: `artracker` (`frontend/`, `backend/`)
Sibling designs: `2026-11-10-replay-screenshot-android-design.md`, `2026-11-10-replay-screenshot-ios-design.md`
This document is the **canonical owner of the shared contract** (§3, §4). The SDK designs copy it verbatim.

## 1. Intent

People watching a mobile replay in the dashboard must see exactly what the user saw on the device: real
fonts, icons, photos, colors, styling, and movement. Both mobile SDKs gain a `SCREENSHOT` mode that sends real
screen images (OpenReplay's approach). The web side must play these, keep playing old sessions, and give project
owners a server-side switch to turn screenshot capture off.

Background (verified on session `01a1c339-3fb0-77ba-9e68-2feabafd6e96`, project 5): Android's current rrweb
replays go blank after the first DOM mutation because the SDK nests subtrees inside one `adds` entry and rrweb's
replayer builds added nodes with `skipChild: true` (`rrweb.js:17387`). Flattening `adds` offline fixed it with
zero replayer errors.

## 2. Goals / non-goals / anti-goals

Goals
- W1. Play `artracker-screen-v1` sessions frame-exact (§7 acceptance).
- W2. Old sessions keep playing: Android rrweb (now fixed by flattening nested `adds` on load) and iOS
  `artracker-mobile-v1` wireframes.
- W3. Per-project kill switch for screenshot capture, enforced by the server.
- W4. Long sessions (30 min+) play without loading the whole session into memory.
- W5. Replay upload traffic cannot starve telemetry ingest, and the reverse.

Non-goals: retention/storage-cost policy; web SDK recording changes; video playback.

Anti-goals: a screenshot chunk accepted for a project whose switch is off; the player showing a frame at the wrong
time (a later frame before its `ts`, or a stale frame after a newer one is due).

## 3. Shared contract: chunk format `artracker-screen-v1`

One chunk = one gzipped UTF-8 JSON object, uploaded through `chunk-url` → `PUT` → `chunk-ack`
(object key `projects/{p}/sessions/{s}/chunk_{seq}.gz`, `Content-Type: application/gzip`).

```jsonc
{
  "format": "artracker-screen-v1",
  "platform": "android",            // "android" | "ios"
  "seq": 3,
  "frames": [
    {
      "ts": 1794276708006,          // epoch ms, device clock
      "screen": "lab/{id}",         // sanitized screen name
      "w": 720, "h": 1600,          // image size in px
      "vw": 360, "vh": 800,         // logical screen size (Android dp / iOS pt) the image covers
      "mime": "image/jpeg",
      "data": "<base64>",           // a complete frame, never a diff
      "masked": 2                   // number of masked regions painted into this frame
    }
  ],
  "touches": [
    { "ts": 1794276721185, "x": 185, "y": 373, "kind": "down" }   // kind: down | move | up; x/y in vw/vh units
  ]
}
```

Rules: frames sorted by `ts` inside a chunk; a chunk's gzipped size ≤ 2 MB; `chunk-ack.event_count` =
`frames.length + touches.length`; `has_rage_click` set by the SDK as today.

## 4. Shared contract: replay capture switch

- New column `projects.replay_screenshot_enabled BOOLEAN NOT NULL DEFAULT true` (migration `00xx_replay_screenshot_switch.sql`).
  Default `true` because the SDK side is already opt-in: nothing is captured unless the app enables `SCREENSHOT`.
- `GET /api/v1/replay/config` (ingest key auth, replay rate bucket) →
  `200 {"screenshot_enabled": true, "max_fps": 10}`. SDKs call it when replay starts and cache the answer.
- `POST /api/v1/replay/chunk-url` gains an optional request field `"format"`. When it is `"artracker-screen-v1"`
  and the project's switch is off → `403 {"error":"screenshot_disabled"}`. SDKs then stop screenshot capture,
  drop queued screenshot chunks of that project, fall back to `WIREFRAME` for the rest of the session and cache
  `screenshot_enabled=false`. Old SDKs that omit `format` are unaffected.

## 5. Backend design

1. **Migration + project API**: add the column (§4). `GET /api/v1/projects/:projectID` returns
   `replay_screenshot_enabled`; new `PATCH /api/v1/projects/:projectID/replay-settings`
   (`RequireProjectRole(admin)`) body `{"screenshot_enabled": bool}`.
2. **Replay config endpoint** `GET /api/v1/replay/config` (§4). Unknown/disabled storage still answers (the SDK
   needs the switch even when uploads will fail).
3. **chunk-url enforcement** (§4). Reads the switch with the project lookup it already does (no extra round trip
   per request beyond one indexed read; cache per project for 30 s in-process).
4. **Separate replay rate bucket**: today `chunk-url`/`chunk-ack` share the per-project ingest bucket
   (`analytics/ratelimit.go`, 50 req/s, burst 100). Add a second per-project bucket used only by
   `/replay/*` ingest routes, configured by `REPLAY_RATE_RPS` (default 100, burst 2×). 429 keeps sending
   `Retry-After` (both SDKs already honour it).
5. **Signed download URL refresh**: `GET /projects/:projectID/sessions/:sessionID` already returns signed chunk
   URLs. Add `GET /projects/:projectID/sessions/:sessionID/chunks?from_seq=&to_seq=` returning fresh signed URLs
   for a seq range so the player can load lazily after earlier URLs expired.
6. **Docs**: `openapi.go` and `frontend/src/pages/DocsPage.tsx` document §3, §4, the new routes and SDK options.

No change to storage, object keys, chunk upload/download proxies, or the replay tables.

## 6. Frontend design

1. **Model** (`replay/model.ts`): new kind
   `{ kind:'screen'; platform:'ios'|'android'; frames: ScreenFrameRef[]; touches; startMs; endMs }` where a
   `ScreenFrameRef` is `{ ts, screen, w, h, vw, vh, mime, chunkSeq, index }` — metadata only, no image bytes.
2. **Normalization** (`replay/normalize.ts`):
   - Detect `artracker-screen-v1` payloads → `screen` model. Precedence: `screen` > `web` > `mobile`.
   - `normalizeWebEvents` flattens nested `adds` (each node becomes its own entry, pre-order, parent before
     child, `nextId` kept only on the subtree root). Safe for web rrweb data, whose recorder never nests.
3. **Progressive loading** (`utils/replayChunkLoader.ts`): for `screen` sessions the loader no longer downloads
   every chunk up front. It
   - loads the first chunk, then keeps chunks covering [playhead − 10 s, playhead + 30 s] resident;
   - builds the timeline index (frame `ts` list) incrementally — first from chunk-ack metadata
     (`chunk_seq`, start/end from session detail), then exact as chunks arrive;
   - on a 401/403 from a chunk URL, requests fresh URLs via §5.5 and retries once;
   - releases decoded images and raw base64 outside the window.
   rrweb and wireframe sessions keep today's eager loading.
4. **Renderer** `components/ScreenReplayRenderer.tsx`:
   - Draws the frame with the greatest `ts ≤ playhead` into a `<canvas>` sized to the stage, aspect ratio kept,
     using `createImageBitmap(blob, { colorSpaceConversion: 'none', premultiplyAlpha: 'none' })`.
   - Decode-ahead of frames within the next 2 s so a swap never flashes; if the due frame is not decoded yet,
     keep the previous one and show a small "buffering" badge (never skip ahead).
   - Touch overlay maps `x,y` from `vw/vh` space to canvas space; `move` points interpolated between samples.
   - Same rAF playback clock, scrubber, speed and markers as the wireframe path; device chrome by `platform`.
5. **ReplayPlayer** picks the renderer by model kind. A badge shows `Screenshot` / `Wireframe` / `DOM` mode.
6. **Project settings UI**: toggle "Screenshot replay" (admins only) calling §5.1, with a one-line explanation
   that capture also requires enabling `SCREENSHOT` in the app's SDK config.

## 7. Acceptance criteria (web)

1. Automated (Vitest + Playwright, Chromium pinned by the repo's Playwright version): for a fixture
   `artracker-screen-v1` session, at each sampled playhead T the canvas equals the decoded JPEG of the frame with
   greatest `ts ≤ T` within a per-channel tolerance of 2/255 (decoder differences), and never shows a frame with
   `ts > T`.
2. Session `01a1c339-…` (Android rrweb) plays end to end with zero `Node … not found` warnings; an iOS
   `artracker-mobile-v1` fixture still renders as wireframe.
3. A 30-minute synthetic screen session (2 fps, ~80 KB frames) plays and seeks with resident memory for replay
   data under 150 MB in Chromium.
4. With the switch off, `chunk-url` with `format:"artracker-screen-v1"` returns 403 `screenshot_disabled`; without
   `format` it still returns 200.
5. Exhausting the replay bucket does not cause 429s on `/api/v1/ingest` for the same project, and vice versa.

## 8. Calls made by Claude (check these)

- Switch default `true` in the DB (opt-in already happens in the SDK).
- Kill switch delivered by a config endpoint plus server-side enforcement at `chunk-url`.
- Progressive loading window −10 s / +30 s; memory target 150 MB.
- Pixel tolerance 2/255 in acceptance #1 (exact equality is not achievable across JPEG decoders).
- Chunk gzipped size cap 2 MB (also keeps Android's SQLite-free queue small; see Android design).

## 9. Left to the builder

Component/file names beyond those listed, cache implementation for the switch, UI copy.
