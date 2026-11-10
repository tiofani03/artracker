import { describe, expect, it } from 'vitest'
import { formatNavBadge } from './navBadge'

describe('formatNavBadge', () => {
  it('hides the badge when there is nothing to show', () => {
    expect(formatNavBadge(0)).toBeUndefined()
    expect(formatNavBadge(undefined)).toBeUndefined()
    expect(formatNavBadge(-3)).toBeUndefined()
    expect(formatNavBadge(Number.NaN)).toBeUndefined()
  })
  it('shows the exact count up to 99', () => {
    expect(formatNavBadge(1)).toBe('1')
    expect(formatNavBadge(99)).toBe('99')
  })
  it('caps larger counts at 99+', () => {
    expect(formatNavBadge(100)).toBe('99+')
    expect(formatNavBadge(12345)).toBe('99+')
  })
})
