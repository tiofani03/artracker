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

const INCREMENTAL_SNAPSHOT = 3
const SOURCE_MUTATION = 0

type FlatAdd = { parentId: number; nextId: number | null; node: Record<string, unknown> }

/**
 * rrweb's replayer builds a node from a mutation `adds` entry with `skipChild: true`, so only the entry's own
 * element is created and any `childNodes` nested inside it are dropped (later mutations then fail with
 * "Node with id … not found"). The Android SDK nests whole subtrees, so each nested add becomes one flat
 * entry per node, pre-order (a parent always precedes its children). Only the subtree root keeps `nextId`;
 * descendants append in order. Events without nested adds are returned as the same object.
 */
export function flattenNestedAdds(events: eventWithTime[]): eventWithTime[] {
  return events.map((e) => {
    if (e.type !== INCREMENTAL_SNAPSHOT) return e
    const data: unknown = e.data
    if (!isRec(data) || data.source !== SOURCE_MUTATION) return e
    const adds: unknown = data.adds
    if (!Array.isArray(adds)) return e
    const nested = adds.some(
      (a) => isRec(a) && isRec(a.node) && Array.isArray(a.node.childNodes) && a.node.childNodes.length > 0,
    )
    if (!nested) return e

    const flat: FlatAdd[] = []
    const visit = (node: Record<string, unknown>, parentId: number, nextId: number | null) => {
      const kids = Array.isArray(node.childNodes) ? (node.childNodes as unknown[]) : []
      const entry: Record<string, unknown> = { ...node }
      if (node.type === 2 || Array.isArray(node.childNodes)) entry.childNodes = []
      flat.push({ parentId, nextId, node: entry })
      for (const kid of kids) {
        if (isRec(kid)) visit(kid, Number(node.id), null)
      }
    }
    for (const a of adds) {
      if (!isRec(a) || !isRec(a.node)) continue
      visit(a.node, Number(a.parentId), a.nextId == null ? null : Number(a.nextId))
    }
    return { ...e, data: { ...data, adds: flat } } as eventWithTime
  })
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

  const out = flattenNestedAdds(sorted.slice(start))
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
 * when both describe the same `ts` + `screen`, and telemetry frames inside the time span covered
 * by chunk frames are dropped.
 */
type RrwebNode = {
  type: number
  id: number
  tagName?: string
  attributes?: Record<string, unknown>
  childNodes?: RrwebNode[]
  textContent?: string
  [key: string]: unknown
}

const WIREFRAME_CSS = `
html, body {
  margin: 0 !important;
  padding: 0 !important;
  font-family: Roboto, -apple-system, BlinkMacSystemFont, 'SF Pro Text', sans-serif !important;
  font-size: 12px !important;
  line-height: 1.25 !important;
  color: #0f172a !important;
  background: #fafafa !important;
  -webkit-font-smoothing: antialiased !important;
}
[data-artracker-type="CONTAINER"] {
  box-sizing: border-box !important;
}
[data-artracker-type="TEXT"] {
  display: flex !important;
  align-items: center !important;
  font-family: Roboto, -apple-system, BlinkMacSystemFont, 'SF Pro Text', sans-serif !important;
  font-size: 11px !important;
  line-height: 1.15 !important;
  letter-spacing: -0.01em !important;
  color: #1e293b !important;
  overflow: visible !important;
  white-space: nowrap !important;
}
[data-artracker-type="BUTTON"] {
  background: rgba(0, 122, 255, 0.10) !important;
  border: 1px solid rgba(0, 122, 255, 0.30) !important;
  border-radius: 8px !important;
  box-sizing: border-box !important;
}
[data-artracker-type="BUTTON"] [data-artracker-type="TEXT"] {
  color: #0066dc !important;
  font-weight: 600 !important;
  font-size: 10.5px !important;
  justify-content: center !important;
}
[data-artracker-type="IMAGE"] {
  background: rgba(148, 163, 184, 0.18) !important;
  border: 1px dashed rgba(100, 116, 139, 0.35) !important;
  border-radius: 6px !important;
  box-sizing: border-box !important;
}
`

function parseBox(style: string) {
  const left = style.match(/(?:^|;)\s*left:\s*(-?\d+)px/)?.[1]
  const top = style.match(/(?:^|;)\s*top:\s*(-?\d+)px/)?.[1]
  const width = style.match(/(?:^|;)\s*width:\s*(-?\d+)px/)?.[1]
  const height = style.match(/(?:^|;)\s*height:\s*(-?\d+)px/)?.[1]
  return {
    left: left != null ? parseInt(left, 10) : undefined,
    top: top != null ? parseInt(top, 10) : undefined,
    width: width != null ? parseInt(width, 10) : undefined,
    height: height != null ? parseInt(height, 10) : undefined,
  }
}

/**
 * Checks whether the serialized DOM tree contains synthetic mobile markers (e.g. data-artracker-type).
 */
function hasArtrackerMarkers(node: RrwebNode): boolean {
  if (node.attributes && 'data-artracker-type' in node.attributes) return true
  if (Array.isArray(node.childNodes)) {
    for (const c of node.childNodes) {
      if (hasArtrackerMarkers(c)) return true
    }
  }
  return false
}

/**
 * Enhances synthetic mobile snapshots by injecting wireframe CSS, styling containers semantically,
 * and eliminating double-screen ghosting during compose transitions.
 */
export function enhanceMobileSyntheticEvents(
  events: eventWithTime[],
  inferredPlatform?: 'android' | 'ios' | 'web',
): eventWithTime[] {
  const isMobile = inferredPlatform === 'android' || inferredPlatform === 'ios'
  const hasSynthetic = events.some((e) => {
    if (e.type === FULL_SNAPSHOT && isRec(e.data) && isRec(e.data.node)) {
      return hasArtrackerMarkers(e.data.node as RrwebNode)
    }
    return false
  })

  if (!isMobile && !hasSynthetic) return events

  return events.map((ev) => {
    if (ev.type !== FULL_SNAPSHOT || !isRec(ev.data) || !isRec(ev.data.node)) {
      return ev
    }

    // Deep clone root node to avoid mutating original
    const root = JSON.parse(JSON.stringify(ev.data.node)) as RrwebNode
    let maxId = 1
    function scanIds(n: RrwebNode) {
      if (typeof n.id === 'number' && n.id > maxId) maxId = n.id
      n.childNodes?.forEach(scanIds)
    }
    scanIds(root)

    // Locate or create <head>
    let head: RrwebNode | null = null
    let html: RrwebNode | null = null

    function findTags(n: RrwebNode) {
      if (n.type === 2) {
        const tag = n.tagName?.toLowerCase()
        if (tag === 'head') head = n
        if (tag === 'html') html = n
      }
      n.childNodes?.forEach(findTags)
    }
    findTags(root)

    const textChild: RrwebNode = {
      type: 3,
      id: ++maxId,
      textContent: WIREFRAME_CSS,
    }
    const styleElem: RrwebNode = {
      type: 2,
      id: ++maxId,
      tagName: 'style',
      attributes: { 'data-artracker-injected': 'true' },
      childNodes: [textChild],
    }

    if (head) {
      const h = head as RrwebNode
      h.childNodes = h.childNodes || []
      const hasStyle = h.childNodes.some(
        (c) => c.type === 2 && c.tagName?.toLowerCase() === 'style' && c.attributes?.['data-artracker-injected'],
      )
      if (!hasStyle) {
        h.childNodes.unshift(styleElem)
      }
    } else if (html) {
      const ht = html as RrwebNode
      const newHead: RrwebNode = {
        type: 2,
        id: ++maxId,
        tagName: 'head',
        attributes: {},
        childNodes: [styleElem],
      }
      ht.childNodes = ht.childNodes || []
      ht.childNodes.unshift(newHead)
    }

    // Process nodes for styling and ghosting elimination
    function processNode(node: RrwebNode) {
      if (node.type === 2 && node.attributes) {
        const trackerType = node.attributes['data-artracker-type']
        let style = typeof node.attributes.style === 'string' ? node.attributes.style : ''
        const box = parseBox(style)

        if (trackerType === 'CONTAINER' && box.width != null && box.height != null) {
          if (box.left === 0 && box.top != null && box.top <= 60 && box.height <= 64 && box.width >= 300) {
            style += ';background-color:#ffffff;border-bottom:1px solid #e2e8f0;'
          } else if (box.top != null && box.top >= 560 && box.width >= 300) {
            style += ';background-color:#ffffff;border-top:1px solid #e2e8f0;'
          } else if (box.width >= 250 && box.width <= 315 && box.height >= 48 && box.height < 350 && box.top != null && box.top > 64 && box.top < 560) {
            style += ';background-color:#ffffff;border:1px solid #e2e8f0;border-radius:12px;box-shadow:0 1px 2px rgba(15,23,42,0.04);'
          } else if (box.width === 64 && box.height === 32) {
            style += ';background-color:rgba(0,122,255,0.12);border-radius:16px;'
          } else if (box.width === 24 && box.height === 24 && (!node.childNodes || node.childNodes.length === 0)) {
            style += ';background-color:rgba(100,116,139,0.15);border-radius:4px;'
          }
          node.attributes.style = style
        }
      }

      // Ghosting elimination: check if multiple large container children overlap
      if (node.childNodes && node.childNodes.length > 1) {
        const largeContainers: { child: RrwebNode; box: ReturnType<typeof parseBox> }[] = []
        node.childNodes.forEach((child) => {
          if (child.type === 2 && child.attributes) {
            const style = typeof child.attributes.style === 'string' ? child.attributes.style : ''
            const box = parseBox(style)
            if (box.width != null && box.width >= 300 && box.height != null && box.height >= 350) {
              largeContainers.push({ child, box })
            }
          }
        })

        if (largeContainers.length > 1) {
          for (let i = 0; i < largeContainers.length - 1; i++) {
            const prev = largeContainers[i]
            const next = largeContainers[largeContainers.length - 1]
            if (
              prev.box.top != null &&
              next.box.top != null &&
              Math.abs(prev.box.top - next.box.top) < 30
            ) {
              // Hide exiting screen
              const prevAttrs = prev.child.attributes || {}
              let prevStyle = typeof prevAttrs.style === 'string' ? prevAttrs.style : ''
              prevStyle += ';display:none !important;'
              prevAttrs.style = prevStyle
              prev.child.attributes = prevAttrs

              // Ensure entering screen is opaque
              const nextAttrs = next.child.attributes || {}
              let nextStyle = typeof nextAttrs.style === 'string' ? nextAttrs.style : ''
              nextStyle += ';background-color:#fafafa !important;z-index:2 !important;'
              nextAttrs.style = nextStyle
              next.child.attributes = nextAttrs
            }
          }
        }
      }

      node.childNodes?.forEach(processNode)
    }

    processNode(root)

    return {
      ...ev,
      data: {
        ...(ev.data as Record<string, unknown>),
        node: root,
      },
    } as eventWithTime
  })
}

export function extractMobileFrames(
  payloads: unknown[],
  timeline: TelemetryEvent[],
): { frames: MobileFrame[]; touches: TouchPoint[]; platform: 'ios' | 'android' | null } {
  const frames = new Map<string, MobileFrame>()
  const touches: TouchPoint[] = []
  let chunkPlatform: 'ios' | 'android' | null = null
  let telemetryPlatform: 'ios' | 'android' | null = null

  for (const p of payloads) {
    if (!isRec(p)) continue
    const isMobilePayload =
      p.format === MOBILE_CHUNK_FORMAT ||
      (!p.format && (p.platform === 'ios' || p.platform === 'android') && Array.isArray(p.frames))
    if (!isMobilePayload) continue
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

  // Telemetry timestamps are clock-corrected at ingest, so they never line up exactly with raw
  // device timestamps in chunks. Chunks are authoritative for the span they cover.
  const chunkTs = [...frames.values()].map((f) => f.ts)
  const covered =
    chunkTs.length > 0
      ? (ts: number) => ts >= Math.min(...chunkTs) && ts <= Math.max(...chunkTs)
      : () => false

  for (const ev of timeline) {
    const attrs = isRec(ev.attributes) ? ev.attributes : {}
    const ts = new Date(ev.event_timestamp).getTime()
    if (Number.isNaN(ts)) continue
    const frame = toFrame(ts, attrs.screen || ev.name, attrs)
    if (frame) {
      telemetryPlatform ??= asPlatform(attrs.platform)
      const key = `${frame.ts}|${frame.screen}`
      if (!covered(frame.ts) && !frames.has(key)) frames.set(key, frame)
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
    let inferredPlatform: 'android' | 'ios' | 'web' | undefined
    let viewport: { width: number; height: number } | undefined

    for (const ev of web) {
      if (ev.type === META) {
        const d = ev.data as { href?: string; width?: number; height?: number }
        if (d?.href) {
          if (d.href.startsWith('android://')) inferredPlatform = 'android'
          else if (d.href.startsWith('ios://')) inferredPlatform = 'ios'
        }
        if (typeof d?.width === 'number' && typeof d?.height === 'number' && !viewport) {
          viewport = { width: d.width, height: d.height }
        }
      }
    }

    if (!inferredPlatform) {
      if (input.projectPlatform === 'android') inferredPlatform = 'android'
      else if (input.projectPlatform === 'ios') inferredPlatform = 'ios'
    }

    const enhanced = enhanceMobileSyntheticEvents(web, inferredPlatform)

    return {
      kind: 'web',
      events: enhanced,
      startMs: enhanced[0].timestamp,
      endMs: enhanced[enhanced.length - 1].timestamp,
      inferredPlatform: inferredPlatform ?? 'web',
      viewport,
    }
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
