# Replay Visual Rendering (Web + Mobile) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The session replay player shows the recorded website (rrweb DOM replay) or the recorded mobile screens (view-tree wireframe). It falls back to the telemetry canvas only when no visual data exists, and then it says why.

**Architecture:** Raw chunk payloads and telemetry are normalized into one `ReplayModel`: `web` | `mobile` | `none`. Renderer choice is driven by that model, not by string-sniffing `detectSessionPlatform`. Mobile view trees come mainly from replay chunks (no 32 KB attribute cap), with `telemetry.attributes.view_tree` as a secondary source. The backend sanitizer keeps the small attributes when it truncates oversized ones. A local-filesystem `ReplayStore` lets the whole chain run without R2.

**Tech Stack:** React 19 + TypeScript + Vite 8, rrweb 2 / rrweb-player 2, Vitest (new dev dependency), Go 1.25 + Fiber.

**Spec:** No separate spec document. This plan argues from the root-cause investigation on 2026-11-09:
1. `ReplayPlayer.tsx:473-492`: a renderer is chosen only if `hasFullSnapshot` (web) or a telemetry event carries `attributes.view_tree` (mobile). Otherwise the telemetry canvas is shown.
2. `backend/internal/ingest/sanitize.go:141`: any `attributes` > `MaxAttributeBytes` (32 KB) is replaced with `{"_truncated":true,...}`. Real UIKit/Android view trees exceed that, so `view_tree` (and `screen`) are erased at ingest.
3. Chunk contents are only interpreted as rrweb. Mobile chunk payloads are parsed and silently discarded (`extractEvents` → `[]`).
4. The rrweb player is torn down and rebuilt on every `stageWidth` / `durationMs` change (`ReplayPlayer.tsx:355`). Playback position is lost on resize.
5. Without R2, `GetProjectSession` returns chunks with empty `url` (`replay.go:625`), so nothing can be fetched in dev.

## Global Constraints

- Do not touch R2 configuration or `docker-compose.prod.yml` (out of scope per user).
- Existing chunk format stays valid: a gzipped JSON rrweb array, or `{ "events": [...] }`.
- New mobile chunk format (contract for the iOS/Android SDKs): gzipped JSON `{ "format": "artracker-mobile-v1", "platform": "ios" | "android", "frames": MobileFrame[], "touches": TouchPoint[] }`, with timestamps in epoch milliseconds.
- `MaxAttributeBytes` stays 32 KB. Truncation must keep top-level keys whose encoded value is ≤ 1 KB.
- Frontend tests: Vitest, files colocated as `*.test.ts`, run with `npm test` in `frontend/`.
- Backend tests: `cd backend && go test -p 1 ./...` (as in CI).
- No new runtime frontend dependencies. Vitest (and jsdom only if a test needs a DOM) as devDependencies.

## Review Focus

1. **A web session whose first chunk failed to upload**, so events start mid-session with no Meta/FullSnapshot at the head: the player should start at the first FullSnapshot, not show the telemetry canvas when a later snapshot exists. Pinned in Task 1.
2. **A mobile session where chunks and telemetry both carry frames for the same screen**: show one frame per timestamp, no flicker or duplicates. Pinned in Task 2.
3. **A chunk that is valid gzip but not JSON, or an unknown `format`**: skip that chunk and still render the others. Pinned in Task 3.
4. **Resizing the browser mid-playback**: the rrweb player keeps its position and play state. Pinned in Task 4 (manual check in Task 7).
5. **A telemetry event whose `attributes` exceed 32 KB**: `screen`/`platform` survive so the wireframe banner and detection still work. Pinned in Task 5.

---

## File Structure

| File | Responsibility |
|---|---|
| `frontend/src/replay/model.ts` (create) | `ReplayModel`, `MobileFrame`, `TouchPoint` types |
| `frontend/src/replay/normalize.ts` (create) | Pure: chunk payloads + telemetry → `ReplayModel` |
| `frontend/src/replay/normalize.test.ts` (create) | Unit tests for normalization |
| `frontend/src/utils/replayChunkLoader.ts` / `replayChunkWorker.ts` (modify) | Return raw parsed payloads per chunk, skip bad chunks |
| `frontend/src/replay/parseChunk.ts` (+ `.test.ts`) (create) | Shared gunzip + JSON parse used by worker and main thread |
| `frontend/src/components/ReplayPlayer.tsx` (modify) | Consume `ReplayModel`; stable rrweb instance; explicit fallback reason |
| `frontend/src/components/MobileWireframeRenderer.tsx` (modify) | Take `frames: MobileFrame[]` + `touches: TouchPoint[]` instead of telemetry |
| `backend/internal/ingest/sanitize.go` (+ test) (modify) | Partial truncation that keeps small keys |
| `backend/internal/storage/local.go` (+ test) (create) | Filesystem `ReplayStore` for dev without R2 |
| `backend/cmd/api/main.go`, `internal/config/config.go` (modify) | `REPLAY_LOCAL_DIR` wiring |
| `frontend/src/pages/DocsPage.tsx` (modify) | Document the mobile chunk contract |

