import { describe, expect, it } from 'vitest'
import type { TelemetryEvent } from '../api'
import { buildTimelineRows } from './sessionTimeline'

function ev(over: Partial<TelemetryEvent>): TelemetryEvent {
  return {
    id: 'e1',
    kind: 'event',
    name: 'screen_view',
    level: 'info',
    message: '',
    session_id: 's1',
    user_id: 'u1',
    environment: 'production',
    release_version: '1.0.0',
    event_timestamp: '2026-01-01T00:00:00.000Z',
    attributes: {},
    ...over,
  }
}

describe('buildTimelineRows', () => {
  it('returns no rows for no events', () => {
    expect(buildTimelineRows([])).toEqual([])
  })

  it('sorts out-of-order events and labels offsets from the first event', () => {
    const rows = buildTimelineRows([
      ev({ id: 'b', event_timestamp: '2026-01-01T00:00:01.200Z', message: 'second' }),
      ev({ id: 'a', event_timestamp: '2026-01-01T00:00:00.000Z', message: 'first' }),
    ])
    expect(rows.map((r) => r.text)).toEqual(['first', 'second'])
    expect(rows.map((r) => r.offsetLabel)).toEqual(['+0.0s', '+1.2s'])
    expect(rows.map((r) => r.key)).toEqual(['a', 'b'])
  })

  it('labels a crash row as an error', () => {
    const [row] = buildTimelineRows([
      ev({ id: 'ie_7', kind: 'crash', name: 'NullPointerException', level: 'fatal', message: 'boom' }),
    ])
    expect(row.label).toBe('crash')
    expect(row.tone).toBe('error')
    expect(row.text).toBe('boom')
  })

  it('classifies a network event', () => {
    const [row] = buildTimelineRows([
      ev({ name: 'network', message: 'POST /checkout', attributes: { method: 'POST', status_code: 502 } }),
    ])
    expect(row.tone).toBe('network')
  })

  it('falls back to the name when the message is empty', () => {
    const [row] = buildTimelineRows([ev({ name: 'app_start', message: '' })])
    expect(row.text).toBe('app_start')
  })
})
