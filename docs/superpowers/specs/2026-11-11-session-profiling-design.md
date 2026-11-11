# Session Profiling (CPU / Memory / FPS) + Replay Workspace — Design

Date: 2026-11-11 · Repos: `artracker` (backend + dashboard), `artracker-android-sdk`, `artracker-ios-sdk`
Plans:
- `docs/superpowers/plans/2026-11-11-replay-workspace-and-profiling-dashboard.md`
- `docs/superpowers/plans/2026-11-11-session-profiling-android.md`
- `docs/superpowers/plans/2026-11-11-session-profiling-ios.md`

## 1. Intent

1. **Replay UX (now).** The session replay page stacks the stage (~600 px) above an activity stream (~300 px).
   The total is ~1,200 px against a ~750 px laptop viewport, so the user scrolls up and down to correlate
   video with logs. "Follow Playhead" makes it worse: `scrollIntoView` scrolls the window too. The replay page
   must become a **viewport-height workspace** with no page scroll. Video, the event panel and the timeline
   must be visible together.
2. **Profiling (later, but the layout is built for it now).** Mobile SDKs sample CPU, memory, FPS/jank, thread
   count, battery and thermal state about once per second while the app is in the foreground. The dashboard
   draws the samples as **series tracks on the replay timeline**, aligned with video and events, and adds a
   **Performance** tab.

Web profiling is out of scope. Browsers do not expose CPU. A later web plan would use Long Tasks and Web Vitals.

## 2. Decisions made by Claude (check these)

- Profiling is **opt-in** (`enabled = false` by default), the same as screenshot replay.
- Sampling runs **only in the foreground**, for every session (no sample rate). Overhead target is ≤ 1 % CPU on
  the sampling thread.
- Perf samples travel through the **existing `/api/v1/ingest` pipeline** as a new item `type: "perf"`, one item
  per batch of samples. They are **stored in their own table** and never in `telemetry_events`. Otherwise
  12 existing queries (logs, issues, nav counts, analytics) would need a `kind <> 'perf'` filter.
- OS signals are sent as ordinary `event` items so they show in the activity stream with no extra work:
  `perf.memory_warning` and `perf.thermal_change`.
- Breaches ("High CPU", "Low FPS", "Jank burst") are **computed in the dashboard** from the samples, not
  sent by the SDK. Thresholds are in §6.4.
- Sample `t` is **device wall-clock ms**, the same clock as replay frames, so tracks line up with the video.
  No server skew correction is applied.
- iOS does **not** collect battery in v1. Reading it requires `UIDevice.isBatteryMonitoringEnabled = true`, a
  global side effect on the host app.

## 3. Wire contract `perf.samples` v1 (shared by both SDKs)

The item is sent inside the normal ingest envelope (`POST /api/v1/ingest`, gzip):

```json
{
  "id": "<uuid>",
  "type": "perf",
  "name": "perf.samples",
  "timestamp": "<ISO-8601 of the first sample>",
  "session_id": "<session id>",
  "attributes": {
    "v": 1,
    "platform": "android",
    "interval_ms": 1000,
    "device": { "ram_mb": 7812, "storage_mb": 128000, "os_version": "14", "cores": 8, "refresh_hz": 120 },
    "samples": [
      { "t": 1794214384360, "cpu": 23.5, "mem_mb": 212.4, "fps": 58, "jank": 1, "frozen": 0,
        "threads": 34, "battery": 0.81, "thermal": "nominal",
        "ram_free_mb": 2310, "storage_free_mb": 51200, "net": "wifi" }
    ]
  }
}
```

Rules:
- `platform` is `"android"` or `"ios"`. `interval_ms` is the configured sample interval, 500–10000.
- `samples` holds 1–60 entries, sorted by `t` ascending. `t` is epoch ms. Every field except `t` is optional.
  A metric the platform cannot measure, or did not measure in that interval, is **omitted**, never sent as 0.
- An SDK flushes a perf item every **10 samples**, when the app goes to background, and when the session id
  changes. Samples from an old session are flushed under the old id.
- The item must stay under 24 KB of attributes, the Android redactor limit. 60 samples is about 7 KB.
- Servers older than this change drop unknown `type` values, so the rollout order does not matter.

## 4. Metric definitions

