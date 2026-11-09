import type { eventWithTime } from 'rrweb'
import type { Platform, TelemetryEvent } from '../api'
import type { ReplayModel } from './model'

// rrweb EventType values (kept numeric so this module stays free of rrweb runtime code)
const FULL_SNAPSHOT = 2
const META = 4

function isRrwebEvent(v: unknown): v is eventWithTime {
  return (
    !!v &&
    typeof v === 'object' &&
    typeof (v as { type?: unknown }).type === 'number' &&
    typeof (v as { timestamp?: unknown }).timestamp === 'number'
  )
}

/** Pulls rrweb events out of one chunk payload (bare array or `{ events: [...] }`). */
function rrwebEventsOf(payload: unknown): eventWithTime[] {
  const list = Array.isArray(payload)
    ? payload
    : payload && typeof payload === 'object' && Array.isArray((payload as { events?: unknown }).events)
      ? (payload as { events: unknown[] }).events
      : []
  return list.filter(isRrwebEvent)
}

/**
 * Sorts, de-duplicates and trims rrweb events so playback starts at a Meta event
 * followed by a FullSnapshot. Returns null when nothing replayable remains.
 */
export function normalizeWebEvents(raw: eventWithTime[]): eventWithTime[] | null {
  const seen = new Set<string>()
  const sorted = [...raw]
    .sort((a, b) => a.timestamp - b.timestamp)
    .filter((e) => {
      const key = `${e.type}|${e.timestamp}|${JSON.stringify(e.data)}`
      if (seen.has(key)) return false
      seen.add(key)
      return true
    })

  const fullIdx = sorted.findIndex((e) => e.type === FULL_SNAPSHOT)
  if (fullIdx < 0) return null

  // Start at the Meta closest before the first FullSnapshot; otherwise at the snapshot itself.
  let start = fullIdx
  for (let i = fullIdx - 1; i >= 0; i--) {
    if (sorted[i].type === META) {
      start = i
      break
    }
  }

  const out = sorted.slice(start)
  return out.length >= 2 ? out : null
}

export function buildReplayModel(input: {
  payloads: unknown[]
  timeline: TelemetryEvent[]
  projectPlatform?: Platform
}): ReplayModel {
  const payloads = input.payloads.filter((p) => p != null)

  const web = normalizeWebEvents(payloads.flatMap(rrwebEventsOf))
  if (web) {
    return { kind: 'web', events: web, startMs: web[0].timestamp, endMs: web[web.length - 1].timestamp }
  }

  if (payloads.length === 0) return { kind: 'none', reason: 'no-chunks' }
  return { kind: 'none', reason: 'no-visual-data' }
}
