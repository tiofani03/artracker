# Replay Screenshot Mode — Web (Frontend + Backend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The dashboard plays `artracker-screen-v1` mobile replays frame-exact, project owners can switch screenshot capture off server-side, and already-recorded Android rrweb sessions stop going blank.

**Architecture:** Backend stays a dumb chunk store; it gains a per-project switch (`projects.replay_screenshot_enabled`), a replay-only rate bucket, a config endpoint and a signed-URL refresh endpoint. The frontend adds a pure screen-replay core (`replay/screen*.ts`: parse, index, window cache, session orchestrator) behind a thin canvas renderer, wired into `ReplayPlayer` by chunk format. Screen sessions load progressively (scan pass builds a metadata index; image data is held only for a window around the playhead).

**Tech Stack:** Go 1.25 + Fiber + pgx (backend); React 19 + TypeScript + Vite + Vitest (frontend); Playwright (new devDependency, e2e only).

**Spec:** `docs/superpowers/specs/2026-11-10-replay-screenshot-web-design.md` (canonical contract). Siblings `…-android-design.md` / `…-ios-design.md` copy §3–§4 and are out of scope here: **this plan touches only `artracker` (`frontend/`, `backend/`, `docs/`), never the SDK repos.**

## Global Constraints

- Chunk format `artracker-screen-v1`, one gzipped JSON object: `{ format, platform:"android"|"ios", seq, frames:[{ts,screen,w,h,vw,vh,mime:"image/jpeg",data:<base64>,masked}], touches:[{ts,x,y,kind:"down"|"move"|"up"}] }`; `ts` epoch ms; touch `x,y` in `vw/vh` units; frames sorted by `ts` within a chunk; chunk ≤ 2 MB gzipped; `chunk-ack.event_count = frames.length + touches.length`.
- Column `projects.replay_screenshot_enabled BOOLEAN NOT NULL DEFAULT true`.
- `GET /api/v1/replay/config` (ingest key auth, replay rate bucket) → `200 {"screenshot_enabled": true, "max_fps": 10}`.
- `POST /api/v1/replay/chunk-url` optional field `"format"`; `"artracker-screen-v1"` with the switch off → `403 {"error":"screenshot_disabled"}`; requests without `format` are unaffected.
- `PATCH /api/v1/projects/:projectID/replay-settings` body `{"screenshot_enabled": bool}`, admin only. `GET /api/v1/projects/:projectID/sessions/:sessionID/chunks?from_seq=&to_seq=` returns fresh signed URLs.
- Replay routes get their own per-project bucket: env `REPLAY_RATE_RPS`, default 100, burst 2×; 429 keeps `Retry-After`.
- Player: resident window `[playhead − 10 s, playhead + 30 s]`; decode-ahead 2 s; `createImageBitmap(blob, { colorSpaceConversion: 'none', premultiplyAlpha: 'none' })`; pixel tolerance 2/255; replay-data memory < 150 MB for a 30-minute 2 fps session; never draw a frame with `ts > playhead`.
- Precedence: `screen` > `web` (rrweb) > `mobile` (wireframe). Old sessions must keep playing.
- Frontend rules: no new runtime dependencies; Vitest tests colocated `*.test.ts` (node env, no jsdom); keep `npx tsc -b` clean. Backend tests: `cd backend && go test -p 1 ./...`.
- **Safety:** `.env`'s `DATABASE_URL` points at a remote production database under a read-only mandate. Never run the API (it migrates at boot) or any test against it. DB-backed Go tests use the local dev Postgres only: `make db-up`, then `export TEST_DATABASE_URL='postgres://artracker:artracker@localhost:5433/artracker?sslmode=disable'`. Without it those tests skip.
- Commits stay local on `feat/replay-screenshot-web` (no push). End every commit message with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Mixed-format session** (Android starts in wireframe, switches to screenshot; or the reverse after a 403): the player must pick screen when the first *or* last chunk is screen, and ignore the non-screen chunks. → Task 7.
2. **Seek into a chunk that is not scanned yet** (or a gap between scanned chunks): show the nearest earlier decoded frame plus a "buffering" badge, never a stale frame presented as current and never a blank canvas. → Tasks 5, 6, 8.
3. **Signed chunk URL expires mid-playback** (403/401): refresh once via the range endpoint and retry; if refresh fails, keep buffering and retry later, never throw into React. → Task 7.
4. **Bad frames** (invalid base64, wrong mime, zero size, duplicate `ts`, out-of-order frames across chunks): skipped or deduplicated; the rest of the chunk still plays. → Tasks 4, 5.
5. **Switch turned off while someone watches** an existing screenshot session: playback is unaffected (the switch gates uploads only); and old SDKs that send no `format` still upload. → Task 3.

---

### Task 0: Commit the design docs and this plan

**Files:** `docs/superpowers/specs/2026-11-10-replay-screenshot-{web,android,ios}-design.md`, this plan.

