import { describe, expect, it } from 'vitest'
import { toShareRows, versionRange } from './breakdown'

describe('toShareRows', () => {
  it('turns bucket counts into percentages of the total', () => {
    expect(toShareRows([{ value: 'Android 14', count: 3 }, { value: 'Android 13', count: 1 }], 4)).toEqual([
      { label: 'Android 14', count: 3, pct: 75 },
      { label: 'Android 13', count: 1, pct: 25 },
    ])
  })

  it('folds everything past the limit and any untracked remainder into Other', () => {
    const rows = toShareRows(
      [
        { value: 'a', count: 5 },
        { value: 'b', count: 3 },
        { value: 'c', count: 1 },
      ],
      10,
      2,
    )
    expect(rows).toEqual([
      { label: 'a', count: 5, pct: 50 },
      { label: 'b', count: 3, pct: 30 },
      { label: 'Other', count: 2, pct: 20 },
    ])
  })

  it('returns nothing when there are no events', () => {
    expect(toShareRows([], 0)).toEqual([])
  })
})

describe('versionRange', () => {
  it('orders versions semantically and shows lowest to highest', () => {
    expect(versionRange([{ value: '2.10.0', count: 1 }, { value: '2.9.1', count: 4 }])).toBe('2.9.1 – 2.10.0')
  })

  it('shows a single version on its own', () => {
    expect(versionRange([{ value: '1.0', count: 2 }])).toBe('1.0')
  })

  it('ignores Unknown and returns a dash when nothing is known', () => {
    expect(versionRange([{ value: 'Unknown', count: 2 }])).toBe('—')
  })
})
