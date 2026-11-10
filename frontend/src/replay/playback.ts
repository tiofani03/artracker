import { displayScreenName } from './wireframe'
import type { MobileFrame, ReplayModel, TouchPoint } from './model'

/** A touch ripple is shown while the playhead is within this many ms of the touch. */
export const TOUCH_WINDOW_MS = 600

/** The last frame at or before `ts` (epoch ms); the first frame when `ts` precedes all frames. */
export function activeFrameAt(frames: MobileFrame[], ts: number): MobileFrame | null {
  if (frames.length === 0) return null
  let active = frames[0]
  for (const frame of frames) {
    if (frame.ts > ts) break
    active = frame
  }
  return active
}

export function activeTouchAt(touches: TouchPoint[], ts: number): TouchPoint | null {
  return touches.find((t) => Math.abs(t.ts - ts) <= TOUCH_WINDOW_MS) ?? null
}

/** Human-readable reason shown on the telemetry canvas when there is nothing visual to replay. */
export function fallbackNotice(model: ReplayModel): string | null {
  if (model.kind !== 'none') return null
  switch (model.reason) {
    case 'no-chunks':
      return 'No replay chunks were uploaded for this session.'
    case 'no-visual-data':
      return 'Chunks contain no DOM snapshot or view hierarchy.'
    case 'load-error':
      return model.detail || 'Replay chunks could not be loaded.'
  }
}

/** Playback offset (ms) for a pointer at `clientX` over a track box; clamped to [0, durationMs]. */
export function offsetFromPointer(
  clientX: number,
  track: { left: number; width: number },
  durationMs: number,
): number {
  if (track.width <= 0) return 0
  const ratio = Math.min(1, Math.max(0, (clientX - track.left) / track.width))
  return Math.round(ratio * durationMs)
}

/** Recorded wall-clock time for the device status bar (HH:MM, viewer's local time zone). */
export function deviceClock(ts: number): string | null {
  if (!Number.isFinite(ts)) return null
  const d = new Date(ts)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

/** The route / screen strip above the player: only recorded values, never placeholders. */
export function routeStrip(input: {
  platform: 'web' | 'ios' | 'android'
  screen: string | null
  recordedUrl: string | null
}): { label: string; value: string } {
  const label = input.platform === 'web' ? 'URL' : input.platform === 'ios' ? 'SCREEN' : 'ACTIVITY'
  const raw = input.platform === 'web' ? input.recordedUrl : (input.screen ?? input.recordedUrl)
  return { label, value: raw ? displayScreenName(raw) : 'Not recorded' }
}

/** Which renderer plays the session, for the badge in the player header; null when nothing is visual. */
export function replayModeLabel(model: ReplayModel): 'Screenshot' | 'Wireframe' | 'DOM' | null {
  switch (model.kind) {
    case 'screen':
      return 'Screenshot'
    case 'mobile':
      return 'Wireframe'
    case 'web':
      return model.inferredPlatform === 'android' || model.inferredPlatform === 'ios' ? 'Wireframe' : 'DOM'
    case 'none':
      return null
  }
}