- [ ] **Step 1:** `git add docs/superpowers/specs docs/superpowers/plans/2026-11-10-replay-screenshot-web.md && git commit -m "docs(replay): add screenshot-mode designs and web implementation plan"` (with the attribution line).

---

## Backend

### Task 1: Project switch column + settings API

**Files:**
- Create: `backend/internal/db/migrations/0009_replay_screenshot_switch.sql`, `backend/internal/handler/replay_settings.go`, `backend/internal/handler/replay_settings_test.go`
- Modify: `backend/internal/handler/project.go` (`Project`, `projectColumns`, `scanProject`), `backend/internal/handler/routes.go`

**Interfaces:**
- Produces: `Project.ReplayScreenshotEnabled bool` (`json:"replay_screenshot_enabled"`); `func (h *Handler) UpdateReplaySettings(c *fiber.Ctx) error`; route `PATCH /projects/:projectID/replay-settings` guarded by `h.RequireAuth, h.RequireProjectRole(auth.RoleAdmin)`; after a successful update it calls `h.replaySwitch.invalidate(projectID)` (defined in Task 2 — add the call there, not here).

- [ ] **Step 1: Write failing tests** in `replay_settings_test.go` (use the `newTestEnv` helpers from `org_test.go`/`replay_test.go`; they skip without `TEST_DATABASE_URL`):
  - `TestReplaySettingsDefaultOn`: a new project's `GET /api/v1/projects/:id` has `replay_screenshot_enabled == true`.
  - `TestReplaySettingsPatchAdmin`: admin `PATCH` `{"screenshot_enabled":false}` → 200, body `replay_screenshot_enabled == false`; a following GET shows `false`.
  - `TestReplaySettingsViewerForbidden`: a `viewer` member gets 403/404 per `RequireProjectRole` and the value is unchanged.
  - `TestReplaySettingsValidation`: empty body `{}` → 400.
- [ ] **Step 2:** Run `cd backend && go test -p 1 ./internal/handler -run TestReplaySettings -v` (with `TEST_DATABASE_URL`). Expected: FAIL (route missing).
- [ ] **Step 3:** Migration: `ALTER TABLE projects ADD COLUMN IF NOT EXISTS replay_screenshot_enabled BOOLEAN NOT NULL DEFAULT true;` (same style as `0006`). Add the field to `Project`, append `, replay_screenshot_enabled` to `projectColumns`, and to the `Scan` in `scanProject`. Implement `UpdateReplaySettings` (`*bool` request field → 400 `"screenshot_enabled is required"` when nil; `UPDATE projects SET replay_screenshot_enabled=$2 WHERE id=$1 RETURNING `+projectColumns). Register the route next to the other `/projects/:projectID/...` routes.
- [ ] **Step 4:** Re-run the command from Step 2. Expected: PASS. Then `go test -p 1 ./...` — expected: no regressions.
- [ ] **Step 5:** Commit `feat(replay): per-project screenshot switch column and settings endpoint`.

### Task 2: Replay config endpoint + `chunk-url` enforcement

**Files:**
- Create: `backend/internal/handler/replay_switch.go`, `backend/internal/handler/replay_switch_test.go`
- Modify: `backend/internal/handler/health.go` (add field `replaySwitch replaySwitchCache` to `Handler`), `backend/internal/handler/replay.go` (`ReplayChunkURLRequest.Format`, enforcement), `backend/internal/handler/routes.go`, `backend/internal/handler/replay_settings.go` (cache invalidation), `backend/internal/handler/replay_test.go` (extract a helper)

**Interfaces:**
- Produces:
  - `const ScreenChunkFormat = "artracker-screen-v1"`, `const replayMaxFPS = 10`
  - `type replaySwitchCache struct` (zero value usable; 30 s TTL) with `get(projectID int64) (enabled, ok bool)`, `put(projectID int64, enabled bool)`, `invalidate(projectID int64)`
  - `func (h *Handler) replayScreenshotEnabled(ctx context.Context, projectID int64) (bool, error)` — `true` when `h.db == nil`; cache first; else `SELECT replay_screenshot_enabled FROM projects WHERE id=$1` (a DB error is returned, never treated as enabled)
  - `type ReplayConfigResponse struct { ScreenshotEnabled bool \`json:"screenshot_enabled"\`; MaxFPS int \`json:"max_fps"\` }` and `func (h *Handler) GetReplayConfig(c *fiber.Ctx) error`
  - test helper `setupReplayTestHandler(t *testing.T, store storage.ReplayStore) (*fiber.App, *Handler, string)`; `setupReplayTestAppWithStore` becomes a wrapper (project 88, key already seeded there).

