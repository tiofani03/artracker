import { describe, expect, it } from 'vitest'
import type { HeatmapCell, HeatmapResponse } from '../api'
import { cellAt, cellShare, MIN_RELIABLE_SESSIONS, shareLabel } from './grid'

const res = (cols: number, rows: number, cells: HeatmapCell[]): HeatmapResponse => ({
  cols, rows, cells, rage: [], screen_sessions: 100, aspect: 0.5, aspects: [], background: null,
})

describe('cellShare', () => {
  it('divides cell sessions by screen sessions', () => {
    expect(cellShare({ c: 0, r: 0, sessions: 25, taps: 40 }, 100)).toBe(0.25)
  })
  it('returns 0 when denominator is 0', () => {
    expect(cellShare({ c: 0, r: 0, sessions: 5, taps: 5 }, 0)).toBe(0)
  })
})

describe('cellAt', () => {
  const a: HeatmapCell = { c: 0, r: 0, sessions: 1, taps: 1 }
  const last: HeatmapCell = { c: 3, r: 7, sessions: 2, taps: 2 }
  const r = res(4, 8, [a, last])
  it('finds a cell by normalized coords', () => {
    expect(cellAt(r, 0.1, 0.05)).toBe(a)
  })
  it('maps x=1,y=1 to the last cell', () => {
    expect(cellAt(r, 1, 1)).toBe(last)
  })
  it('returns null for empty cell or out of range', () => {
    expect(cellAt(r, 0.5, 0.5)).toBeNull()
    expect(cellAt(r, -0.1, 0.5)).toBeNull()
    expect(cellAt(r, 0.5, 1.2)).toBeNull()
  })
})

describe('shareLabel', () => {
  it('formats', () => {
    expect(shareLabel(0.123)).toBe('12%')
    expect(shareLabel(0.004)).toBe('<1%')
    expect(shareLabel(0)).toBe('0%')
    expect(shareLabel(1)).toBe('100%')
  })
  it('exposes threshold', () => {
    expect(MIN_RELIABLE_SESSIONS).toBe(20)
  })
})
