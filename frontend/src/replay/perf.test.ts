import { describe, expect, it } from 'vitest'
import type { PerfSample } from '../api'
import { detectBreaches, perfValueAt, summarizePerf, toSeries } from './perf'

const S = (t: number, rest: Partial<PerfSample> = {}): PerfSample => ({ t, ...rest })
const START = 1_000_000

describe('perfValueAt', () => {
  const samples = [S(1000, { cpu: 10 }), S(2000, { cpu: 20 })]

  it('returns the latest sample at or before the timestamp', () => {
    expect(perfValueAt(samples, 1500, 1000)?.cpu).toBe(10)
    expect(perfValueAt(samples, 2000, 1000)?.cpu).toBe(20)
  })

  it('is null before the first sample', () => {
    expect(perfValueAt(samples, 999, 1000)).toBeNull()
  })

  it('is null once the playhead is more than 3 intervals past the last sample', () => {
    expect(perfValueAt(samples, 5000, 1000)?.cpu).toBe(20)
    expect(perfValueAt(samples, 5001, 1000)).toBeNull()
  })

  it('is null for no samples', () => {
    expect(perfValueAt([], 1000, 1000)).toBeNull()
  })
})

describe('toSeries', () => {
  it('skips samples without the metric and offsets t from the session start', () => {
    const pts = toSeries([S(START + 0, { fps: 60 }), S(START + 1000), S(START + 2000, { fps: 30 })], 'fps', START)
    expect(pts).toEqual([
      { tMs: 0, v: 60 },
      { tMs: 2000, v: 30 },
    ])
  })
})

describe('detectBreaches', () => {
  const run = (cpus: (number | undefined)[]) =>
    cpus.map((cpu, i) => S(START + (i + 1) * 1000, cpu === undefined ? {} : { cpu }))

  it('high_cpu needs 3 consecutive samples at >= 80', () => {
    const b = detectBreaches(run([85, 90, 95, 10]), START, 1000)
    expect(b).toHaveLength(1)
    expect(b[0]).toEqual({ kind: 'high_cpu', startMs: 1000, endMs: 4000, peak: 95 })
    expect(detectBreaches(run([85, 90, 10]), START, 1000)).toEqual([])
  })

  it('a sample without cpu breaks a high_cpu run', () => {
    expect(detectBreaches(run([85, undefined, 90, 95]), START, 1000)).toEqual([])
  })

  it('runs do not bridge a time gap (app was in the background)', () => {
    const samples = [S(START + 1000, { cpu: 90 }), S(START + 2000, { cpu: 90 }), S(START + 60_000, { cpu: 90 })]
    expect(detectBreaches(samples, START, 1000)).toEqual([])
  })

  it('low_fps ignores samples without fps', () => {
    const none = [S(START + 1000, { fps: 20 }), S(START + 2000), S(START + 3000, { fps: 20 })]
    expect(detectBreaches(none, START, 1000)).toEqual([])
    const two = [S(START + 1000, { fps: 20 }), S(START + 2000, { fps: 22 })]
    expect(detectBreaches(two, START, 1000)).toEqual([{ kind: 'low_fps', startMs: 1000, endMs: 3000, peak: 20 }])
  })

  it('low_fps threshold is half the display refresh rate', () => {
    const fifty = [S(START + 1000, { fps: 50 }), S(START + 2000, { fps: 50 })]
    expect(detectBreaches(fifty, START, 1000, 120)).toHaveLength(1)
    expect(detectBreaches(fifty, START, 1000, 60)).toHaveLength(0)
    expect(detectBreaches(fifty, START, 1000)).toHaveLength(0) // default refresh is 60
  })

  it('jank_burst is a single sample with jank >= 5', () => {
    expect(detectBreaches([S(START + 1000, { jank: 5 })], START, 1000)).toEqual([
      { kind: 'jank_burst', startMs: 1000, endMs: 2000, peak: 5 },
    ])
    expect(detectBreaches([S(START + 1000, { jank: 4 })], START, 1000)).toEqual([])
  })

  it('returns breaches ordered by start time', () => {
    const samples = [
      S(START + 1000, { jank: 9 }),
      S(START + 2000, { cpu: 90 }),
      S(START + 3000, { cpu: 90 }),
      S(START + 4000, { cpu: 90 }),
    ]
    expect(detectBreaches(samples, START, 1000).map((b) => b.kind)).toEqual(['jank_burst', 'high_cpu'])
  })
})

describe('summarizePerf', () => {
  it('averages cpu over present values only and sums frames', () => {
    const sum = summarizePerf([
      S(1, { cpu: 10, mem_mb: 100, jank: 2, frozen: 1 }),
      S(2, { mem_mb: 300 }),
      S(3, { cpu: 30, jank: 3 }),
    ])
    expect(sum).toEqual({ cpuPeak: 30, cpuAvg: 20, memPeak: 300, jankTotal: 5, frozenTotal: 1 })
  })

  it('leaves metrics undefined when no sample has them', () => {
    expect(summarizePerf([S(1)])).toEqual({ jankTotal: 0, frozenTotal: 0 })
  })
})