- [ ] **Step 1: Write failing tests** (`replay_switch_test.go`, no DB needed):
  - `TestReplayConfigDefault`: `GET /api/v1/replay/config` with the key → 200, JSON equals `{"screenshot_enabled":true,"max_fps":10}`; without key → 401.
  - `TestChunkURLScreenFormatBlockedWhenSwitchOff`: `h.replaySwitch.put(88,false)`; `POST /replay/chunk-url` with `"format":"artracker-screen-v1"` → 403 and body `{"error":"screenshot_disabled"}`.
  - `TestChunkURLWithoutFormatUnaffected`: same switch-off state, no `format` → 200 (Review Focus 5).
  - `TestChunkURLScreenFormatAllowedWhenSwitchOn`: switch on → 200.
  - `TestReplayConfigReflectsSwitch`: after `put(88,false)` the config answers `screenshot_enabled:false`.
- [ ] **Step 2:** Run `go test ./internal/handler -run 'TestReplayConfig|TestChunkURL' -v`. Expected: FAIL.
- [ ] **Step 3:** Implement per Interfaces. Enforcement sits in `GenerateChunkUploadURL` after `sessionIDRegex` validation and **before** the `replay_sessions` upsert, answering `c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "screenshot_disabled"})`. Register `api.Get("/replay/config", h.VerifyIngestKey, h.RateLimitIngest, h.GetReplayConfig)` beside the other replay routes (before the `h.db == nil` block). `UpdateReplaySettings` calls `h.replaySwitch.invalidate`.
- [ ] **Step 4:** Re-run Step 2 command, then the whole `go test -p 1 ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(replay): replay config endpoint and screenshot_disabled enforcement at chunk-url`.

### Task 3: Separate replay rate bucket

**Files:**
- Modify: `backend/internal/ingest/ratelimit.go`, `backend/internal/ingest/service.go` (`Config.ReplayRPS`), `backend/internal/handler/ingest.go` (middleware), `backend/internal/handler/routes.go`, `backend/internal/config/config.go`, `backend/cmd/api/main.go`, `.env.example`
- Test: `backend/internal/ingest/ratelimit_test.go` (create or extend), `backend/internal/config/config_test.go`

**Interfaces:**
- Produces: `func NewProjectRateLimiterWithReplay(rps, eps, replayRPS float64) *ProjectRateLimiter` (the existing `NewProjectRateLimiter(rps, eps)` delegates with `replayRPS = 100`); `func (prl *ProjectRateLimiter) AllowReplayRequest(projectID int64) (bool, int)`; `ingest.Config.ReplayRPS float64`; `config.Config.ReplayRateRPS float64` (env `REPLAY_RATE_RPS`, default `100.0`); `func (h *Handler) RateLimitReplay(c *fiber.Ctx) error` (same 429 + `Retry-After` behaviour as `RateLimitIngest`).

- [ ] **Step 1: Write failing tests:**
  - `TestReplayBucketIndependentOfIngest`: limiter with `rps=1, replayRPS=1` (burst 2 each): drain `AllowReplayRequest(7)` until false; `AllowRequest(7)` is still true; then drain `AllowRequest(7)`; `AllowReplayRequest(8)` (other project) still true.
  - `TestReplayBucketDefaults`: `NewProjectRateLimiter(50, 1000)` allows 200 consecutive `AllowReplayRequest(1)` (burst 2×100) and rejects the 201st with retry-after ≥ 1.
  - config test: unset env → `ReplayRateRPS == 100`; `REPLAY_RATE_RPS=250` → `250`.
- [ ] **Step 2:** Run `go test ./internal/ingest ./internal/config -v`. Expected: FAIL.
- [ ] **Step 3:** Add `replayBucket *TokenBucket` to `projectLimiters` (created together with the others), `defaultReplayRPS` on the limiter, wire `ReplayRPS` through `ingest.NewService`, config and `main.go`. In `routes.go` replace `h.RateLimitIngest` with `h.RateLimitReplay` on `/replay/chunk-url`, `/replay/chunk-ack` and `/replay/config`. Add `REPLAY_RATE_RPS=100` with a comment to `.env.example`.
- [ ] **Step 4:** Re-run Step 2, then `go test -p 1 ./...`. Expected: PASS (existing replay tests still pass: they seed `ProjectRPS: 100`).
- [ ] **Step 5:** Commit `feat(replay): dedicated per-project rate bucket for replay routes`.

### Task 4: Signed chunk URL refresh endpoint

**Files:**
- Modify: `backend/internal/handler/replay.go` (extract helper from `GetProjectSession`), `backend/internal/handler/routes.go`
- Test: `backend/internal/handler/replay_test.go` (DB test beside `TestReplayDatabaseIntegration`)

**Interfaces:**
- Produces: `func (h *Handler) chunkDownloadURL(c *fiber.Ctx, ctx context.Context, objectKey string) string` (the exact logic now inline in the chunk loop: presigned GET when `!h.cfg.ReplayProxyUpload`, else `signedChunkURL(c,"download-chunk",key,time.Hour)`); `type ReplayChunkListResponse struct { Chunks []ReplayChunkDetail \`json:"chunks"\` }`; `func (h *Handler) ListSessionChunks(c *fiber.Ctx) error`; route `GET /projects/:projectID/sessions/:sessionID/chunks` with `h.RequireAuth, h.RequireProjectRole(auth.RoleViewer)`.

