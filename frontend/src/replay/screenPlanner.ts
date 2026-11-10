import { activeScreenFrameAt, type ScreenFrameMeta } from './screen'

export const frameKey = (f: ScreenFrameMeta): string => `${f.chunkSeq}:${f.index}`

export type ScreenFramePlan = {
  /** The frame the playhead is on (greatest ts <= playhead); null before the first frame. */
  due: ScreenFrameMeta | null
  /** What to draw: the due frame once decoded, else the latest decoded earlier frame; never a later frame. */
  show: ScreenFrameMeta | null
  /** True while the due frame is not decoded or the playhead lies in a region whose chunks are not loaded. */
  buffering: boolean
  /** Undecoded frames after `due` up to `aheadMs` past the playhead, ascending. */
  prefetch: ScreenFrameMeta[]
}

/**
 * Pure decision of what the screen renderer draws and decodes next. `frames` must be sorted by ts.
 * `resolved` is `ScreenIndex.isResolved(playhead)`.
 */
export function planScreenFrame(input: {
  frames: ScreenFrameMeta[]
  playheadMs: number
  decoded: ReadonlySet<string>
  resolved: boolean
  aheadMs?: number
}): ScreenFramePlan {
  const { frames, playheadMs, decoded, resolved, aheadMs = 2_000 } = input
  const due = activeScreenFrameAt(frames, playheadMs)

  let show: ScreenFrameMeta | null = null
  if (due) {
    if (decoded.has(frameKey(due))) {
      show = due
    } else {
      for (let i = frames.indexOf(due) - 1; i >= 0; i--) {
        if (decoded.has(frameKey(frames[i]))) {
          show = frames[i]
          break
        }
      }
    }
  }

  const lower = due ? due.ts : -Infinity
  const upper = playheadMs + aheadMs
  const prefetch = frames.filter((f) => f.ts > lower && f.ts <= upper && !decoded.has(frameKey(f)))

  return { due, show, buffering: !resolved || (due !== null && !decoded.has(frameKey(due))), prefetch }
}
