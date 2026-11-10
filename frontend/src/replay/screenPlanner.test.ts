import { describe, expect, it } from 'vitest'
import { frameKey, planScreenFrame } from './screenPlanner'
import type { ScreenFrameMeta } from './screen'

const f = (ts: number, index: number): ScreenFrameMeta => ({
  ts,
  screen: 's',
  w: 10,
  h: 10,
  vw: 5,
  vh: 5,
  mime: 'image/jpeg',
  chunkSeq: 0,
  index,
  masked: 0,
})
const frames = [100, 200, 300].map(f)
const keys = (...fs: ScreenFrameMeta[]) => new Set(fs.map(frameKey))

describe('planScreenFrame', () => {
  it('shows the due frame when it is decoded', () => {
    const plan = planScreenFrame({ frames, playheadMs: 250, decoded: keys(frames[1]), resolved: true })
    expect(plan.due).toBe(frames[1])
    expect(plan.show).toBe(frames[1])
    expect(plan.buffering).toBe(false)
  })

  it('keeps an earlier decoded frame and reports buffering while the due frame is not decoded', () => {
    const plan = planScreenFrame({ frames, playheadMs: 250, decoded: keys(frames[0]), resolved: true })
    expect(plan.due).toBe(frames[1])
    expect(plan.show).toBe(frames[0])
    expect(plan.buffering).toBe(true)
  })

  it('never shows a decoded frame that is later than the playhead', () => {
    const plan = planScreenFrame({ frames, playheadMs: 150, decoded: keys(frames[2]), resolved: true })
    expect(plan.due).toBe(frames[0])
    expect(plan.show).toBeNull()
    expect(plan.buffering).toBe(true)
  })

  it('shows nothing when nothing is decoded, and nothing is due before the first frame', () => {
    expect(planScreenFrame({ frames, playheadMs: 250, decoded: new Set(), resolved: true }).show).toBeNull()
    const early = planScreenFrame({ frames, playheadMs: 50, decoded: new Set(), resolved: true })
    expect(early.due).toBeNull()
    expect(early.buffering).toBe(false)
  })

  it('reports buffering when the playhead is in an unresolved gap, even if the due frame is decoded (Review Focus 2)', () => {
    const plan = planScreenFrame({ frames, playheadMs: 250, decoded: keys(frames[1]), resolved: false })
    expect(plan.show).toBe(frames[1])
    expect(plan.buffering).toBe(true)
  })

  it('prefetches only the next 2 s, ascending, excluding decoded frames', () => {
    const many = Array.from({ length: 11 }, (_, i) => f(i * 500, i)) // 0 .. 5000
    const plan = planScreenFrame({ frames: many, playheadMs: 1000, decoded: keys(many[3]), resolved: true })
    expect(plan.due).toBe(many[2])
    expect(plan.prefetch.map((x) => x.ts)).toEqual([2000, 2500, 3000])
  })

  it('honours a custom look-ahead', () => {
    const many = Array.from({ length: 11 }, (_, i) => f(i * 500, i))
    const plan = planScreenFrame({ frames: many, playheadMs: 1000, decoded: new Set(), resolved: true, aheadMs: 500 })
    expect(plan.prefetch.map((x) => x.ts)).toEqual([1500])
  })
})