- [ ] **Step 1: Write failing tests** (DB-backed, reuse the env from `TestReplayDatabaseIntegration`; seed 3 chunks through chunk-url/ack):
  - `TestListSessionChunksRange`: `?from_seq=1&to_seq=2` → exactly seq 1 and 2, each with a non-empty `url`.
  - `TestListSessionChunksDefaultsAndLimit`: no query → all chunks ascending; never more than 100 rows.
  - `TestListSessionChunksValidation`: `from_seq=-1`, `from_seq=abc`, `from_seq=5&to_seq=2` → 400; unknown session → empty list (200); another project's session id → empty list.
- [ ] **Step 2:** Run `go test -p 1 ./internal/handler -run TestListSessionChunks -v`. Expected: FAIL.
- [ ] **Step 3:** Extract `chunkDownloadURL` and make `GetProjectSession` use it (behaviour unchanged). Implement `ListSessionChunks`: validate `sessionIDRegex`, parse `from_seq` (default 0) and `to_seq` (default `math.MaxInt32`), `SELECT … FROM replay_chunks WHERE project_id=$1 AND session_id=$2 AND chunk_seq BETWEEN $3 AND $4 ORDER BY chunk_seq LIMIT 100`.
- [ ] **Step 4:** Re-run Step 2, then `go test -p 1 ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(replay): endpoint to refresh signed chunk URLs for a seq range`.

---

## Frontend

### Task 5: Flatten nested rrweb `adds` (fixes recorded Android sessions)

**Files:** Modify `frontend/src/replay/normalize.ts`; Test `frontend/src/replay/normalize.test.ts`.

**Interfaces:** Produces `export function flattenNestedAdds(events: eventWithTime[]): eventWithTime[]`; `normalizeWebEvents` applies it to its output (before returning the trimmed list).

- [ ] **Step 1: Write failing tests** in `normalize.test.ts`:
  - `flattenNestedAdds splits a nested add into pre-order flat adds`: input mutation (`type:3, data.source:0`) with `adds:[{parentId:16,nextId:48,node:{type:2,id:87,tagName:'div',attributes:{},childNodes:[{type:3,id:88,textContent:'x'},{type:2,id:89,tagName:'span',attributes:{},childNodes:[{type:3,id:90,textContent:'y'}]}]}}]` → output `adds` ids in order `87,88,89,90`; every element node has `childNodes: []`; entry 87 keeps `parentId:16,nextId:48`; 88 and 89 have `parentId:87,nextId:null`; 90 has `parentId:89,nextId:null`.
  - `returns the same event object when nothing is nested` (reference equality) and leaves FullSnapshot / non-mutation events untouched.
  - `normalizeWebEvents flattens nested adds in its result` (a FullSnapshot + a nested-add mutation → result has no add with non-empty `childNodes`).
  - Real-data guard: build a mutation shaped like session `01a1c339…` at 14 637 ms (5 adds incl. `parentId:105,node 120` with a text child) and assert text node `121` appears as its own add with `parentId:120`.
- [ ] **Step 2:** Run `cd frontend && npx vitest run src/replay/normalize.test.ts`. Expected: FAIL.
- [ ] **Step 3:** Implement. Text nodes (`type:3`) have no `childNodes` and are copied as is; only element/document nodes are stripped to `childNodes: []`. Do not touch `enhanceMobileSyntheticEvents`.
- [ ] **Step 4:** Re-run the file, then `npm test`. Expected: PASS (all prior 74 tests too).
- [ ] **Step 5:** Commit `fix(replay): flatten nested rrweb adds so recorded Android sessions render`.

### Task 6: Screen chunk parsing and playback helpers (`replay/screen.ts`)

**Files:** Create `frontend/src/replay/screen.ts`, `frontend/src/replay/screen.test.ts`.

**Interfaces — Produces** (all exported):
```ts
export const SCREEN_CHUNK_FORMAT = 'artracker-screen-v1'
export type ScreenFrameMeta = { ts: number; screen: string; w: number; h: number; vw: number; vh: number; mime: 'image/jpeg'; chunkSeq: number; index: number; masked: number }
export type ScreenTouch = { ts: number; x: number; y: number; kind: 'down' | 'move' | 'up' }
export type ParsedScreenChunk = { seq: number; platform: 'android' | 'ios' | null; frames: ScreenFrameMeta[]; touches: ScreenTouch[] }
export function isScreenPayload(v: unknown): boolean
export function isScreenSession(probes: unknown[]): boolean            // true if ANY probe is a screen payload
export function parseScreenChunk(payload: unknown, seq: number): ParsedScreenChunk | null
export function screenFrameData(payload: unknown, index: number): string | null
export function activeScreenFrameAt(frames: ScreenFrameMeta[], ts: number): ScreenFrameMeta | null
export function touchMarkerAt(touches: ScreenTouch[], ts: number, view: { vw: number; vh: number }, canvas: { width: number; height: number }): { x: number; y: number; kind: ScreenTouch['kind'] } | null
```
A frame is valid iff `ts` finite, `w,h,vw,vh` finite and > 0, `mime === 'image/jpeg'`, `data` a non-empty string. `index` is the frame's position in the original `frames` array. `parseScreenChunk` returns `null` for non-screen payloads and for chunks with no valid frame *and* no touch.

