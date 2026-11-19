# App Start Profiling (cold / warm / hot + "what made it slow") — Design

Date: 2026-11-19 · Repos: `artracker` (backend + dashboard), `artracker-android-sdk`, `artracker-ios-sdk`
Plans (to be written after this design is approved):
- `docs/superpowers/plans/2026-11-19-app-start-backend-web.md`
- `docs/superpowers/plans/2026-11-19-app-start-android.md`
- `docs/superpowers/plans/2026-11-19-app-start-ios.md`

## 1. Intent

Teams using the SDK want to know **what makes their app's startup slow**, on Android and iOS. A single
"cold start took 2.4 s" number does not help: the developer still has to guess which code to look at.

The feature has three layers, all shipped together:

1. **Start measurement.** Every cold, warm and hot start is measured, with the total time and a **phase
   breakdown** (for example: process start → SDK init → first Activity → first frame).
2. **Startup profiling.** On a sampled fraction of cold starts, the SDK samples the **main-thread stack
   every 10 ms** from SDK init until the first frame. The backend symbolicates the stacks with the
   R8/ProGuard mappings and dSYMs it already stores.
3. **Dashboard.** An "App Start" page shows percentiles per version and device, the phase breakdown, a
   **"slowest functions during startup" table aggregated across many starts**, and a **flamegraph** of
   any single profiled start.

Success: a developer opens the App Start page for a release, sees which phase is slow, and the top rows of
the functions table name the app functions (deobfuscated / symbolicated) that consumed that time, without
adding any code beyond enabling the feature.

## 2. Current state (verified 2026-11-19)

| Area | State |
|---|---|
| Android SDK | `lifecycle/AppStartTracker.kt` measures **cold start only**: `Process.getStartUptimeMillis()` → first frame of the first Activity (`DecorViewFirstFrame`). Emits `items.event("app_start", {type:"cold", duration_ms, first_screen})`. Solid guards (background-launched process, late init, splash/trampoline, >60 s). No phases, no warm/hot, no profiling. SDK starts from the host's `Application.onCreate` (no auto-init provider, by design). No remote config. |
| iOS SDK | **No launch measurement.** `LaunchMetrics.swift` only times the SDK's own `configure()`. `session_start` carries `launch_type:"cold"` always. No process start time, no prewarm check, no stack sampler, **no C/ObjC target**. MetricKit subscriber exists but ignores launch metrics/diagnostics. |
| Backend | `app_start` events land as generic rows in `telemetry_events`; nothing reads them. `perf` items go through `worker.collectPerf` → `session_perf_batches`. Symbolication runs **at read time per request**, text-regex based; per-frame APIs exist (`RetraceMethod`, `SymbolicateAddress`). The iOS symbolicator scans all DWARF line tables per address and ignores load address/slide. Artifacts are looked up by `release_version` only. No retention jobs exist anywhere. |
| Dashboard | Only a timeline text row for `app_start`. No chart/flamegraph library; charts are hand-built SVG. |

## 3. Decisions made by Claude (check these)

- **Measurement is on by default** (it already is on Android); **profiling is opt-in** with a local
  sample rate (`profileSampleRate`, default `0.1` once enabled). No server-side remote config in v1: the
  SDKs have none for perf today, and a startup decision has to be made before any network call anyway.
- **Only cold starts are profiled.** Warm and hot starts get measurement + phases only.
- **Profiling starts at SDK init.** Code that runs before `ARTracker.init` / `configure` (Android
  ContentProviders, iOS pre-main) is measured as a **phase duration but has no stacks**. Docs tell users to
  call init as the first line of `Application.onCreate` / `didFinishLaunching`. An early-start hook (Android
  ContentProvider, iOS constructor-started sampler) is a v2 candidate.
- **The `app_start` record moves to a `perf` item** (`type:"perf", name:"app_start"`), stored in its own
  table, like `perf.samples`. Android stops emitting the old `event` named `app_start`. The old replay
  timeline row for it goes away (acceptable; it was text-only).
- **Profiles travel on a dedicated endpoint** (`POST /api/v1/app-start/profiles`), not inside an ingest
  item. A profile (hundreds of samples, hundreds of unique frames) does not fit the 24 KB (Android) / 32 KB
  (backend) attribute caps, and raising those caps for one item would weaken every other path.
- **Symbolication runs in a background worker** (like the heatmap worker), once per profile, and the result
  is stored. Read-time symbolication of hundreds of frames per page view is too slow with today's
  symbolicators.
