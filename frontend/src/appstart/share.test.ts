import { describe, expect, it } from 'vitest'
import { shareLabel } from './format'

describe('shareLabel', () => {
  it('rounds the share of all starts', () => {
    expect(shareLabel(8, 11)).toBe('73%')
    expect(shareLabel(11, 11)).toBe('100%')
  })
  it('keeps a tiny non-zero share visible and handles empty totals', () => {
    expect(shareLabel(1, 500)).toBe('<1%')
    expect(shareLabel(0, 11)).toBe('0%')
    expect(shareLabel(0, 0)).toBe('0%')
  })
})