- [ ] **Step 1: Write failing tests** (`screen.test.ts`):
  - `parseScreenChunk maps valid frames and keeps original index` (3 frames, middle one invalid → indexes `[0,2]`, `chunkSeq` = argument).
  - `parseScreenChunk skips frames with wrong mime, zero size, NaN ts, empty data` (Review Focus 4) and returns `null` for `{format:'artracker-mobile-v1',…}`, arrays, `null`.
  - `screenFrameData returns the base64 string for a valid index and null otherwise`.
  - `isScreenSession is true for [rrwebArray, screenPayload] and [screenPayload, rrwebArray], false for [rrwebArray, mobilePayload]` (Review Focus 1).
  - `activeScreenFrameAt returns the greatest ts <= T, null before the first frame, last frame after the end`.
  - `touchMarkerAt scales vw/vh to canvas, interpolates a move between two samples at the midpoint ts, returns null outside TOUCH_WINDOW_MS (600)`.
- [ ] **Step 2:** `npx vitest run src/replay/screen.test.ts` → FAIL.
- [ ] **Step 3:** Implement; import `TOUCH_WINDOW_MS` from `./playback`.
- [ ] **Step 4:** Re-run → PASS; `npx tsc -b` clean.
- [ ] **Step 5:** Commit `feat(replay): parse artracker-screen-v1 chunks and playback helpers`.

### Task 7: Screen index, window cache, chunk source, session orchestrator

**Files:**
- Create: `frontend/src/replay/screenIndex.ts` (+ `.test.ts`), `frontend/src/replay/screenWindow.ts` (+ `.test.ts`), `frontend/src/utils/screenChunkSource.ts` (+ `.test.ts`), `frontend/src/replay/screenSession.ts` (+ `.test.ts`)
- Modify: `frontend/src/api.ts` (`replayApi.listSessionChunks`)

**Interfaces — Produces:**
```ts
// screenIndex.ts
export type ChunkRange = { seq: number; startMs: number; endMs: number }
export class ScreenIndex {
  constructor(allSeqs: number[])                 // every chunk_seq of the session
  addChunk(c: ParsedScreenChunk): void           // idempotent per seq; chunks may arrive in any order
  get frames(): ScreenFrameMeta[]                // sorted by ts; equal ts → keep the higher chunkSeq
  get touches(): ScreenTouch[]
  get startMs(): number; get endMs(): number     // 0 when empty
  get platform(): 'android' | 'ios' | null
  ranges(): ChunkRange[]
  isResolved(ts: number): boolean                // see rule below
}
// screenWindow.ts
export function chunkSeqsInWindow(ranges: ChunkRange[], playheadMs: number, behindMs?: number /*10_000*/, aheadMs?: number /*30_000*/): number[]
export class ChunkCache<T> { get(seq: number): T | undefined; set(seq: number, v: T): void; retainOnly(seqs: Iterable<number>): number[]; get size(): number }
// screenChunkSource.ts
export type RefreshUrls = (fromSeq: number, toSeq: number) => Promise<ReplayChunkDetail[]>
export class ScreenChunkSource {
  constructor(chunks: ReplayChunkDetail[], refresh: RefreshUrls, fetchImpl?: typeof fetch)
  fetchChunk(seq: number, signal?: AbortSignal): Promise<unknown | null>   // null = unreadable
}
// screenSession.ts
export class ScreenReplaySession {
  constructor(deps: { seqs: number[]; sessionStartMs: number; chunkCreatedAtMs: Map<number, number>; fetchChunk: (seq: number, signal?: AbortSignal) => Promise<unknown | null> })
  readonly index: ScreenIndex
  subscribe(fn: () => void): () => void
  start(signal?: AbortSignal): Promise<void>            // scans every chunk (concurrency 3)
  setPlayhead(absMs: number): void                      // retains window chunks, fetches missing ones, fetches the estimated chunk for an unscanned time
  frameImage(f: ScreenFrameMeta): string | null         // base64 if its chunk is resident; otherwise null and a fetch is requested
}
// api.ts
replayApi.listSessionChunks(projectId: number, sessionId: string, fromSeq: number, toSeq: number): Promise<{ chunks: ReplayChunkDetail[] }>
```
`isResolved(ts)` rule: true when `ts` lies inside a known chunk's `[startMs,endMs]`; or between two known chunks whose seqs are adjacent in `allSeqs`; or after the last known chunk when that chunk is the highest seq; false otherwise (a gap or an unscanned region). `ScreenChunkSource.fetchChunk` on 401/403: call `refresh(seq, seq)` once, replace that chunk's `url`, retry once; if the retry fails or refresh throws → `null` (never rejects except for `AbortError`). `chunkCreatedAtMs` is used to estimate which chunk to fetch first for a time with no known range: the first seq whose created-at ≥ the target time.

