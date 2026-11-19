import { describe, expect, it } from 'vitest'
import type { SymbolicatedProfile } from '../api'
import { boxStats, layoutFlame, toPixels, type FlameBox } from './flame'

const frames = (n: number) => Array.from({ length: n }, (_, i) => ({ function: `f${i}`, is_app: true, resolved: true }))

describe('layoutFlame', () => {
  it('merges adjacent samples sharing a frame and parent', () => {
    const p: SymbolicatedProfile = {
      interval_ms: 10,
      frames: frames(3),
      stacks: [[0, 1], [0, 2]],
      samples: [{ t: 0, s: 0 }, { t: 10, s: 0 }, { t: 20, s: 1 }],
    }
    const boxes = layoutFlame(p)
    expect(boxes).toHaveLength(3)
    expect(boxes).toContainEqual({ depth: 0, start: 0, end: 30, frame: 0 })
    expect(boxes).toContainEqual({ depth: 1, start: 0, end: 20, frame: 1 })
    expect(boxes).toContainEqual({ depth: 1, start: 20, end: 30, frame: 2 })
  })

  it('splits the same frame under different parents', () => {
    const p: SymbolicatedProfile = {
      interval_ms: 10,
      frames: frames(3),
      stacks: [[0, 2], [1, 2]],
      samples: [{ t: 0, s: 0 }, { t: 10, s: 1 }],
    }
    const deep = layoutFlame(p).filter((b) => b.depth === 1)
    expect(deep).toEqual([
      { depth: 1, start: 0, end: 10, frame: 2 },
      { depth: 1, start: 10, end: 20, frame: 2 },
    ])
  })

  it('caps a sample gap at 100 ms and gives the last sample interval_ms', () => {
    const p: SymbolicatedProfile = {
      interval_ms: 10,
      frames: frames(2),
      stacks: [[0], [1]],
      samples: [{ t: 0, s: 0 }, { t: 500, s: 1 }],
    }
    expect(layoutFlame(p)).toEqual([
      { depth: 0, start: 0, end: 100, frame: 0 },
      { depth: 0, start: 500, end: 510, frame: 1 },
    ])
  })

  it('weighs a non-positive gap as zero and returns nothing for an empty profile', () => {
    const p: SymbolicatedProfile = {
      interval_ms: 10,
      frames: frames(1),
      stacks: [[0]],
      samples: [{ t: 10, s: 0 }, { t: 10, s: 0 }, { t: 20, s: 0 }],
    }
    expect(layoutFlame(p)).toEqual([{ depth: 0, start: 10, end: 30, frame: 0 }])
    expect(layoutFlame({ ...p, samples: [] })).toEqual([])
  })
})

describe('boxStats', () => {
  const p: SymbolicatedProfile = {
    interval_ms: 10,
    frames: frames(3),
    stacks: [[0, 1], [0, 2], [0]],
    samples: [{ t: 0, s: 0 }, { t: 10, s: 0 }, { t: 20, s: 1 }, { t: 30, s: 2 }],
  }
  it('reports total and self for a leaf box', () => {
    expect(boxStats(p, { depth: 1, start: 0, end: 20, frame: 1 })).toEqual({ totalMs: 20, selfMs: 20 })
  })
  it('reports total as summed sample weight, not wall-clock extent', () => {
    const sparse: SymbolicatedProfile = {
      interval_ms: 10,
      frames: frames(1),
      stacks: [[0]],
      samples: [{ t: 0, s: 0 }, { t: 500, s: 0 }],
    }
    const [box] = layoutFlame(sparse)
    expect(box.end - box.start).toBe(510)
    expect(boxStats(sparse, box)).toEqual({ totalMs: 110, selfMs: 110 })
  })
  it('reports self as only the samples where the box is the top frame', () => {
    expect(boxStats(p, { depth: 0, start: 0, end: 40, frame: 0 })).toEqual({ totalMs: 40, selfMs: 10 })
  })
})

describe('Flamegraph geometry', () => {
  const box = (depth: number, start: number, end: number): FlameBox => ({ depth, start, end, frame: 0 })

  it('maps start and end linearly onto the width', () => {
    const [px] = toPixels([box(0, 0, 50)], 0, 100, 400)
    expect(px).toMatchObject({ x: 0, w: 200 })
    const [shifted] = toPixels([box(1, 25, 75)], 0, 100, 400)
    expect(shifted).toMatchObject({ depth: 1, x: 100, w: 200 })
  })

  it('drops boxes narrower than 1 px', () => {
    const out = toPixels([box(0, 0, 0.2), box(0, 10, 11), box(0, 20, 100)], 0, 100, 100)
    expect(out.map((b) => b.start)).toEqual([10, 20])
  })

  it('zooms: maps against the view window and clips to the width', () => {
    const out = toPixels([box(0, 0, 100), box(1, 0, 20), box(1, 40, 60)], 40, 60, 200)
    expect(out).toHaveLength(2)
    expect(out[0]).toMatchObject({ x: 0, w: 200 })
    expect(out[1]).toMatchObject({ start: 40, x: 0, w: 200 })
  })

  it('returns nothing for an empty window or width', () => {
    expect(toPixels([box(0, 0, 10)], 5, 5, 100)).toEqual([])
    expect(toPixels([box(0, 0, 10)], 0, 10, 0)).toEqual([])
  })
})
