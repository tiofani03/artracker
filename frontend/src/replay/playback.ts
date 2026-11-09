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
