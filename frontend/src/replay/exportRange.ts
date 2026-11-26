import { EventType, type eventWithTime } from 'rrweb'
import type { SessionPerfResponse, TelemetryEvent } from '../api'
import type { MobileFrame, TouchPoint } from './model'
import type { ScreenFrameMeta, ScreenTouch } from './screen'

export type ResolvedRange = {
  startAbsMs: number
  endAbsMs: number
  startOffsetMs: number
  endOffsetMs: number
}

/**
 * Normalizes user-specified clip offsets against session duration and start timestamp.
 */
export function resolveExportRange(
  baseTimestampMs: number,
  durationMs: number,
  clipStartMs: number,
  clipEndMs: number,
): ResolvedRange {
  const dur = Math.max(0, durationMs)
  const sOffset = Math.max(0, Math.min(clipStartMs, dur))
  const eOffset = Math.max(sOffset, Math.min(clipEndMs, dur))

  return {
    startAbsMs: baseTimestampMs + sOffset,
    endAbsMs: baseTimestampMs + eOffset,
    startOffsetMs: sOffset,
    endOffsetMs: eOffset,
  }
}

/**
 * Slices timeline events that fall within [startAbsMs, endAbsMs].
 */
export function sliceTimelineEvents(
  events: TelemetryEvent[],
  startAbsMs: number,
  endAbsMs: number,
): TelemetryEvent[] {
  return events.filter((ev) => {
    const ts = Date.parse(ev.event_timestamp)
    if (Number.isNaN(ts)) return true
    return ts >= startAbsMs && ts <= endAbsMs
  })
}

/**
 * Slices performance samples within [startAbsMs, endAbsMs].
 */
export function slicePerfSamples(
  perf: SessionPerfResponse | null | undefined,
  startAbsMs: number,
  endAbsMs: number,
): SessionPerfResponse | null {
  if (!perf) return null

  return {
    ...perf,
    samples: (perf.samples || []).filter((s) => s.t >= startAbsMs && s.t <= endAbsMs),
  }
}

/**
 * Slices mobile screenshot frames & touches.
 * Captures the last frame at or before startAbsMs so playback does not start on a blank frame.
 */
export function sliceScreenData(
  frames: ScreenFrameMeta[],
  touches: ScreenTouch[],
  startAbsMs: number,
  endAbsMs: number,
): { frames: ScreenFrameMeta[]; touches: ScreenTouch[] } {
  if (frames.length === 0) {
    return {
      frames: [],
      touches: touches.filter((t) => t.ts >= startAbsMs && t.ts <= endAbsMs),
    }
  }

  // Find last frame <= startAbsMs
  let baselineFrame: ScreenFrameMeta | null = null
  for (const f of frames) {
    if (f.ts <= startAbsMs) {
      if (!baselineFrame || f.ts >= baselineFrame.ts) {
        baselineFrame = f
      }
    }
  }

  const insideFrames = frames.filter((f) => f.ts > startAbsMs && f.ts <= endAbsMs)
  const slicedFrames: ScreenFrameMeta[] = []
  if (baselineFrame) {
    slicedFrames.push(baselineFrame)
  }
  slicedFrames.push(...insideFrames)

  // Ensure unique frames by (chunkSeq, index)
  const seen = new Set<string>()
  const dedupedFrames = slicedFrames.filter((f) => {
    const key = `${f.chunkSeq}:${f.index}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })

  const slicedTouches = touches.filter((t) => t.ts >= startAbsMs && t.ts <= endAbsMs)

  return { frames: dedupedFrames, touches: slicedTouches }
}

/**
 * Slices mobile wireframe frames & touches.
 * Captures the last frame at or before startAbsMs as the baseline state.
 */
export function sliceMobileData(
  frames: MobileFrame[],
  touches: TouchPoint[],
  startAbsMs: number,
  endAbsMs: number,
): { frames: MobileFrame[]; touches: TouchPoint[] } {
  if (frames.length === 0) {
    return {
      frames: [],
      touches: touches.filter((t) => t.ts >= startAbsMs && t.ts <= endAbsMs),
    }
  }

  let baselineFrame: MobileFrame | null = null
  for (const f of frames) {
    if (f.ts <= startAbsMs) {
      if (!baselineFrame || f.ts >= baselineFrame.ts) {
        baselineFrame = f
      }
    }
  }

  const insideFrames = frames.filter((f) => f.ts > startAbsMs && f.ts <= endAbsMs)
  const slicedFrames: MobileFrame[] = []
  if (baselineFrame) {
    slicedFrames.push(baselineFrame)
  }
  slicedFrames.push(...insideFrames)

  const slicedTouches = touches.filter((t) => t.ts >= startAbsMs && t.ts <= endAbsMs)
  return { frames: slicedFrames, touches: slicedTouches }
}

/**
 * Slices web rrweb events, preserving the preceding Meta and FullSnapshot so the DOM tree can reconstruct cleanly.
 */
export function sliceWebEventsWithBaseline(
  events: eventWithTime[],
  startAbsMs: number,
  endAbsMs: number,
): eventWithTime[] {
  if (events.length === 0) return []

  // If startAbsMs is at or before first event, simply filter up to endAbsMs
  if (startAbsMs <= events[0].timestamp) {
    return events.filter((e) => e.timestamp <= endAbsMs)
  }

  // Find the last FullSnapshot at or before startAbsMs
  let lastFullSnapshotIdx = -1
  for (let i = 0; i < events.length; i++) {
    const ev = events[i]
    if (ev.timestamp > startAbsMs) break
    if (ev.type === EventType.FullSnapshot) {
      lastFullSnapshotIdx = i
    }
  }

  // If no preceding FullSnapshot exists, take from index 0
  const startIdx = lastFullSnapshotIdx >= 0 ? lastFullSnapshotIdx : 0

  // Also look for the latest Meta event preceding startIdx if any
  let metaEvent: eventWithTime | null = null
  for (let i = 0; i < startIdx; i++) {
    if (events[i].type === EventType.Meta) {
      metaEvent = events[i]
    }
  }

  const result: eventWithTime[] = []
  if (metaEvent && (events[startIdx] === undefined || events[startIdx].type !== EventType.Meta)) {
    result.push(metaEvent)
  }

  for (let i = startIdx; i < events.length; i++) {
    const ev = events[i]
    if (ev.timestamp > endAbsMs) break
    result.push(ev)
  }

  return result
}