- **iOS gets a small C target** (`ARTrackerNative`): the Mach stack sampler needs `thread_suspend` /
  `thread_get_state` / safe memory reads, and a C constructor gives a cheap "pre-main end" timestamp.
- **iOS prewarmed launches** (`ActivePrewarm=1`, iOS 15+) are reported as **`warm`**, measured from SDK init.
  Mixing them into cold would show 10 s+ "cold starts" that never happened.
- **A process launched in the background that the user opens later is a `warm` start**, never `cold`, on
  both platforms (Android: existing `startedInForeground` guard drops cold; the later Activity creation is
  warm. iOS: `applicationState == .background` at `configure` → no cold; the first foreground transition is
  reported as `warm`, anchored at `willEnterForegroundNotification`).
- **"App code" is decided by package/image, sent by the SDK.** Android sends `app_packages` (default: the
  `applicationId`, plus an optional `appStart.inAppPackages` config list); a frame is app code when its
  **deobfuscated** class starts with one of them. iOS uses the image `app` flag. Being present in the R8
  mapping is **not** a signal (R8 maps library classes too).
- **Environment is stored and filterable.** `app_starts` and `app_start_profiles` get `environment` from the
  envelope context; the dashboard filter defaults to `production` when that environment has data, else all.
  Debug builds are not skipped by the SDK (users may want to profile them), but they are never mixed into the
  default view.
- **Old Android `app_start` events (pre-upgrade SDKs) are not converted.** They stay in `telemetry_events`
  untouched; the App Start page shows data only from upgraded SDKs and its empty state says so.
- **R8 inline frames are expanded** during symbolication: one obfuscated frame may become several frames,
  and stacks are rewritten accordingly before computing stats.
- **Profiles are kept 30 days**, then deleted by the worker. This is the first retention job in the repo; it
  is justified by profile size (up to 1 MB gzip each). `app_starts` rows are kept indefinitely (small).

## 4. Start types and phases

All times are milliseconds. Phases are **contiguous** and listed in order; their sum equals `duration_ms`.
A phase whose boundary is unavailable is omitted and its time merged into the next phase.

### 4.1 Android

| Type | Definition | Start anchor | End |
|---|---|---|---|
| `cold` | New process, launched to show UI (existing guards in `AppStartTracker` stay) | `Process.getStartUptimeMillis()` | first frame of the first Activity that draws |
| `warm` | Process alive, **no Activity alive**, an Activity is created (e.g. back-pressed out then reopened, or a process started by a broadcast/push/WorkManager that the user now opens) | `onActivityPreCreated` (API 29+) else `onActivityCreated` | first frame |
| `hot` | Process and Activity alive, app returns from background (Activity `onStart` after `onStop`) | `onActivityStarted` | first frame after resume |

Cold phases:

