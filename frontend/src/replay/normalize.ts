import type { eventWithTime } from 'rrweb'
import type { Platform, TelemetryEvent } from '../api'
import type { MobileFrame, ReplayModel, TouchPoint, ViewNode } from './model'

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

export const MOBILE_CHUNK_FORMAT = 'artracker-mobile-v1'

type Rec = Record<string, unknown>
const isRec = (v: unknown): v is Rec => !!v && typeof v === 'object' && !Array.isArray(v)

function asViewTree(v: unknown): ViewNode | null {
  if (typeof v === 'string') {
    try {
      v = JSON.parse(v)
    } catch {
      return null
    }
  }
  return isRec(v) ? (v as ViewNode) : null
}

function asPlatform(v: unknown): 'ios' | 'android' | null {
  const s = String(v ?? '').toLowerCase()
  if (s.includes('android')) return 'android'
  if (s.includes('ios') || s.includes('iphone') || s.includes('ipad')) return 'ios'
  return null
}

function toFrame(ts: number, screen: unknown, attrs: Rec): MobileFrame | null {
  const viewTree = asViewTree(attrs.view_tree ?? attrs.tree)
  if (!viewTree || !Number.isFinite(ts)) return null
  const frame: MobileFrame = { ts, screen: String(screen || 'Screen'), viewTree }
  if (typeof attrs.presentation === 'string') frame.presentation = attrs.presentation
  if (attrs.masked_count != null) frame.maskedCount = Number(attrs.masked_count) || 0
  return frame
}

const TOUCH_NAME = /touch|tap|click|gesture/i

/**
 * Collects mobile view-tree frames and touches from `artracker-mobile-v1` chunk payloads
 * (primary) and from telemetry `attributes.view_tree` (secondary). Chunk frames win
 * when both describe the same `ts` + `screen`.
 */
export function extractMobileFrames(
  payloads: unknown[],
  timeline: TelemetryEvent[],
): { frames: MobileFrame[]; touches: TouchPoint[]; platform: 'ios' | 'android' | null } {
  const frames = new Map<string, MobileFrame>()
  const touches: TouchPoint[] = []
  let chunkPlatform: 'ios' | 'android' | null = null
  let telemetryPlatform: 'ios' | 'android' | null = null

  for (const p of payloads) {
    if (!isRec(p) || p.format !== MOBILE_CHUNK_FORMAT) continue
    chunkPlatform ??= asPlatform(p.platform)
    for (const f of Array.isArray(p.frames) ? p.frames : []) {
      if (!isRec(f)) continue
      const frame = toFrame(Number(f.ts), f.screen, f)
      if (frame) frames.set(`${frame.ts}|${frame.screen}`, frame)
    }
    for (const t of Array.isArray(p.touches) ? p.touches : []) {
      if (!isRec(t)) continue
      const ts = Number(t.ts)
      if (!Number.isFinite(ts)) continue
      const touch: TouchPoint = { ts, x: Number(t.x) || 0, y: Number(t.y) || 0 }
      if (typeof t.name === 'string') touch.name = t.name
      touches.push(touch)
    }
  }

  for (const ev of timeline) {
    const attrs = isRec(ev.attributes) ? ev.attributes : {}
    const ts = new Date(ev.event_timestamp).getTime()
    if (Number.isNaN(ts)) continue
    const frame = toFrame(ts, attrs.screen || ev.name, attrs)
    if (frame) {
      telemetryPlatform ??= asPlatform(attrs.platform)
      const key = `${frame.ts}|${frame.screen}`
      if (!frames.has(key)) frames.set(key, frame)
      continue
    }
    if (TOUCH_NAME.test(ev.name || '') && attrs.x !== undefined && attrs.y !== undefined) {
      touches.push({ ts, x: Number(attrs.x) || 0, y: Number(attrs.y) || 0, name: ev.name })
    }
  }

  return {
    frames: [...frames.values()].sort((a, b) => a.ts - b.ts),
    touches: touches.sort((a, b) => a.ts - b.ts),
    platform: chunkPlatform ?? telemetryPlatform,
  }
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

  const mobile = extractMobileFrames(payloads, input.timeline)
  if (mobile.frames.length > 0) {
    const projectMobile =
      input.projectPlatform === 'ios' || input.projectPlatform === 'android' ? input.projectPlatform : null
    const lastTouch = mobile.touches.length > 0 ? mobile.touches[mobile.touches.length - 1].ts : -Infinity
    return {
      kind: 'mobile',
      platform: mobile.platform ?? projectMobile ?? 'ios',
      frames: mobile.frames,
      touches: mobile.touches,
      startMs: mobile.frames[0].ts,
      endMs: Math.max(mobile.frames[mobile.frames.length - 1].ts, lastTouch),
    }
  }

  if (payloads.length === 0) return { kind: 'none', reason: 'no-chunks' }
  return { kind: 'none', reason: 'no-visual-data' }
}