---

### Task 1: ReplayModel + web normalization (with Vitest setup)

**Files:**
- Modify: `frontend/package.json` (add `"test": "vitest run"`, devDependency `vitest` at a version whose peer range includes the installed `vite`; check with `npm view vitest peerDependencies`)
- Create: `frontend/src/replay/model.ts`, `frontend/src/replay/normalize.ts`, `frontend/src/replay/normalize.test.ts`

**Interfaces:**
- Produces:
  ```ts
  // model.ts
  export type MobileFrame = { ts: number; screen: string; viewTree: ViewNode; presentation?: string; maskedCount?: number }
  export type TouchPoint = { ts: number; x: number; y: number; name?: string }
  export type ReplayModel =
    | { kind: 'web'; events: eventWithTime[]; startMs: number; endMs: number }
    | { kind: 'mobile'; platform: 'ios' | 'android'; frames: MobileFrame[]; touches: TouchPoint[]; startMs: number; endMs: number }
    | { kind: 'none'; reason: 'no-chunks' | 'no-visual-data' | 'load-error'; detail?: string }
  // normalize.ts
  export function normalizeWebEvents(raw: eventWithTime[]): eventWithTime[] | null
  export function buildReplayModel(input: { payloads: unknown[]; timeline: TelemetryEvent[]; projectPlatform?: Platform }): ReplayModel
  ```
  `ViewNode` moves to `model.ts` (re-exported from `MobileWireframeRenderer.tsx` for compatibility).

- [ ] **Step 1: Write failing tests** in `normalize.test.ts`:
  - `normalizeWebEvents` sorts by `timestamp`, removes exact duplicates (same `type`+`timestamp`+JSON of `data`), and drops every event before the first Meta (`type 4`) that is followed by a FullSnapshot (`type 2`). For input `[inc@100, meta@200, full@201, inc@300]` → `[meta@200, full@201, inc@300]`.
  - It returns `null` when no FullSnapshot exists, or when fewer than 2 events remain.
  - `buildReplayModel({ payloads: [[meta@1000, full@1001, inc@5000]], timeline: [] })` → `{ kind: 'web', startMs: 1000, endMs: 5000 }`.
  - `{ events: [...] }` payloads are accepted the same way as bare arrays.
  - `buildReplayModel({ payloads: [], timeline: [] })` → `{ kind: 'none', reason: 'no-chunks' }`.
  - `buildReplayModel({ payloads: [[inc@1]], timeline: [] })` → `{ kind: 'none', reason: 'no-visual-data' }`.
- [ ] **Step 2:** Run `cd frontend && npm test`. Expected: FAIL (module not found).
- [ ] **Step 3:** Implement `model.ts` and `normalize.ts` (web branch only; leave the mobile branch to Task 2, which returns `no-visual-data` for now).
- [ ] **Step 4:** Run `npm test`. Expected: PASS. Also run `npm run build`. Expected: exit 0.
- [ ] **Step 5:** Commit `feat(replay): add ReplayModel and web event normalization`.

### Task 2: Mobile frame normalization (chunks + telemetry)

**Files:**
- Modify: `frontend/src/replay/normalize.ts`, `frontend/src/replay/normalize.test.ts`

**Interfaces:**
- Consumes: types from Task 1.
- Produces: `export function extractMobileFrames(payloads: unknown[], timeline: TelemetryEvent[]): { frames: MobileFrame[]; touches: TouchPoint[]; platform: 'ios' | 'android' | null }`. `buildReplayModel` returns `kind: 'mobile'` when `frames.length > 0` and there is no web replay.

- [ ] **Step 1: Write failing tests:**
  - A payload `{format:'artracker-mobile-v1', platform:'android', frames:[{ts:1000, screen:'Home', view_tree:{...}}], touches:[{ts:1500,x:10,y:20}]}` → one frame (`viewTree` mapped from `view_tree`), one touch, `platform:'android'`.
  - Telemetry `{ name:'screen.view', event_timestamp:'1970-01-01T00:00:02.000Z', attributes:{ screen:'Detail', view_tree:{...}, platform:'ios' } }` → a frame with `ts:2000`. Telemetry with `attributes.x/y` and a name containing `tap`/`touch`/`click` → a touch.
  - A chunk frame and a telemetry frame with the same `ts` and `screen` → one frame (the chunk wins).
  - Frames come back sorted by `ts`. A frame without `view_tree` is ignored.
  - Platform precedence: chunk `platform` > telemetry `attributes.platform` > `projectPlatform` > `'ios'`.
  - `buildReplayModel` with a web replay AND mobile frames → `kind: 'web'`. With mobile frames only → `kind: 'mobile'`, `startMs` = first frame ts, `endMs` = max(last frame ts, last touch ts).
