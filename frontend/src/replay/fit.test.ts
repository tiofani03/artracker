import { describe, expect, it } from 'vitest'
import { fillHeight, fitScale } from './fit'

describe('fitScale', () => {
  it('scales down to the tighter axis', () => {
    expect(fitScale({ w: 600, h: 400 }, { w: 320, h: 560 })).toBeCloseTo(400 / 560, 5)
  })

  it('never scales up', () => {
    expect(fitScale({ w: 2000, h: 2000 }, { w: 320, h: 560 })).toBe(1)
  })

  it('never goes below 0.25', () => {
    expect(fitScale({ w: 10, h: 10 }, { w: 320, h: 560 })).toBe(0.25)
  })

  it('falls back to 1 for an unmeasured (zero-sized) content or area', () => {
    expect(fitScale({ w: 600, h: 400 }, { w: 0, h: 0 })).toBe(1)
    expect(fitScale({ w: 0, h: 0 }, { w: 320, h: 560 })).toBe(1)
  })
})

describe('fillHeight', () => {
  it('fills the viewport below the page top minus the bottom pad', () => {
    expect(fillHeight(768, 120)).toBe(632)
  })

  it('never drops below the minimum', () => {
    expect(fillHeight(500, 120)).toBe(480)
  })

  it('honours custom pad and minimum', () => {
    expect(fillHeight(1000, 100, 0, 300)).toBe(900)
    expect(fillHeight(300, 100, 0, 300)).toBe(300)
  })
})