- [ ] **Step 1: Write failing tests**, one file per module:
  - `screenIndex.test.ts`: sorted merge of out-of-order chunks; duplicate `ts` keeps the higher `chunkSeq` (Review Focus 4); `addChunk` twice is idempotent; `isResolved` is false in a gap between non-adjacent known chunks and true between adjacent ones; after the highest known seq it is true only if that seq is the last in `allSeqs`; `startMs/endMs/platform` of an empty index.
  - `screenWindow.test.ts`: `chunkSeqsInWindow` returns exactly the chunks overlapping `[p−10 s, p+30 s]`, ascending; custom behind/ahead; `ChunkCache.retainOnly([2,3])` evicts 1 and returns `[1]`.
  - `screenChunkSource.test.ts` (injected `fetchImpl` returning gzipped JSON via `CompressionStream`, like `parseChunk.test.ts`): 200 → payload; 403 then refresh then 200 → payload and `refresh` called once with `(seq, seq)`; 403 twice → `null`; refresh rejects → `null`; aborted signal → rejects `AbortError` (Review Focus 3).
  - `screenSession.test.ts` (fake `fetchChunk` generating 5 chunks of 10 s each, 2 fps): `start()` indexes all chunks and notifies subscribers; `frameImage` returns data only for resident chunks; `setPlayhead(25 s)` makes chunks around 15–55 s resident and evicts others (resident count ≤ 5 and never the whole session); `setPlayhead` beyond the scanned region triggers a fetch of the estimated chunk and `index.isResolved` flips to true once it lands (Review Focus 2); a chunk whose fetch returns `null` is skipped and retried on a later `setPlayhead`.
- [ ] **Step 2:** `npx vitest run src/replay src/utils` → FAIL.
- [ ] **Step 3:** Implement the four modules (pure TypeScript, no React). `ScreenReplaySession` keeps parsed `payload` objects in a `ChunkCache<unknown>` (base64 strings stay inside; that bounded cache is the memory budget) and drops all other payloads right after parsing metadata. Add `listSessionChunks` to `replayApi` following the existing `request<…>()` pattern.
- [ ] **Step 4:** Re-run → PASS; `npx tsc -b` clean.
- [ ] **Step 5:** Commit `feat(replay): progressive screen-session loading core (index, window cache, chunk source)`.

### Task 8: Screen renderer and frame planner

**Files:**
- Create: `frontend/src/replay/screenPlanner.ts` (+ `.test.ts`), `frontend/src/components/ScreenReplayRenderer.tsx`
- Modify: `frontend/src/dashboard.css` (renderer styles: stage, buffering badge, touch ripple — reuse the `.mw-touch-*` look)

**Interfaces — Produces:**
```ts
// screenPlanner.ts
export function planScreenFrame(input: {
  frames: ScreenFrameMeta[]; playheadMs: number; decoded: ReadonlySet<string>; resolved: boolean; aheadMs?: number /*2000*/
}): { show: ScreenFrameMeta | null; due: ScreenFrameMeta | null; buffering: boolean; prefetch: ScreenFrameMeta[] }
export const frameKey = (f: ScreenFrameMeta) => `${f.chunkSeq}:${f.index}`
// ScreenReplayRenderer.tsx
export function ScreenReplayRenderer(props: { session: ScreenReplaySession; currentTs: number; width: number; height: number }): JSX.Element
```
Planner rules: `due` = `activeScreenFrameAt(frames, playhead)`; `show` = `due` if its key is in `decoded`, else the latest decoded frame with `ts ≤ playhead` (never one with `ts > playhead`), else `null`; `buffering = !resolved || (due !== null && !decoded.has(key(due)))`; `prefetch` = frames with `due.ts < ts ≤ playhead + aheadMs` not yet decoded, ascending.

