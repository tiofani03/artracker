export type TrackId = 'screens' | 'network' | 'cpu' | 'mem' | 'fps' | 'events'
export type TrackKind = 'spans' | 'series' | 'markers'

/** A point of a series track; `tMs` is the offset from the session start. */
export type SeriesPoint = { tMs: number; v: number }
export type Span = { startMs: number; endMs: number; label: string; tone?: 'error' }
export type TrackMarker = { id: string; offsetMs: number; tone: 'error' | 'warn' | 'info'; label: string }

/** A stretch of the timeline highlighted on a series track (e.g. a CPU breach). Offsets are from session start. */
export type TrackBand = { startMs: number; endMs: number }

export type TrackDef = {
  id: TrackId
  label: string
  kind: TrackKind
  spans?: Span[]
  points?: SeriesPoint[]
  markers?: TrackMarker[]
  /** Value that maps to the top of a series track. */
  max?: number
  /** Dashed reference line on a series track, in the same units as `max` (e.g. 80 for the high-CPU limit). */
  threshold?: number
  /** Highlighted stretches (breaches) drawn behind the line. */
  bands?: TrackBand[]
  unit?: string
  /** Text shown next to the label for the playhead position (e.g. "87%"); null when there is no value there. */
  valueAt?(tMs: number): string | null
}

export const TRACK_ORDER: TrackId[] = ['screens', 'network', 'cpu', 'mem', 'fps', 'events']

/** `defs` minus hidden ones, in TRACK_ORDER. The events track is the playhead's home and can never be hidden. */
export function visibleTracks(defs: TrackDef[], hidden: Set<TrackId | string>): TrackDef[] {
  return [...defs]
    .filter((d) => d.id === 'events' || !hidden.has(d.id))
    .sort((a, b) => TRACK_ORDER.indexOf(a.id) - TRACK_ORDER.indexOf(b.id))
}

const GAP_FACTOR = 2.5

const fmt = (n: number) => String(Math.round(n * 100) / 100)

/**
 * SVG path for a series: `M x y L x y …` in a `width` x `height` box (y inverted, values clamped to 0..max).
 * A gap wider than 2.5 x the median sample step starts a new subpath, so missing data draws as a gap instead of
 * a misleading line. An isolated point becomes a zero-length segment (renders as a dot with round caps).
 */
export function seriesPath(
  points: SeriesPoint[],
  durationMs: number,
  width: number,
  height: number,
  max: number,
): string {
  if (points.length === 0 || durationMs <= 0 || max <= 0) return ''
  const pts = [...points].sort((a, b) => a.tMs - b.tMs)

  const steps: number[] = []
  for (let i = 1; i < pts.length; i++) {
    const d = pts[i].tMs - pts[i - 1].tMs
    if (d > 0) steps.push(d)
  }
  steps.sort((a, b) => a - b)
  const median = steps.length ? steps[Math.floor(steps.length / 2)] : 0
  const gap = median * GAP_FACTOR

  const xy = (p: SeriesPoint) => {
    const x = Math.min(1, Math.max(0, p.tMs / durationMs)) * width
    const y = height - Math.min(1, Math.max(0, p.v / max)) * height
    return `${fmt(x)} ${fmt(y)}`
  }

  const segments: string[][] = []
  let current: string[] = []
  pts.forEach((p, i) => {
    if (i > 0 && gap > 0 && p.tMs - pts[i - 1].tMs > gap) {
      segments.push(current)
      current = []
    }
    current.push(xy(p))
  })
  segments.push(current)

  return segments
    .map((seg) => (seg.length === 1 ? `M${seg[0]} L${seg[0]}` : `M${seg[0]} ${seg.slice(1).map((c) => `L${c}`).join(' ')}`))
    .join(' ')
}

/** How many items a lane could not show (e.g. "+5" on the network lane). */
export function overflowCount(total: number, shown: number): number {
  return Math.max(0, total - shown)
}

/**
 * Collapse consecutive same-screen spans into one. Screenshot sessions record several frames per second, so a
 * span per frame would draw dozens of identical tags; a revisited screen stays a separate span.
 */
export function mergeScreenRuns(
  raw: { screen: string; startMs: number; endMs: number }[],
): { screen: string; startMs: number; endMs: number }[] {
  const out: { screen: string; startMs: number; endMs: number }[] = []
  for (const s of raw) {
    const last = out[out.length - 1]
    if (last && last.screen === s.screen) last.endMs = Math.max(last.endMs, s.endMs)
    else out.push({ ...s })
  }
  return out
}
