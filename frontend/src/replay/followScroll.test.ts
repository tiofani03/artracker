import { describe, expect, it } from 'vitest'
import { nextScrollTop } from './followScroll'

describe('nextScrollTop', () => {
  it('scrolls down just enough to reveal an item below the view', () => {
    // item bottom = 530; view bottom = 0 + 260 -> needs 530 - 260 + 8
    expect(nextScrollTop({ scrollTop: 0, clientHeight: 260 }, { offsetTop: 500, offsetHeight: 30 })).toBe(278)
  })

  it('scrolls up to reveal an item above the view', () => {
    expect(nextScrollTop({ scrollTop: 200, clientHeight: 260 }, { offsetTop: 40, offsetHeight: 30 })).toBe(32)
  })

  it('returns null when the item is already fully visible', () => {
    expect(nextScrollTop({ scrollTop: 0, clientHeight: 260 }, { offsetTop: 100, offsetHeight: 30 })).toBeNull()
  })

  it('never returns a negative scrollTop', () => {
    expect(nextScrollTop({ scrollTop: 50, clientHeight: 260 }, { offsetTop: 2, offsetHeight: 30 })).toBe(0)
  })

  it('honours a custom pad', () => {
    expect(nextScrollTop({ scrollTop: 0, clientHeight: 100 }, { offsetTop: 200, offsetHeight: 20 }, 0)).toBe(120)
  })
})