- [ ] **Step 2:** `npm test`. Expected: FAIL.
- [ ] **Step 3:** Implement `extractMobileFrames` and wire it into `buildReplayModel`. Unknown `format` values are ignored.
- [ ] **Step 4:** `npm test`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(replay): extract mobile view-tree frames from chunks and telemetry`.

### Task 3: Loader returns raw payloads and tolerates bad chunks

**Files:**
- Create: `frontend/src/replay/parseChunk.ts`, `frontend/src/replay/parseChunk.test.ts`
- Modify: `frontend/src/utils/replayChunkWorker.ts`, `frontend/src/utils/replayChunkLoader.ts`

**Interfaces:**
- Produces:
  - `export async function parseChunkBytes(buf: ArrayBuffer): Promise<unknown | null>`: gunzip if the gzip magic is present, then `JSON.parse`. Returns `null` on any failure.
  - `export async function loadChunkPayloads(chunks: ReplayChunkDetail[], signal?: AbortSignal): Promise<{ payloads: unknown[]; failed: number }>`, replacing `loadSessionChunks`. Payloads are ordered by `chunk_seq`; null results are counted in `failed`, not thrown.
- The worker message becomes `{ id, ok: true, payloads: unknown[], failed: number }`. Delete the duplicated decompress/extract code from both files and use `parseChunkBytes` in both.

- [ ] **Step 1: Write failing tests** (`parseChunk.test.ts`, Node env, using `CompressionStream` from Node ≥ 18):
  - Gzipped `[{"type":4}]` → `[{type:4}]`.
  - Plain (un-gzipped) JSON bytes → parsed.
  - Gzipped `not json` → `null`.
  - Empty buffer → `null`.
- [ ] **Step 2:** `npm test`. Expected: FAIL.
- [ ] **Step 3:** Implement `parseChunkBytes`. Refactor the worker and loader to it. HTTP non-OK for one chunk counts as `failed` rather than rejecting the whole load. Only reject when every chunk fails.
- [ ] **Step 4:** `npm test && npm run build`. Expected: PASS, exit 0.
- [ ] **Step 5:** Commit `refactor(replay): load raw chunk payloads and skip unreadable chunks`.

### Task 4: ReplayPlayer renders from ReplayModel

**Files:**
- Modify: `frontend/src/components/ReplayPlayer.tsx`, `frontend/src/components/MobileWireframeRenderer.tsx`

**Interfaces:**
- Consumes: `loadChunkPayloads` (Task 3), `buildReplayModel` (Tasks 1-2).
- `MobileWireframeRenderer` props become `{ frames: MobileFrame[]; touches: TouchPoint[]; currentTs: number; platform: 'ios' | 'android'; containerWidth?: number }`, where `currentTs` is an absolute epoch ms value. The active frame is the last one with `ts ≤ currentTs`, else the first. The touch ripple shows within ±600 ms.

Behavior to implement:
- `model` = `useMemo(() => buildReplayModel({ payloads, timeline: timelineEvents, projectPlatform }))`. `overrideEvents` (live capture) is passed as `payloads: [overrideEvents]`.
- The frame/chrome platform in Auto mode is `model.kind === 'mobile' ? model.platform : model.kind === 'web' ? 'web' : detection.platform`. The manual Web/iOS/Android toggle still overrides chrome only, never which renderer is used.
- rrweb: create the player in an effect keyed only on `model` (web events identity). On width changes call `instance.$set({ width, height })` followed by `instance.triggerResize()` instead of re-creating it. Remove `stageWidth` and `durationMs` from the creation effect's deps.
- Clock: for web, read `replayer.getCurrentTime()`. For mobile, advance `currentTimeMs` with `requestAnimationFrame` × `playbackSpeed`. `durationMs` = `model.endMs - model.startMs` (min 1000), falling back to `session.duration_ms` for `none`.
- `kind: 'none'`: keep the telemetry canvas, but add a one-line notice in its top bar:
  - `no-chunks` → "No replay chunks were uploaded for this session."
  - `no-visual-data` → "Chunks contain no DOM snapshot or view hierarchy."
  - `load-error` → `detail`

  If `failed > 0` while a model was still built, show a small "N chunk(s) could not be read" chip in the HUD.

- [ ] **Step 1:** Implement the changes above. Delete `hasFullSnapshot`, `mobileScreenEvents`, `hasMobileWireframe` and `fetchedEvents` (replaced by `payloads` state + `model`).
- [ ] **Step 2:** `cd frontend && npm test && npm run lint && npm run build`. Expected: all pass, exit 0.
- [ ] **Step 3:** Commit `fix(replay): choose renderer from normalized replay model and keep rrweb instance across resizes`.

### Task 5: Sanitizer keeps small attributes when truncating

**Files:**
- Modify: `backend/internal/ingest/sanitize.go:136-157`
- Test: `backend/internal/ingest/sanitize_test.go` (create)

**Interfaces:**
- `func (s *Sanitizer) sanitizeAttributes(raw json.RawMessage) ([]byte, bool)` keeps its signature. When `len(raw) > MaxAttributeBytes` and `raw` is a JSON object, the output keeps every top-level key whose raw value is ≤ `MaxKeptAttributeValueBytes` (new const = 1024) and adds `"_truncated": true`, `"_original_bytes": N`, `"_dropped_keys": [...]` (sorted). Non-objects keep today's behavior.

- [ ] **Step 1: Write failing test** `TestSanitizeAttributesKeepsSmallKeysWhenOversized`: input `{"screen":"Home","platform":"ios","view_tree":<40 KB string>}` → output has `screen=="Home"`, `platform=="ios"`, `_truncated==true`, `_dropped_keys==["view_tree"]`, and no `view_tree`; the boolean result is `true`. Plus `TestSanitizeAttributesUnderLimitUnchanged`: a 1 KB object is returned byte-identical with `false`.
- [ ] **Step 2:** `cd backend && go test ./internal/ingest/ -run SanitizeAttributes -v`. Expected: FAIL.
- [ ] **Step 3:** Implement it (decode into `map[string]json.RawMessage` and re-marshal the kept keys).
- [ ] **Step 4:** `go test -p 1 ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `fix(ingest): keep small attributes when truncating oversized payloads`.

