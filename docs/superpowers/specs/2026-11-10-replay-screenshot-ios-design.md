# Mobile Replay Screenshot Mode — iOS SDK Design

Date: 2026-11-10 · Repo: `artracker-ios-sdk` (`~/AndroidStudioProjects/github/artracker-ios-sdk`)
Sibling designs: `2026-11-10-replay-screenshot-web-design.md` (canonical shared contract),
`2026-11-10-replay-screenshot-android-design.md`

## 1. Intent

The dashboard must show exactly what the user saw: real fonts, icons, photos, colors, styling and movement.
Today `ReplayEngine` sends `artracker-mobile-v1` (class name + frame + text, no color) and the dashboard draws a
grey wireframe; SwiftUI content inside `_UIHostingView` is invisible. Images may be sent (rule lifted by the
user). Sensitive content must stay masked on the device.

Decisions by the user:
- New screenshot capture mode, selectable; the current wireframe stays as an option ("as is").
- **Opt-in** plus a server-side switch for the project owner.
- **Data budget: economical, all networks**: ~720 px / JPEG 0.6 default, target ≤ 5 MB per minute, Wi‑Fi and
  cellular, offline cache ≤ 50 MB.
- **SwiftUI masking**: a public modifier `.artrackerMask()`; `SecureField` masked automatically; with
  `maskAllText` on, every SwiftUI hosting area is masked.

## 2. Public API

```swift
public enum ReplayMode: String, Sendable { case wireframe, screenshot }
public enum ReplayQuality: String, Sendable { case low, standard, high }

// ARTrackerConfig: new stored properties + init params
public var replayMode: ReplayMode = .wireframe          // opt-in to .screenshot
public var replayQuality: ReplayQuality = .standard
public var replayFramesPerSecond: Double = 2.0          // clamped 0.5...10 in screenshot mode

// SwiftUI
extension View { public func artrackerMask() -> some View }
// UIKit (existing convention kept): accessibilityIdentifier prefix "mask:"; new: UIView.artrackerMasked = true
```

Quality presets (short side px / JPEG quality): `low` 480 / 0.5, `standard` 720 / 0.6, `high` 1080 / 0.75.
Never upscale. The user's idea `isRealScreen` is expressed as `replayMode`.

## 3. Shared contract (copy of web design §3–§4)

Chunk `artracker-screen-v1` (gzipped JSON, ≤ 2 MB gzipped):
`{ format, platform:"ios", seq, frames:[{ts, screen, w, h, vw, vh, mime:"image/jpeg", data:<base64>, masked}], touches:[{ts, x, y, kind}] }`
— `vw/vh` and touch `x/y` in points; `event_count` in ack = frames + touches.

Switch: `GET /api/v1/replay/config` → `{screenshot_enabled, max_fps}`; `chunk-url` request carries
`"format":"artracker-screen-v1"`; `403 {"error":"screenshot_disabled"}` means stop screenshot capture.

## 4. Design

### 4.1 Mode resolution
- `ReplayEngine.start` gets `mode`, `quality`, `fps`. Effective mode = `.screenshot` only when configured **and**
  the cached `screenshot_enabled` is true (UserDefaults, refreshed at every start). Until the first answer the
  session records `.wireframe`, then switches with a forced first screenshot.
- `403 screenshot_disabled`: cache false, delete queued screenshot chunks, continue as `.wireframe`.
- `max_fps` caps the configured fps.

### 4.2 Capture (`Replay/ScreenCapturer.swift`, `@MainActor`)
- Trigger: `CADisplayLink`-free timer at the configured fps while the app is active, plus existing triggers
  (`touch.debounce` 300 ms, `window.didBecomeKey`, screen change, session start). iOS has no cheap "did draw"
  signal, so duplicate suppression (CRC of a 64×64 downscale) provides "only on change".
- Windows: all visible windows of foreground-active `UIWindowScene`s, sorted by `windowLevel` (app, alerts,
  keyboard). Each drawn with `drawHierarchy(in:afterScreenUpdates: false)` into one `UIGraphicsImageRenderer`
  context at the preset scale. This includes SwiftUI content.