| Phase | From → to |
|---|---|
| `process_to_init` | process start → `ARTracker.init` entry. On **API 35+**, replaced by three phases from `ApplicationStartInfo` timestamps: `bind_application` (fork → bind), `content_providers` (bind → `APPLICATION_ONCREATE`), `app_on_create_pre_init` (→ init) |
| `application_on_create` | init → first `onActivityCreated` (rest of `Application.onCreate` + queued main-thread work) |
| `activity_create` | first Activity created → resumed (when a splash/trampoline is given up, from the **first** Activity's creation; `trampoline: true` is set) |
| `first_draw` | resumed → first frame |

Warm phases: `activity_create`, `first_draw`. Hot phases: `activity_start` (started → resumed), `first_draw`.

Warm/hot guards: drop when the app goes to background before the first frame, or duration > 10 s
(warm/hot) / 60 s (cold, unchanged).

### 4.2 iOS

| Type | Definition | Start anchor | End |
|---|---|---|---|
| `cold` | New process, not prewarmed | process start: `sysctl(KERN_PROC_PID).kp_proc.p_starttime` | first frame |
| `warm` | New process that was **prewarmed** (`ProcessInfo.environment["ActivePrewarm"] == "1"`) | SDK `configure` | first frame |
| `warm` | New process **launched in the background** (`applicationState == .background` at `configure`: silent push, background fetch, location) that the user later brings to the foreground | first `willEnterForegroundNotification` | first frame |
| `hot` | Existing process returning from background | `willEnterForegroundNotification` | first frame after `didBecomeActive` |

"First frame" = the first `CADisplayLink` callback after the first `didBecomeActive` (hot: after the
`didBecomeActive` that follows the foreground transition).

Cold phases:

| Phase | From → to |
|---|---|
| `pre_main` | process start → `ARTrackerNative` C constructor (dyld loading, static initializers of earlier images) |
| `pre_init` | constructor → `ARTracker.configure` entry (`main`, `UIApplicationMain`, start of `didFinishLaunching`) |
| `did_finish_launching` | `configure` → `UIApplication.didFinishLaunchingNotification` |
| `scene_activation` | → first `didBecomeActive` |
| `first_draw` | → first frame |

Warm phases (prewarmed): `did_finish_launching`, `scene_activation`, `first_draw`. Warm (background-launched)
and hot: `foreground_transition`
(willEnterForeground → didBecomeActive), `first_draw`.

Guards: drop when `configure` runs more than 10 s after process start (late init) or after the app already
became active, when the app goes to background before the first frame, or duration > 60 s.

## 5. Wire contract

### 5.1 `app_start` item (both SDKs, via `POST /api/v1/ingest`)

```json
{
  "id": "<uuid>", "type": "perf", "name": "app_start",
  "timestamp": "<ISO-8601 of the start anchor>",
  "session_id": "<session id>",
  "attributes": {
    "v": 1,
    "platform": "android",
    "start_id": "<uuid, unique per start>",
    "type": "cold",
    "duration_ms": 2412,
    "phases": [
      { "name": "process_to_init", "ms": 310 },
      { "name": "application_on_create", "ms": 1502 },
      { "name": "activity_create", "ms": 380 },
      { "name": "first_draw", "ms": 220 }
    ],
    "first_screen": "MainActivity",
    "trampoline": false,
    "profiled": true,
    "build": "1203",
    "device": { "model": "SM-A145F", "os_version": "14", "ram_mb": 3712, "cores": 8 }
  }
}
```

Rules: `start_id` links the item to its profile. `profiled: true` means a profile upload will follow.
Missing values are omitted, never 0. `app_version` comes from the envelope `context`, like other items.

### 5.2 Profile upload `POST /api/v1/app-start/profiles`

Auth: `X-Artracker-Key` (ingest key), same middleware and rate limit as `/ingest`. Body: gzip JSON, at most
**1 MB compressed / 4 MB uncompressed**. Response `202`. Duplicate `start_id` → `202`, ignored.

```json
{
  "sdk": { "name": "artracker-android", "version": "0.x" },
  "context": { "app_version": "3.2.0", "build": "1203", "environment": "production" },
  "profile": {
    "v": 1,
    "start_id": "<same uuid>",
    "platform": "android",
    "app_packages": ["com.example.shop"],
    "interval_ms": 10,
    "t0": "process_start",
    "frames": [
      { "c": "a.b.c", "m": "d", "l": 42, "f": "SourceFile" }
    ],
    "stacks": [[0, 3, 7]],
    "samples": [{ "t": 318, "s": 0 }]
  }
}
```

- `frames`: unique frames. **Android**: `c` class, `m` method, `l` line (omit if <0), `f` file. **iOS**:
  `{ "a": "0x1045f3a20", "i": 2, "sym": "dladdr name?" }` where `i` indexes `images`.
- iOS only: `"images": [{ "uuid": "…", "name": "MyApp", "load": "0x104000000", "app": true }]`, holding
  only images referenced by frames. `sym` is filled on device with `dladdr` for **non-app** images (system
  frameworks, whose symbols the server cannot resolve). App-image frames are symbolicated server-side.
- `stacks`: unique stacks as frame indexes, **root first**. At most **128 frames per stack**; when deeper,
  the frames nearest the root are dropped (they are always the same `main` / `ZygoteInit` frames).
- `samples`: `t` = ms since the start anchor of the cold start (`t0`), `s` = stack index. At most **1000
  samples** (10 s at 10 ms).

### 5.3 Profiler behaviour (both SDKs)

- Decided once per process at init: `enabled && type will be cold && random() < profileSampleRate`. If the
  start later turns out not to be a reportable cold start (guards in §4), the profile is discarded.
- Runs on its own thread, waking every `interval_ms` (10). Stops at first frame, at background, at 10 s, or on
  SDK disable. Buffers in memory with pre-allocated capacity.
- **Overhead budget**: mean sampling cost ≤ 0.5 ms on the main thread per sample, measured in a debug
  counter. If the running mean exceeds 1 ms, the interval doubles (10 → 20 → 40 ms) and `interval_ms`
  reflects the final value (samples carry real `t`, so the backend does not assume a fixed interval).
- After the first frame, the profile is serialised **off the main thread**, written to a file in the SDK's
  directory, and uploaded by the existing flush machinery. At most 3 pending profile files; the oldest is
  dropped. A file older than 3 days or failing 5 uploads is deleted. 413 → delete (never retried).
- Android: sampler reads `Looper.getMainLooper().thread.stackTrace`.
- iOS: sampler (C) suspends the main thread (`thread_suspend`), reads its registers
  (`thread_get_state`, arm64 and x86_64 for simulator), walks frame pointers with bounded, fault-safe reads
  (`vm_read_overwrite` or equivalent), resumes. **No allocation, locks or Objective-C/Swift runtime calls
  while the thread is suspended.** Image list built afterwards from `_dyld_get_image_header` + `LC_UUID`.

## 6. Backend

### 6.1 Storage (migration `0019_app_start.sql`)

- `app_starts(id, project_id, start_id UNIQUE per project, session_id, platform, environment, type, duration_ms,
  phases jsonb, first_screen, app_version, build, os_version, device_model, ram_mb, trampoline,
  profiled bool, started_at timestamptz, received_at)`. Indexes `(project_id, started_at)`,
  `(project_id, platform, type, app_version, started_at)`.
- `app_start_profiles(start_id, project_id, platform, environment, app_version, build, raw bytea (gzip as received),
  status 'pending'|'done'|'missing_symbols'|'failed', attempts, next_attempt_at, symbolicated jsonb,
  received_at)`.
- `app_start_fn_stats(project_id, start_id, platform, app_version, function, is_app bool, self_ms,
  total_ms, started_at)`. Index `(project_id, platform, app_version, started_at)`.

### 6.2 Ingest

- `worker.collectPerf` routes `perf` items by name: `perf.samples` → existing path; `app_start` →
  `ParseAppStartItem` (validate `v==1`, known `type`, phases sum within ±5 ms of `duration_ms`, clamp
  values) → `app_starts` batched insert. Unknown names are dropped as today.
- New handler `IngestAppStartProfile`: key auth, gzip/size limits, JSON shape validation (indexes in range,
  sample/stack/frame caps), insert into `app_start_profiles` with `status='pending'`. A profile may arrive
  before its `app_starts` row; the worker does not depend on the row.

### 6.3 Symbolication worker (`internal/appstart`)

- Started from `cmd/api/main.go` behind `APPSTART_WORKER` (default on), ticking every 30 s, batch 20.
- Per profile: load the artifact once — Android: ProGuard mapping by `(app_version, build)`, falling back to
  newest for `app_version`; iOS: dSYM by **image `uuid`** (`project_release_artifacts.debug_uuid` is already
  indexed), with the load address subtracted (`address - load + __TEXT vmaddr`).
  - Missing artifact → `missing_symbols`, retried with backoff (5 min → 6 h) for 24 h, then kept as-is
    (frames shown raw). An artifact upload for that version/uuid re-queues matching profiles.
- Resolve each **unique frame once**, write the symbolicated frame table into `symbolicated`, then compute
  `app_start_fn_stats` per function: `self_ms` = Σ sample gaps where the function is the leaf, `total_ms` =
  Σ sample gaps where it appears anywhere in the stack (counted once per sample).
- Function identity: Android `class.method` after retrace (inline frames expanded into separate frames);
  iOS demangled symbol, else `sym` from device, else `image+0xoffset`. `is_app`: Android – the
  **deobfuscated** class starts with one of the profile's `app_packages`; iOS – image flagged `app` (main
  binary or embedded app frameworks).
