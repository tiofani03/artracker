import { describe, expect, it } from 'vitest'
import { EventType, type eventWithTime } from 'rrweb'
import {
  resolveExportRange,
  sliceMobileData,
  slicePerfSamples,
  sliceScreenData,
  sliceTimelineEvents,
  sliceWebEventsWithBaseline,
} from './exportRange'
import type { ScreenFrameMeta, ScreenTouch } from './screen'
import type { MobileFrame, TouchPoint } from './model'
import type { TelemetryEvent } from '../api'

describe('resolveExportRange', () => {
  it('clamps and normalizes clip offsets against session duration', () => {
    const base = 1000000
    const dur = 60000

    const r1 = resolveExportRange(base, dur, -5000, 30000)
    expect(r1.startOffsetMs).toBe(0)
    expect(r1.endOffsetMs).toBe(30000)
    expect(r1.startAbsMs).toBe(base)
    expect(r1.endAbsMs).toBe(base + 30000)

    const r2 = resolveExportRange(base, dur, 40000, 20000) // start > end
    expect(r2.startOffsetMs).toBe(40000)
    expect(r2.endOffsetMs).toBe(40000)

    const r3 = resolveExportRange(base, dur, 10000, 100000) // end > duration
    expect(r3.startOffsetMs).toBe(10000)
    expect(r3.endOffsetMs).toBe(60000)
  })
})

describe('sliceTimelineEvents & slicePerfSamples', () => {
  it('filters timeline events within time bounds', () => {
    const events: TelemetryEvent[] = [
      { id: '1', kind: 'info', name: 'ev1', level: 'info', message: '', session_id: 's1', user_id: 'u1', environment: 'prod', release_version: '1', event_timestamp: '2026-11-26T10:00:00Z', attributes: {} },
      { id: '2', kind: 'error', name: 'ev2', level: 'error', message: '', session_id: 's1', user_id: 'u1', environment: 'prod', release_version: '1', event_timestamp: '2026-11-26T10:00:15Z', attributes: {} },
      { id: '3', kind: 'info', name: 'ev3', level: 'info', message: '', session_id: 's1', user_id: 'u1', environment: 'prod', release_version: '1', event_timestamp: '2026-11-26T10:00:30Z', attributes: {} },
    ]

    const start = Date.parse('2026-11-26T10:00:10Z')
    const end = Date.parse('2026-11-26T10:00:20Z')
    const sliced = sliceTimelineEvents(events, start, end)
    expect(sliced.map((e) => e.id)).toEqual(['2'])
  })

  it('filters perf samples', () => {
    const perf = {
      interval_ms: 1000,
      device: {},
      metrics: ['cpu', 'mem_mb', 'fps'],
      downsampled: false,
      samples: [
        { t: 1000, cpu: 10, fps: 60 },
        { t: 5000, cpu: 20, fps: 55 },
        { t: 9000, cpu: 30, fps: 45 },
      ],
    }
    const sliced = slicePerfSamples(perf, 4000, 6000)
    expect(sliced?.samples.length).toBe(1)
    expect(sliced?.samples[0].t).toBe(5000)
    expect(sliced?.samples[0].cpu).toBe(20)
  })
})

describe('sliceScreenData', () => {
  it('includes preceding baseline frame plus frames inside range', () => {
    const frames: ScreenFrameMeta[] = [
      { ts: 1000, screen: 'Home', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg', chunkSeq: 0, index: 0, masked: 0 },
      { ts: 3000, screen: 'Details', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg', chunkSeq: 0, index: 1, masked: 0 },
      { ts: 7000, screen: 'Settings', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg', chunkSeq: 1, index: 0, masked: 0 },
      { ts: 10000, screen: 'Settings', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg', chunkSeq: 1, index: 1, masked: 0 },
    ]
    const touches: ScreenTouch[] = [
      { ts: 2000, x: 10, y: 10, kind: 'down' },
      { ts: 5000, x: 20, y: 20, kind: 'up' },
    ]

    // Clip 4000..8000. Last frame before 4000 is ts: 3000. Frame in range is ts: 7000.
    const res = sliceScreenData(frames, touches, 4000, 8000)
    expect(res.frames.map((f) => f.ts)).toEqual([3000, 7000])
    expect(res.touches.map((t) => t.ts)).toEqual([5000])
  })
})

describe('sliceMobileData', () => {
  it('includes baseline frame and touches inside range', () => {
    const frames: MobileFrame[] = [
      { ts: 1000, screen: 'A', viewTree: { cls: 'UIView' } },
      { ts: 2000, screen: 'B', viewTree: { cls: 'UIView' } },
      { ts: 5000, screen: 'C', viewTree: { cls: 'UIView' } },
    ]
    const touches: TouchPoint[] = [
      { ts: 1500, x: 10, y: 10 },
      { ts: 2500, x: 20, y: 20 },
    ]

    const res = sliceMobileData(frames, touches, 2000, 4000)
    // ts: 2000 is <= startAbsMs, so it is the baseline
    expect(res.frames.map((f) => f.ts)).toEqual([2000])
    expect(res.touches.map((t) => t.ts)).toEqual([2500])
  })
})

describe('sliceWebEventsWithBaseline', () => {
  it('preserves Meta and closest preceding FullSnapshot', () => {
    const events: eventWithTime[] = [
      { type: EventType.Meta, data: { href: 'http://localhost' }, timestamp: 1000 } as eventWithTime,
      { type: EventType.FullSnapshot, data: { node: {} }, timestamp: 1100 } as eventWithTime,
      { type: EventType.IncrementalSnapshot, data: {}, timestamp: 2000 } as eventWithTime,
      { type: EventType.FullSnapshot, data: { node: { updated: true } }, timestamp: 5000 } as eventWithTime,
      { type: EventType.IncrementalSnapshot, data: {}, timestamp: 6000 } as eventWithTime,
      { type: EventType.IncrementalSnapshot, data: {}, timestamp: 9000 } as eventWithTime,
    ]

    // Slice 5500..8000
    // Last FullSnapshot <= 5500 is at ts: 5000. Preceding Meta is at ts: 1000.
    const res = sliceWebEventsWithBaseline(events, 5500, 8000)
    expect(res[0].type).toBe(EventType.Meta)
    expect(res[1].type).toBe(EventType.FullSnapshot)
    expect(res[1].timestamp).toBe(5000)
    expect(res[2].type).toBe(EventType.IncrementalSnapshot)
    expect(res[2].timestamp).toBe(6000)
    expect(res.length).toBe(3)
  })
})