- **Keyboard window**: captured, except while the first responder is masked (secure entry, sensitive content
  type, `mask:`/`artrackerMasked`, `.artrackerMask()` region, or `maskAllText`): then the keyboard window's area
  is filled with the mask pattern, because key-press pop-ups would reveal typed characters.
- Watchdog: a capture over 16 ms of main-thread time three times in a row halves fps (min 0.5); restored after 10 s
  without a slow capture.

### 4.3 Masking
- Rects in window coordinates, painted into the same render context **before** encoding:
  - UIKit: `MaskingRules.shouldMask` walk (secure entry, password/OTP/card content types, `mask:` id,
    `artrackerMasked`, `maskAllText` for `UILabel`/`UITextField`/`UITextView`, `WKWebView`).
  - SwiftUI: `.artrackerMask()` attaches a zero-cost background `GeometryReader` that registers its global frame
    in a main-actor `MaskRegistry` (registered on appear/change, removed on disappear). `SecureField` is found
    through its backing `UITextField` (`isSecureTextEntry`).
  - `maskAllText` on: every `_UIHostingView` (class-name match) frame is masked entirely, since SwiftUI text
    cannot be located individually.
- A frame whose mask collection throws is dropped, never sent unmasked.

### 4.4 Encode, budget, chunk
- Background queue: `jpegData(compressionQuality:)` → base64 → append to an `artracker-screen-v1` chunk.
- Same byte budget governor as Android: rolling 60 s window, over 5 MB halves fps (min 0.5); persistent overrun
  steps quality down one preset.
- `ReplayChunkManager`: new screen-chunk buffer alongside the existing wireframe buffer; closes at 10 s or 2 MB
  gzipped; oversized single frame re-encoded one preset lower, else dropped.

### 4.5 Upload & offline storage
- `ReplayUploader.requestChunkUrl` adds `format`; handles 403 `screenshot_disabled` and `Retry-After`.
- Pending uploads move from the in-memory `pendingUploadQueue` to files in
  `Library/Caches/ARTracker/replay/` so they survive app termination; cap 50 MB (oldest dropped). Wi‑Fi and
  cellular; existing `wifiOnly` config still applies when the app sets it.

### 4.6 WIREFRAME
Unchanged (`artracker-mobile-v1`).

## 5. Acceptance criteria (iOS)

1. XCTest: chunk JSON matches the contract; masked UIKit views, `.artrackerMask()` views and `SecureField` are
   mask pixels in the decoded JPEG; with `maskAllText` the hosting view area is masked; keyboard area masked while
   a secure field is focused; `403 screenshot_disabled` falls back to wireframe and clears queued screenshot chunks.
2. UI test on a real iPhone with the SwiftUI and UIKit samples: a decoded frame matches
   `XCUIScreen.main.screenshot()` of the same moment (same scale) within per-channel tolerance 8/255 outside masked
   areas.
3. Manual checklist on a real iPhone at `high`: fonts, SF Symbols, photos, colors, alerts, sheets, keyboard
   (non-sensitive fields), status bar match a screen recording; transitions and scrolling move at 4 fps.
4. Budget: 5 minutes of continuous scrolling at `standard`, 2 fps produces ≤ 25 MB of chunks; hitches measured
   with `CADisplayLink` rise by ≤ 2 percentage points versus replay disabled.

## 6. Calls made by Claude (check these)

- Record `.wireframe` until the server switch answer arrives.
- Keyboard captured but masked while a sensitive field is focused.
- `maskAllText` masks whole SwiftUI hosting areas.
- Preset numbers; governor behavior; file-based pending queue in Caches.
- Default fps 2 for iOS (Android keeps its existing 1.0 default for `WIREFRAME`; README recommends 2 for screenshots).

## 7. Left to the builder

File layout, mask pattern image, `MaskRegistry` internals, timer implementation.
