import { describe, expect, it } from 'vitest'
import { addedScreens, normalizeExcluded, sameExcluded } from './excluded'

describe('normalizeExcluded', () => {
  it('trims, drops empties and duplicates, keeps order', () => {
    expect(normalizeExcluded([' Home ', '', '  ', 'Cart', 'Home'])).toEqual(['Home', 'Cart'])
  })
  it('returns empty for empty input', () => {
    expect(normalizeExcluded([])).toEqual([])
  })
})

describe('addedScreens / sameExcluded', () => {
  it('returns only newly added, normalized names', () => {
    expect(addedScreens(['A', 'B'], ['A', ' C ', 'C', 'D'])).toEqual(['C', 'D'])
    expect(addedScreens(['A', 'B'], ['A'])).toEqual([])
  })
  it('compares lists ignoring order', () => {
    expect(sameExcluded(['A', 'B'], ['B', 'A'])).toBe(true)
    expect(sameExcluded(['A'], ['A', 'B'])).toBe(false)
  })
})
