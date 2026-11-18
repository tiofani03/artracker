import { describe, expect, it } from 'vitest'
import { normalizeExcluded } from './excluded'

describe('normalizeExcluded', () => {
  it('trims, drops empties and duplicates, keeps order', () => {
    expect(normalizeExcluded([' Home ', '', '  ', 'Cart', 'Home'])).toEqual(['Home', 'Cart'])
  })
  it('returns empty for empty input', () => {
    expect(normalizeExcluded([])).toEqual([])
  })
})