- [ ] **Step 1: Write failing tests** (`screenPlanner.test.ts`): due decoded → `show === due`, `buffering false`; due not decoded but an earlier one decoded → `show` is the earlier one, `buffering true`; decoded set contains a frame with `ts > playhead` → never chosen; nothing decoded → `show null`; `resolved:false` → `buffering true` even if `due` is decoded (Review Focus 2); `prefetch` limited to the 2 s look-ahead, ascending, excluding decoded.
- [ ] **Step 2:** `npx vitest run src/replay/screenPlanner.test.ts` → FAIL.
- [ ] **Step 3:** Implement the planner, then the component: a `<canvas>` sized to `width×height` keeping the frame's aspect ratio (letterbox); on each render compute `planScreenFrame`, decode planned frames with `createImageBitmap(blob, { colorSpaceConversion: 'none', premultiplyAlpha: 'none' })` (blob from `session.frameImage` base64 via `Uint8Array.from(atob(...))`), keep decoded bitmaps in a `Map` keyed by `frameKey` and `close()` bitmaps whose `ts` is more than 10 s behind the playhead; draw `show`; render a "Buffering…" badge when `buffering`; overlay `touchMarkerAt(...)` ripple. Call `session.setPlayhead(currentTs)` from an effect.
- [ ] **Step 4:** Re-run planner test → PASS; `npx tsc -b` clean.
- [ ] **Step 5:** Commit `feat(replay): screen frame planner and canvas renderer`.

### Task 9: Wire screen sessions into `ReplayPlayer`

**Files:** Modify `frontend/src/replay/model.ts`, `frontend/src/components/ReplayPlayer.tsx`, `frontend/src/replay/playback.ts` (`fallbackNotice` stays exhaustive); Test `frontend/src/replay/normalize.test.ts`.

**Interfaces:**
- Consumes: `isScreenSession`, `parseScreenChunk` (Task 6); `ScreenReplaySession`, `ScreenChunkSource` (Task 7); `ScreenReplayRenderer` (Task 8).
- Produces: `ReplayModel` gains `{ kind: 'screen'; platform: 'ios' | 'android'; startMs: number; endMs: number; session: ScreenReplaySession }`.

- [ ] **Step 1: Write failing test** in `normalize.test.ts`: `buildReplayModel ignores artracker-screen-v1 payloads` (a screen payload alone → `{kind:'none', reason:'no-visual-data'}`; a screen payload next to an rrweb payload → `kind:'web'`). This pins that the eager path never misreads screen chunks; screen detection happens in the player.
- [ ] **Step 2:** Run it → expected PASS or FAIL depending on `extractMobileFrames` (it already requires `format` to match); if it passes, keep it as a regression guard and continue.
- [ ] **Step 3:** In `ReplayPlayer.tsx`:
  1. Replace the chunk-loading effect so it first **probes**: fetch the lowest-seq chunk and, when the session has more than one chunk, the highest-seq chunk (through `ScreenChunkSource.fetchChunk`); `isScreenSession(probes)` decides the path (Review Focus 1).
  2. Screen path: create one `ScreenReplaySession` per `(session.id, chunkSig)`, `start()` it, derive the model `{kind:'screen', platform: index.platform ?? projectPlatform ?? 'android', startMs: index.startMs, endMs: Math.max(index.endMs, index.startMs + session.duration_ms), session}` and re-render on `session.subscribe`. Refresh function: `replayApi.listSessionChunks(session.project_id, session.id, from, to).then(r => r.chunks)`.
  3. Non-screen path: unchanged eager `loadChunkPayloads` (reuse already-fetched probe payloads is optional).
  4. Rendering: `model.kind === 'screen'` → `<ScreenReplayRenderer … currentTs={model.startMs + currentTimeMs} />`; `effectivePlatform` uses `model.platform` for `screen` too; the rAF clock already falls back to dt-based time when `playerRef` is null.
  5. Mode badge: show `Screenshot` / `Wireframe` / `DOM` next to the existing replay header chips.