### Task 6: Local filesystem ReplayStore (dev without R2)

**Files:**
- Create: `backend/internal/storage/local.go`, `backend/internal/storage/local_test.go`
- Modify: `backend/internal/config/config.go` (add `ReplayLocalDir string` from `REPLAY_LOCAL_DIR`, default `""`), `backend/cmd/api/main.go:118-136`

**Interfaces:**
- Produces: `func NewLocal(dir string) (*LocalStore, error)` implementing `ReplayStore`. `PutObject`/`GetObject` read and write `filepath.Join(dir, objectKey)` and reject keys containing `..` or starting with `/`. `GeneratePresignedPut`/`GeneratePresignedGet` return an error (`ErrPresignUnsupported`). `GetObject` content type is `application/gzip`. `Bucket()` returns `"local"`.
- Wiring: if `R2_BUCKET_NAME` is empty and `REPLAY_LOCAL_DIR` is set, use `NewLocal`. When `replayStore` is a `*LocalStore`, force the proxy upload/download path (`ReplayProxyUpload = true`) so URLs point at `/api/v1/replay/{upload,download}-chunk`.

- [ ] **Step 1: Write failing tests:** a put-then-get round trip returns identical bytes; key `../x` → error; get of a missing key → error.
- [ ] **Step 2:** `go test ./internal/storage/ -run Local -v`. Expected: FAIL.
- [ ] **Step 3:** Implement and wire it up.
- [ ] **Step 4:** `go test -p 1 ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(storage): add local filesystem replay store for development`.

### Task 7: Contract docs + end-to-end verification

**Files:**
- Modify: `frontend/src/pages/DocsPage.tsx` (Session Replay section: document the `artracker-mobile-v1` chunk shape and the 32 KB attribute cap with its keep-small-keys behavior)
- Modify: `.env.example` (add `REPLAY_LOCAL_DIR=` with a comment)

- [ ] **Step 1:** Update the docs and `.env.example`.
- [ ] **Step 2:** Run with `REPLAY_LOCAL_DIR=./.replay-data` and the API + web dev servers. Then:
  - Upload a gzipped rrweb fixture (record one with the dashboard's live capture) through `POST /api/v1/replay/chunk-url` → `PUT upload_url` → `POST /api/v1/replay/chunk-ack`.
  - Do the same with an `artracker-mobile-v1` fixture of 3 frames using the seeder's `tree1..tree3` from `handler/telemetry.go`.
- [ ] **Step 3:** In the dashboard, open each session. Expected:
  - Web session: the DOM replay plays.
  - Mobile session: the wireframe switches screens at the right times.
  - Resizing the window mid-playback keeps the position (Review Focus #4).
  - A session with no chunks shows the telemetry canvas with the "No replay chunks…" notice.

  Take a screenshot of each state.
- [ ] **Step 4:** Commit `docs(replay): document mobile chunk contract and local replay storage`.

---

## Out of scope / dependencies

- **iOS/Android SDKs** (not in this repo) must start uploading `artracker-mobile-v1` chunks for real apps to get full-fidelity mobile replay. Until then, mobile replay works only for telemetry `view_tree` ≤ 32 KB.
- R2 / production compose configuration (deferred by the user).
