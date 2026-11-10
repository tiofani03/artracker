import { TOUCH_WINDOW_MS } from './playback'

/** Chunk format of screenshot-mode mobile replay (see the web design doc, section 3). */
export const SCREEN_CHUNK_FORMAT = 'artracker-screen-v1'

/** One captured screen image. Metadata only: the base64 image stays in its chunk payload. */
export type ScreenFrameMeta = {
  ts: number
  screen: string
  /** Image size in px. */
  w: number
  h: number
  /** Logical screen size (dp / pt) the image covers; touch coordinates use this space. */
  vw: number
  vh: number
  mime: 'image/jpeg'
  chunkSeq: number
  /** Position of the frame in its chunk's `frames` array (for `screenFrameData`). */
  index: number
  masked: number
}

export type ScreenTouch = { ts: number; x: number; y: number; kind: 'down' | 'move' | 'up' }

export type ParsedScreenChunk = {
  seq: number
  platform: 'android' | 'ios' | null
  frames: ScreenFrameMeta[]
  touches: ScreenTouch[]
}

type Rec = Record<string, unknown>
const isRec = (v: unknown): v is Rec => !!v && typeof v === 'object' && !Array.isArray(v)
const positive = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v > 0
const TOUCH_KINDS = new Set<ScreenTouch['kind']>(['down', 'move', 'up'])

export function isScreenPayload(v: unknown): boolean {
  return isRec(v) && v.format === SCREEN_CHUNK_FORMAT
}

/** A session is a screen session when any probed chunk (first / last) is a screen chunk. */
export function isScreenSession(probes: unknown[]): boolean {
  return probes.some(isScreenPayload)
}

function validFrame(f: unknown): f is Rec & { ts: number; data: string } {
  return (
    isRec(f) &&
    typeof f.ts === 'number' &&
    Number.isFinite(f.ts) &&
    positive(f.w) &&
    positive(f.h) &&
    positive(f.vw) &&
    positive(f.vh) &&
    f.mime === 'image/jpeg' &&
    typeof f.data === 'string' &&
    f.data.length > 0
  )
}

function asPlatform(v: unknown): 'android' | 'ios' | null {
  const s = String(v ?? '').toLowerCase()
  if (s === 'android') return 'android'
  if (s === 'ios') return 'ios'
  return null
}

/**
 * Reads the frame / touch metadata of one `artracker-screen-v1` chunk. Invalid frames and touches are
 * skipped; returns null for other formats and for chunks with nothing usable.
 */
export function parseScreenChunk(payload: unknown, seq: number): ParsedScreenChunk | null {
  if (!isRec(payload) || payload.format !== SCREEN_CHUNK_FORMAT) return null

  const frames: ScreenFrameMeta[] = []
  const rawFrames = Array.isArray(payload.frames) ? payload.frames : []
  rawFrames.forEach((f, index) => {
    if (!validFrame(f)) return
    frames.push({
      ts: f.ts,
      screen: typeof f.screen === 'string' && f.screen ? f.screen : 'Screen',
      w: f.w as number,
      h: f.h as number,
      vw: f.vw as number,
      vh: f.vh as number,
      mime: 'image/jpeg',
      chunkSeq: seq,
      index,
      masked: typeof f.masked === 'number' && Number.isFinite(f.masked) ? f.masked : 0,
    })
  })
  frames.sort((a, b) => a.ts - b.ts)

  const touches: ScreenTouch[] = []
  for (const t of Array.isArray(payload.touches) ? payload.touches : []) {
    if (!isRec(t)) continue
    const { ts, x, y, kind } = t
    if (typeof ts !== 'number' || !Number.isFinite(ts)) continue
    if (typeof x !== 'number' || !Number.isFinite(x) || typeof y !== 'number' || !Number.isFinite(y)) continue
    if (!TOUCH_KINDS.has(kind as ScreenTouch['kind'])) continue
    touches.push({ ts, x, y, kind: kind as ScreenTouch['kind'] })
  }
  touches.sort((a, b) => a.ts - b.ts)

  if (frames.length === 0 && touches.length === 0) return null
  return { seq, platform: asPlatform(payload.platform), frames, touches }
}

/** The base64 image of `frames[index]` in a screen payload, or null when missing or invalid. */
export function screenFrameData(payload: unknown, index: number): string | null {
  if (!isRec(payload) || payload.format !== SCREEN_CHUNK_FORMAT || !Array.isArray(payload.frames)) return null
  const f = payload.frames[index]
  return validFrame(f) ? f.data : null
}

/** The frame with the greatest `ts <= ts`; null before the first frame. `frames` must be sorted by ts. */
export function activeScreenFrameAt(frames: ScreenFrameMeta[], ts: number): ScreenFrameMeta | null {
  let lo = 0
  let hi = frames.length - 1
  let found = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (frames[mid].ts <= ts) {
      found = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return found < 0 ? null : frames[found]
}

/**
 * Where to draw the touch ripple at `ts`, in canvas pixels. Between two samples of one gesture the position
 * is interpolated; otherwise the nearest touch within TOUCH_WINDOW_MS is used. `touches` must be sorted by ts.
 */
export function touchMarkerAt(
  touches: ScreenTouch[],
  ts: number,
  view: { vw: number; vh: number },
  canvas: { width: number; height: number },
): { x: number; y: number; kind: ScreenTouch['kind'] } | null {
  if (touches.length === 0 || view.vw <= 0 || view.vh <= 0) return null
  const sx = canvas.width / view.vw
  const sy = canvas.height / view.vh

  let prev: ScreenTouch | null = null
  let next: ScreenTouch | null = null
  for (const t of touches) {
    if (t.ts <= ts) prev = t
    else {
      next = t
      break
    }
  }

  if (prev && next && prev.kind !== 'up' && next.kind !== 'down' && next.ts - prev.ts <= TOUCH_WINDOW_MS) {
    const k = (ts - prev.ts) / (next.ts - prev.ts)
    return {
      x: (prev.x + (next.x - prev.x) * k) * sx,
      y: (prev.y + (next.y - prev.y) * k) * sy,
      kind: 'move',
    }
  }

  const dPrev = prev ? ts - prev.ts : Infinity
  const dNext = next ? next.ts - ts : Infinity
  const nearest = dPrev <= dNext ? prev : next
  if (!nearest || Math.min(dPrev, dNext) > TOUCH_WINDOW_MS) return null
  return { x: nearest.x * sx, y: nearest.y * sy, kind: nearest.kind }
}
