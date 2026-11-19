import { describe, expect, it } from 'vitest'
import { formatMs, phaseLabel } from './format'

describe('formatMs', () => {
  it('formats sub-second values in ms', () => {
    expect(formatMs(950)).toBe('950 ms')
  })
  it('formats seconds with two decimals below 10 s', () => {
    expect(formatMs(2412)).toBe('2.41 s')
  })
  it('formats seconds with one decimal from 10 s', () => {
    expect(formatMs(12000)).toBe('12.0 s')
  })
  it('rounds before choosing the unit', () => {
    expect(formatMs(999.6)).toBe('1.00 s')
    expect(formatMs(999.4)).toBe('999 ms')
    expect(formatMs(9996)).toBe('10.0 s')
  })
})

describe('phaseLabel', () => {
  it('maps known phase names to labels', () => {
    expect(phaseLabel('first_draw')).toBe('First draw')
    expect(phaseLabel('process_to_init')).toBe('Process → SDK init')
  })
  it('replaces underscores for unknown names', () => {
    expect(phaseLabel('x_y')).toBe('x y')
  })
})
