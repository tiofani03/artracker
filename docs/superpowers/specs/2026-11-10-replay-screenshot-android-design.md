# Mobile Replay Screenshot Mode — Android SDK Design

Date: 2026-11-10 · Repo: `artracker-android-sdk` (`~/AndroidStudioProjects/github/artracker-android-sdk`, minSdk 24)
Sibling designs: `2026-11-10-replay-screenshot-web-design.md` (canonical shared contract),
`2026-11-10-replay-screenshot-ios-design.md`

## 1. Intent

The dashboard must show exactly what the user saw: real fonts, icons, photos, colors, styling and movement.
Today the SDK rebuilds the screen as rrweb DOM (`RrwebEncoder`), which cannot be exact and, as shipped, renders
blank screens and clipped text. The user lifted the "icons only, no photos" rule; images may be sent. Sensitive
content must stay masked on the device.

Decisions by the user:
- New capture mode with real screenshots (OpenReplay's approach), selectable; the current reconstruction stays as
  an option ("as is").
- **Opt-in**: default stays the current mode; screenshot capture must be enabled explicitly, and the project owner
  can switch it off from the server.
- **Data budget: economical, all networks**: ~720 px / JPEG 60 default, target ≤ 5 MB per minute, upload on Wi‑Fi
  and cellular, offline cache ≤ 50 MB.

## 2. Prior art (OpenReplay `android-tracker`, commit `f1295c5`)

`ScreenshotManager.kt`: `PixelCopy` of the Activity window (fallback `view.draw`), 1 fps default, scale to
480/720 px, JPEG 40/50/60, sanitized views painted over with a cross-stripe pattern before compression, frames
gzipped into archives uploaded about every second.

## 3. Public API

```kotlin
public enum class ReplayMode { WIREFRAME, SCREENSHOT }
public enum class ReplayQuality { LOW, STANDARD, HIGH }

public class ReplayConfig @JvmOverloads constructor(
    public val enabled: Boolean = false,
    public val sampleRate: Double = 0.1,
    public val framesPerSecond: Double = 1.0,
    public val mode: ReplayMode = ReplayMode.WIREFRAME,      // opt-in to SCREENSHOT
    public val quality: ReplayQuality = ReplayQuality.STANDARD,
)
```

- The user's idea `isRealScreen: Boolean` is expressed as `mode` (`SCREENSHOT` = real screen, `WIREFRAME` = as is)
  so a later `VIDEO` mode is not an API break.
- `framesPerSecond`: `WIREFRAME` keeps its clamp 0.5…4; `SCREENSHOT` clamps 0.5…10. Recommended 2 for screenshots
  (README).
- Quality presets (short side of the output image / JPEG quality): `LOW` 480 px / 50, `STANDARD` 720 px / 60,
  `HIGH` 1080 px / 75. Never upscale.
- `validate()` errors for non-finite fps stay as today.

## 4. Shared contract (copy of web design §3–§4)

Chunk `artracker-screen-v1` (gzipped JSON, ≤ 2 MB gzipped):
`{ format, platform:"android", seq, frames:[{ts, screen, w, h, vw, vh, mime:"image/jpeg", data:<base64>, masked}], touches:[{ts, x, y, kind}] }`
— `vw/vh` and touch `x/y` in dp; `event_count` in ack = frames + touches.

Switch: `GET /api/v1/replay/config` → `{screenshot_enabled, max_fps}`; `chunk-url` request carries
`"format":"artracker-screen-v1"`; `403 {"error":"screenshot_disabled"}` means stop screenshot capture.

## 5. Design

### 5.1 Mode resolution
- `ReplayController.begin()` resolves the effective mode: config `SCREENSHOT` **and** cached
  `screenshot_enabled == true`. Before the first config answer is cached, the session records in `WIREFRAME`
  and switches to `SCREENSHOT` (with a forced first screenshot) when `GET /replay/config` returns enabled.
  The cache (KeyValueStore) survives restarts; it is refreshed at every replay start.
- `403 screenshot_disabled` from `chunk-url`: cache `false`, delete queued screenshot chunks, switch the running
  session to `WIREFRAME` (forced full snapshot), log once.
- `max_fps` from the server caps `framesPerSecond`.

### 5.2 Capture (`replay/screenshot/ScreenshotCapturer`, main thread start, async completion)
- Trigger: existing `FrameScheduler` model — `OnDrawListener` sets a dirty flag, a tick at the configured fps
  captures only when dirty, `requestSoon()` after touch-up and screen change. Static screens produce no frames.
- Roots from `RootViewLocator.roots()` (activity, dialogs, popups) in z-order:
  - API 26+: `PixelCopy.request(window, bitmap, …)` per window (activity/dialog windows); popup roots without a
    `Window` use `view.draw(canvas)`.
  - API 24–25: `view.draw(canvas)` for every root (documented limitation: no `SurfaceView` content).
  - Composite into one screen-sized bitmap at each root's `getLocationOnScreen`, drawing the dialog dim the
    window applies (`dimAmount`) between layers.
- `SurfaceView` (video, maps, camera), API 26+: `PixelCopy.request(surfaceView, …)` per visible `SurfaceView`,
  composited under the window layer (over it if `setZOrderOnTop`). `TextureView` is already in the window.
- **`FLAG_SECURE` windows**: never captured. The window's area is filled with the mask pattern and the `draw()`
  fallback is never used for it (it would bypass the app's protection). Counted in `masked`.
- **Keyboard (IME)**: drawn by another process; not capturable. When the IME inset is visible
  (`WindowInsetsCompat.Type.ime()`), the inset area is filled with a neutral keyboard placeholder (flat grey with
  a "keyboard" label). Acceptance excludes the keyboard pixels on Android.
- Bitmaps are pooled (two screen-size `ARGB_8888` buffers reused) to avoid GC churn.

### 5.3 Masking
- In the same main-thread pass, collect mask rects in screen coordinates from:
  - `ViewMaskRules` (password/sensitive inputs, `explicitMask`, `sensitiveViewClasses`, `maskAllText`,
    `maskAllInputs`, blocked views incl. WebView by default);
  - Compose: a **mask-only** traversal of the semantics tree (password, sensitive, explicit mask, and all text
    when `maskAllText`) reusing `ComposeWalker`'s rules but skipping style reading.
- Rects are painted with the mask pattern on the composited bitmap **before** scaling/encoding, on the processing
  thread. A frame whose mask collection fails is dropped, never sent unmasked.

### 5.4 Encode, budget, chunk
- Processing thread: scale to the preset → JPEG → base64 → append to the open `artracker-screen-v1` chunk.
- Duplicate suppression: CRC of a 64×64 downscale equal to the previous frame → skip.
- **Byte budget governor**: rolling 60 s window of encoded bytes. Above 5 MB the effective fps is halved (min
  0.5) until the window is back under 80 %; persistent overrun steps quality down one preset for the session.
- Chunk closes at 10 s or when adding a frame would exceed 2 MB gzipped. A single frame that cannot fit is
  re-encoded one preset lower, else dropped and counted.
- Main-thread budget watchdog (existing logic): 16 ms per capture in screenshot mode.

### 5.5 Upload & offline storage
- Reuse `ReplayClient` (chunk-url → PUT → chunk-ack) with the new `format` field and 403 handling.
- **`ReplayStore` change**: chunk bodies move from SQLite BLOBs to files under `noBackupFilesDir/artracker/replay/`;
  the table keeps metadata + file name (DB version 2 migration: existing BLOB rows are written out to files).
  Reason: Android's ~2 MB `CursorWindow` limit makes large BLOBs unreadable.
- Caps: 50 MB total on disk (oldest dropped first), 200 batches. Upload on any network (Wi‑Fi and cellular).

### 5.6 WIREFRAME fixes (mode `WIREFRAME`)
- `RrwebEncoder.mutation`: one `adds` entry per node, pre-order, parent before child, element `node` sent without
  `childNodes`, its text node as its own entry, `nextId` only on the subtree root.
- Text font: `font-family:Roboto,<generic>` instead of the bare generic.
- `overflow:hidden` only when `VisualStyle.clip` is true or the node is a scroll container.

## 6. Acceptance criteria (Android)

1. Unit/Robolectric: chunk JSON matches the contract; masked rects are mask pixels in the decoded JPEG; a
   `FLAG_SECURE` window is fully masked; `403 screenshot_disabled` switches to `WIREFRAME` and clears queued
   screenshot chunks.
2. Instrumented test on a real device (API 26+): a frame of the sample app decoded from the chunk matches a
   `UiAutomation.takeScreenshot()` of the same moment (same scale) within per-channel tolerance 8/255 outside
   masked areas and the IME area.
3. Manual checklist on a real device with the sample app at `HIGH`: fonts, icons, photos, colors, dialogs, bottom
   sheets, system bars match a device screen recording; navigation transitions and scrolling appear as motion at
   4 fps.
4. Budget: 5 minutes of continuous scrolling at `STANDARD`, 2 fps produces ≤ 25 MB of chunks; host app jank
   (`FrameMetrics`, frames > 16 ms) rises by ≤ 2 percentage points versus replay disabled.
5. `WIREFRAME`: a recorded flow plays in the dashboard with zero `Node … not found` warnings and no clipped titles.

## 7. Calls made by Claude (check these)

- Before the first config answer, record in `WIREFRAME` rather than wait or capture blindly.
- `FLAG_SECURE` → masked, never drawn by fallback.
- IME placeholder instead of nothing.
- Preset numbers (480/50, 720/60, 1080/75); governor halving fps then lowering quality.
- File-based chunk storage in `ReplayStore`.
- Instrumented tolerance 8/255 (JPEG 60 vs raw screenshot).

## 8. Left to the builder

Class/file names, mask pattern image, bitmap pool details, governor thresholds' exact hysteresis.