- [ ] **Step 4:** `npm test && npx tsc -b`. Expected: all pass, TypeScript clean. Manual smoke with local Postgres: `make db-up`, run API with `DATABASE_URL` set to the local DSN (never `.env`'s), upload a fixture session through chunk-url → PUT → chunk-ack, open it in `make web`: frames switch, scrubbing works, buffering badge appears on far seeks.
- [ ] **Step 5:** Commit `feat(replay): play artracker-screen-v1 sessions in the player`.

### Task 10: Settings toggle for the project switch

**Files:**
- Create: `frontend/src/components/ReplaySettingsPanel.tsx`
- Modify: `frontend/src/api.ts` (`Project.replay_screenshot_enabled: boolean`, `projectApi.setReplaySettings`), `frontend/src/pages/SettingsPage.tsx` (third tab "Replay")

**Interfaces:** Produces `projectApi.setReplaySettings(projectId: number, body: { screenshot_enabled: boolean }): Promise<Project>`; `ReplaySettingsPanel(props: { project: Project; canManage: boolean; onChange?: (p: Project) => void })`.

- [ ] **Step 1:** (No jsdom in this repo, so the panel is verified by typecheck + manual run; keep the logic trivial.) Add the API type and function following `keyApi.create`'s request style (`method: 'PATCH'`, JSON body).
- [ ] **Step 2:** Implement the panel: a toggle "Screenshot replay" (disabled unless `canManage`), explanatory line "The app must also enable `SCREENSHOT` mode in its SDK config", optimistic update with rollback + `useToast` error on failure. Add the "Replay" tab to `SettingsPage` (`activeTab` union gains `'replay'`), rendered only for a selected project.
- [ ] **Step 3:** `npx tsc -b && npm test`. Expected: clean/pass. Manual: toggle off with the local API, then `POST /replay/chunk-url` with `format:"artracker-screen-v1"` → 403 (curl against the local API only).
- [ ] **Step 4:** Commit `feat(dashboard): project setting to disable screenshot replay`.

### Task 11: Playwright pixel/ordering test (acceptance 1) and memory check (acceptance 3)

**Files:**
- Create: `frontend/playwright.config.ts`, `frontend/harness/screen.html`, `frontend/harness/screen-main.tsx`, `frontend/e2e/screen-replay.spec.ts`
- Modify: `frontend/package.json` (devDependency `@playwright/test`, script `"test:e2e": "playwright test"`), `frontend/tsconfig.node.json` or equivalent so the config type-checks

**Interfaces:** `harness/screen.html` is a dev-only entry (not part of `vite build`; Vite serves it in dev) that mounts `ScreenReplayRenderer` with a fake `ScreenReplaySession` fed from `window.__fixture` (frames generated in-page: each frame is a solid-color JPEG whose color encodes its index, made with `canvas.toBlob('image/jpeg')`, base64-wrapped into a screen payload).

- [ ] **Step 1: Write the failing spec** `screen-replay.spec.ts`:
  - `canvas shows the frame with greatest ts <= T and never a later one`: 40 frames at 500 ms spacing; for 15 sampled playheads (including exact `ts` equality and 1 ms before a frame) set `currentTs`, read the canvas center pixel and assert it matches the color of frame `activeScreenFrameAt` within 2/255 per channel; additionally assert the pixel never equals the color of any frame with `ts > T`.
  - `seek far ahead shows buffering and keeps the previous frame` (fake session delays one chunk).
  - `perf (E2E_PERF=1 only)`: synthetic 30-minute, 2 fps, 80 KB-frame session through the real `ScreenReplaySession` with generated chunks; scrub across it; assert `performance.memory.usedJSHeapSize` stays under 150 MB (Chromium flag `--enable-precise-memory-info`).
- [ ] **Step 2:** Run `cd frontend && npm i -D @playwright/test && npx playwright test` — expected FAIL (harness missing). Config: `use.channel: 'chrome'` (system Chrome; no browser download), `webServer: { command: 'npm run dev -- --port 5199', url: 'http://localhost:5199/harness/screen.html', reuseExistingServer: true }`.
- [ ] **Step 3:** Implement the harness files until the spec passes; keep harness code out of the production bundle (it is not referenced from `index.html`).
- [ ] **Step 4:** `npx playwright test` → PASS; `E2E_PERF=1 npx playwright test -g perf` → PASS. `npm run build` still succeeds.
- [ ] **Step 5:** Commit `test(replay): playwright pixel-order and memory checks for screen replay`.

### Task 12: Docs, OpenAPI and final verification

**Files:** Modify `backend/internal/handler/openapi.go`, `frontend/src/pages/DocsPage.tsx`; run checks.

- [ ] **Step 1:** Document in `openapi.go`: `GET /replay/config`, `PATCH /projects/{projectID}/replay-settings`, `GET /projects/{projectID}/sessions/{sessionID}/chunks`, the `format` field and the 403 `screenshot_disabled` response on `chunk-url`, `replay_screenshot_enabled` on the project schema. In `DocsPage.tsx` (Session Replay section) document `artracker-screen-v1` (Global Constraints shape), the switch, and the `SCREENSHOT`/`WIREFRAME` SDK modes (`.env`-style names untouched).
- [ ] **Step 2:** Run everything: `cd backend && go vet ./... && go test -p 1 ./...` (with `TEST_DATABASE_URL` set to the local DSN); `cd frontend && npm test && npx tsc -b && npm run build && npx playwright test`. Expected: all green; `openapi_test.go` passes.
- [ ] **Step 3:** Replay the real session `01a1c339-3fb0-77ba-9e68-2feabafd6e96` data (chunks already downloaded to `/tmp/claude-1000/-home-gli-it-IdeaProjects-artracker/658fd01a-1ae0-41c9-a023-5989a85f63c3/scratchpad/data/chunk_1.json` and `chunk_2.json`, with the harness in `…/scratchpad/harness`; re-download read-only from R2 if the scratchpad is gone) through the player harness path for rrweb and confirm zero `Node … not found` warnings (acceptance 2); confirm an iOS `artracker-mobile-v1` fixture still renders as wireframe.
- [ ] **Step 4:** Per `CLAUDE.md`: `graphify update .` (AST-only).
- [ ] **Step 5:** Commit `docs(replay): document screenshot mode contract, switch and endpoints`.