| Field | Meaning | Android | iOS |
|---|---|---|---|
| `cpu` | Process CPU % of the **whole device** (0–100), averaged over the interval: Δ(process CPU time) / Δwall / cores × 100 | `/proc/self/stat` utime+stime (fields 14+15) ÷ `_SC_CLK_TCK` | Δ of existing `Metrics.currentProcessCPUTimeMs()` |
| `mem_mb` | Resident memory of the app process, MB | `/proc/self/statm` field 2 × `_SC_PAGESIZE` (RSS) | `task_vm_info.phys_footprint` (matches the Xcode gauge) |
| `fps` | Frames per second in the interval. **Omitted when fewer than 2 frames** were seen (idle screen ≠ low FPS) | `FrameMetrics` frames reported for the resumed activity window | `CADisplayLink` callbacks delivered (main-thread throughput) |
| `jank` | Frames whose duration > 2 × the expected frame interval | `FrameMetrics.TOTAL_DURATION` > 2 × (1e9 / refresh) | link `timestamp` delta > 2 × (`targetTimestamp − timestamp`) |
| `frozen` | Frames > 700 ms | same source | same source |
| `threads` | Thread count | `/proc/self/stat` field 20 | `task_threads` count |
| `battery` | 0–1 | `BatteryManager.BATTERY_PROPERTY_CAPACITY` / 100 | not sent (v1) |
| `thermal` | `nominal` / `fair` / `serious` / `critical` | `PowerManager.currentThermalStatus` (API 29+): NONE/LIGHT → nominal, MODERATE → fair, SEVERE → serious, CRITICAL/EMERGENCY/SHUTDOWN → critical | `ProcessInfo.thermalState` 1:1 |
| `device.ram_mb` | Total device RAM | `ActivityManager.MemoryInfo.totalMem` | `ProcessInfo.physicalMemory` |
| `ram_free_mb` | Free device RAM, MB (system-wide, not the app's) | `ActivityManager.MemoryInfo.availMem` | `host_statistics64` (`free + inactive + speculative` pages × page size) |
| `storage_free_mb` | Free space on the app's data volume, MB | `StatFs(Environment.getDataDirectory()).availableBytes` | `volumeAvailableCapacityForImportantUsageKey` of the home directory |
| `net` | Active connection: `wifi` / `cellular` / `ethernet` / `none`. Omitted when unknown (never `"unknown"`) | default-network `NetworkCapabilities` transport | `NWPath`: unsatisfied → `none`, else the interface type |
| `device.storage_mb` | Total size of that data volume | `StatFs.totalBytes` | `volumeTotalCapacityKey` |
| `device.os_version` | OS release, no product name (max 32 chars) | `Build.VERSION.RELEASE` (`"14"`) | `operatingSystemVersion` as `major.minor[.patch]` (`"26.4"`) |
| `device.cores` | | `Runtime.availableProcessors()` | `ProcessInfo.activeProcessorCount` |
| `device.refresh_hz` | | `Display.refreshRate` of the resumed window | `UIScreen.main.maximumFramesPerSecond` |

Why these ride in `perf` and not in `session.start`: the free values and `net` change during the session, and
the dashboard's Performance panel reads only the `/perf` endpoint. `device` is repeated in every batch, so a
session never lacks its static facts. `session.start` keeps its own device attributes unchanged.

OS signal events (`type: "event"`):
- `perf.memory_warning`.
  - Android: `onTrimMemory` with level `TRIM_MEMORY_RUNNING_LOW` (10) or `TRIM_MEMORY_RUNNING_CRITICAL` (15)
    while in the foreground. Attributes `{ "level": 10|15 }`.
  - iOS: the existing `didReceiveMemoryWarning` observer. Attributes `{}`.
- `perf.thermal_change`, attributes `{ "from": "...", "to": "..." }`, sent when a sample's `thermal` differs
  from the previous sample's.

## 5. Backend

- Table `session_perf_batches(project_id, session_id, platform, start_ts, end_ts, interval_ms, device jsonb,
  samples jsonb, received_at)`, indexed on `(project_id, session_id, start_ts)`.
- Ingest: the worker routes `type: "perf"` items to a `PerfSink` and never to `telemetry_events`. Validation:
  - `v == 1` and a non-empty `session_id`.
  - 1–60 samples after dropping any sample with `t <= 0`, then sorting by `t`.
  - Values are clamped: `cpu` 0–100, `mem_mb` 0–1,048,576, `fps` 0–240, `battery` 0–1. An unknown `thermal`
    is dropped.
  - Unknown keys are dropped (re-marshal from typed structs).
  - An invalid item counts as `dropped_invalid`.
- Read: `GET /api/v1/projects/:projectID/sessions/:sessionID/perf` returns
  `{ interval_ms, device, metrics: [...present metric keys], samples: [...], downsampled: bool }`.
  - Samples come from all batches merged, sorted by `t`, and deduplicated by `t` (first wins).
  - When there are more than **1,200** samples, the series is downsampled into 1,200 index buckets. Each bucket
    keeps the worst value: max of `cpu`, `mem_mb`, `jank`, `frozen` and `threads`; min of `fps` and `battery`;
    the most severe `thermal`.
  - A session with no data returns `200` with `samples: []`.

## 6. Dashboard UX

### 6.1 Workspace layout (replay page)
```
┌─[≡ Sessions]─ user · id ⧉ · 3m12s · iOS · ● 2 errors ─────────────── [⋯]─┐  header (1 row)
│                                           │ [Events] [Network] [Perf]     │
│               STAGE                       │ filter chips · ☑ Follow        │
│   (scaled to fit; no fixed min-height)    │ list (own scroll)             │
│                                           ├───────────────────────────────┤
│                                           │ Inspector (selected event)    │
├───────────────────────────────────────────┴───────────────────────────────┤
│ ▶ 00:15 / 03:12  1×  [Tracks ▾] [▴]   Screens / Network / CPU / RAM / FPS / Events │
└──────────────────────────────────────────────────────────────────────────────┘
```
- The page container is `height: 100dvh` minus page chrome, with `overflow: hidden`. Each region scrolls on
  its own. `window.scrollY` stays 0 during playback.
- The side panel is 380 px. At ≤ 1100 px it becomes a fixed bottom sheet with three snap heights:
  header-only (44 px), 45 dvh and 85 dvh.
- The session queue becomes a collapsible left rail (300 px), toggled with `≡ Sessions`. The choice persists
  in `localStorage`. The default is open when the viewport is ≥ 1600 px and closed below that.
- Header cleanup:
  - The device-mode toggle, resolution chip and recording-mode chip move into the `⋯` menu.
  - The failed-chunks warning stays visible.
  - The route strip shows only for mobile. Web already shows the URL in the browser chrome.

### 6.2 Playback behaviour
- Follow Playhead scrolls **only the list** (`list.scrollTop`), on every active-marker change, playing or
  paused. Any wheel or touch scroll inside the list turns Follow off. Re-checking it jumps back to the
  active event.
- Keyboard shortcuts are ignored while focus is in an input, textarea, select or contenteditable element, or
  when Ctrl, Meta or Alt is held:

  | Key | Action |
  |---|---|
  | Space | Play / pause |
  | ← / → | Seek −5 s / +5 s |
  | `[` / `]` | Previous / next event |
  | `s` | Cycle speed |
- "Previous event" goes to the latest marker more than 500 ms before the playhead. "Next event" goes to the
  first marker more than 50 ms after the playhead.

### 6.3 Timeline tracks
- Every lane is a track. `spans` covers screens and network, `series` covers cpu, mem and fps, and `markers`
  covers events. Each track has a 96 px label column that shows the value at the playhead.
- Tracks the session has no data for are not offered. `[Tracks ▾]` toggles visibility. The hidden set
  persists in `localStorage` under `artracker.replay.hiddenTracks`.
- `[▴]` collapses the timeline to the rail and the events track only, persisted under
  `artracker.replay.timelineExpanded`.
- When the network lane has more than 15 spans, it shows a `+N` label at its end.

### 6.4 Performance tab and breaches
- **At playhead:** CPU %, memory MB (and % of `device.ram_mb`), FPS / `refresh_hz`, jank, threads, thermal.
- **Session summary:** peak and average CPU, peak memory, total jank, total frozen frames.
- **Breaches list:** clicking an entry seeks to it. Breaches also appear as timeline markers with
  `category: 'perf'`, tone `warn`, and a "Performance" filter chip.
  - `high_cpu`: `cpu ≥ 80` for ≥ 3 consecutive samples.
  - `low_fps`: `fps < 0.5 × refresh_hz` (default refresh 60) for ≥ 2 consecutive samples. Samples without
    `fps` break the run.
  - `jank_burst`: one sample with `jank ≥ 5`.
- **Empty state:** "No profiling data for this session. Enable `performance` in the SDK config." A link
  points to the docs page.

## 7. Acceptance
1. At a 1366×768 viewport, the stage, the side panel and the timeline rail are fully inside the viewport, and
   `window.scrollY === 0` after 3 s of playback with Follow on (Playwright harness).
2. A session ingested with 3 perf items (30 samples) returns 30 samples from `/perf`. A 2,000-sample session
   returns 1,200 samples with `downsampled: true`, and a single 99 % CPU spike survives.
3. `perf` items never appear in `telemetry_events`, the log stream or issue grouping.
4. SDKs:
   - With `performance.enabled = false`, no sampler thread or timer is created and no perf items are sent.
   - With it enabled, a 30 s foreground session produces 3 perf items with 30 samples in total, and nothing is
     sampled while in the background.
   - The sampler's own CPU stays ≤ 1 % of one core, measured with the profiler's own `cpu` metric with the
     sampler on versus off.

## 8. Out of scope
Web profiling, server-side alerting on breaches, the session-list filters "High CPU" and "Janky", and
retention or cleanup of `session_perf_batches` (it follows replay retention once that exists).
