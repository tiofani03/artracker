import type { TelemetryEvent } from '../api'
import { classifyMarker } from './classifyMarker'

export type TimelineRow = {
  key: string
  offsetLabel: string
  label: string
  text: string
  tone: 'error' | 'network' | 'gesture' | 'other'
}

/** One line per event of a session that has no replay, oldest first, offset from the first event. */
export function buildTimelineRows(events: TelemetryEvent[]): TimelineRow[] {
  const sorted = events
    .map((ev) => ({ ev, ts: Date.parse(ev.event_timestamp) }))
    .sort((a, b) => a.ts - b.ts)
  if (sorted.length === 0) return []
  const t0 = sorted[0].ts
  return sorted.map(({ ev, ts }) => {
    const offsetLabel = `+${(Math.max(0, ts - t0) / 1000).toFixed(1)}s`
    const text = ev.message || ev.name
    if (ev.kind === 'crash') {
      return { key: ev.id, offsetLabel, label: 'crash', text, tone: 'error' }
    }
    const { badgeLabel, category } = classifyMarker(ev)
    return { key: ev.id, offsetLabel, label: badgeLabel, text, tone: category }
  })
}