- Needed refactors: move `loadProguardRetracer` / `loadIOSSymbolicator` out of `handler/issues.go` into a
  shared loader with a small LRU (by artifact id); give `IOSSymbolicator` a sorted address index built once
  per dSYM (today's per-address linear scan is too slow for hundreds of addresses).
- Retention: the same worker deletes `app_start_profiles` and `app_start_fn_stats` older than 30 days, once
  per hour.

### 6.4 Dashboard API (JWT, project member)

- Every endpoint below also takes `environment` (default: `production` when it has data in the range, else
  all environments).
- `GET /projects/:id/app-starts/summary?platform&type&version&from&to&ram_tier` → count, p50/p75/p90/p95,
  median per phase, daily p50/p90 trend, per-version p50/p90 (latest 10 versions).
- `GET /projects/:id/app-starts/functions?platform&version&from&to&app_only` → top 50 functions: function,
  is_app, % of profiled starts containing it, median and p90 `total_ms` (over starts containing it), median
  `self_ms`, an example `start_id`.
- `GET /projects/:id/app-starts?…&sort=slowest|recent&limit&cursor` → list rows.
- `GET /projects/:id/app-starts/:startId` → the start (phases, device, session link) plus, when profiled, the
  symbolicated profile (frames, stacks, samples) and its status.
- `ram_tier`: `low` < 4 GB, `mid` 4–8 GB, `high` > 8 GB.

## 7. Dashboard

- New page `/dashboard/app-start`, nav item **"App Start"** in the **Observability** group. Mobile projects
  only (web projects see an empty state explaining it's mobile-only).
- Toolbar: environment, platform, start type (default `cold`), app version, RAM tier, date range.
- Empty state (mobile project, no data): explains that the feature needs the upgraded SDK, and that profiling
  must be enabled for the functions table and flamegraphs.
- Sections, top to bottom:
  1. KPI tiles: p50, p90, count, % profiled.
  2. Trend (daily p50/p90) and per-version bars (spot a regression).
  3. Phase breakdown: one stacked horizontal bar of median phase times, labelled.
  4. **Slowest functions during startup** table (app functions by default, toggle "include libraries").
     Row → opens the example start.
  5. Slowest starts list → start detail.
- Start detail (`/dashboard/app-start/:startId`): phase timeline bar, device/version facts, link to its
  session replay when one exists, and a **flamegraph** (icicle, root on top, x = time) built in hand-written
  SVG, with hover (function, self/total ms) and click-to-zoom. Status banner when symbols are missing,
  pointing to the symbol-upload docs.
- Demo seed: `cmd/seed` generates app starts (all types, both platforms, a regression between two
  versions) and a handful of already-symbolicated profiles, so the page works in demo mode.

## 8. Docs to update

- Dashboard `/docs` page (`DocsPage.tsx`): `app_start` item, profile endpoint, profiler config.
- OpenAPI (`handler/openapi.go`): add `perf` to the `RawItem.type` enum, the profile endpoint and the four
  dashboard endpoints (`TestOpenAPICoversRoutes` enforces routes).
- Android SDK: `README.md`, `ARCHITECTURE.md` event table, `PERF.md` (overhead measurements), `CHANGELOG.md`.
- iOS SDK: `README.md` config table and "Performance monitoring" section.
- Repo `README.md` "What it does" list.

## 9. Goals, non-goals, anti-goals

Goals: §1. Both SDKs send the same contract; the dashboard needs no per-platform code beyond labels.

Non-goals (v1):
- Web startup (no SDK hook equivalent; Web Vitals is a separate feature).
- Profiling warm/hot starts, or code before SDK init.
- Remote config / server-controlled sample rates.
- Alerts on startup regressions (a natural follow-up using `app_starts`).
- Symbolicating iOS system frameworks server-side (device-side `dladdr` names only).
- MetricKit `MXAppLaunchDiagnostic` ingestion.
- Showing the start on the session replay timeline.

Anti-goals (failure even if it "works"):
- The SDK makes startup **measurably slower**. Measurement-only overhead must stay within today's
  `AppStartTracker` budget (≈5 ms main-thread at init); profiling within §5.3.
- **Misleading numbers**: background-launched processes, prewarmed iOS launches or late SDK init counted as
  cold starts. When in doubt, drop the start.
- Any crash, ANR or deadlock caused by the sampler (the iOS suspend path is the main risk).
- Profiles carrying user data. Stacks contain only code locations, never arguments or values.

## 10. Rollout order

1. **Backend + dashboard** with demo seed data (fixes the contract early; UI can be reviewed before SDKs).
2. **Android SDK** (measurement → warm/hot → profiler → upload).
3. **iOS SDK** (C target + measurement → hot/warm → sampler → upload).

## 11. Left to the builder

- Exact SQL for percentiles and the function aggregation (window sizes, caps).
- Flush integration details for profile files in each SDK (WorkManager vs `Flusher` on Android; `Uploader`
  on iOS), within the limits in §5.3.
- Flamegraph rendering details (colour by app/library, min width before a box is hidden).
- Test strategy per repo, following each repo's conventions (Robolectric + fakes; XCTest + injection seams;
  Go table tests + DB tests on `artracker_test`).
