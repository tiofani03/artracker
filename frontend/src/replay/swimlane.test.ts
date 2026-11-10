import { describe, expect, it } from 'vitest'

describe('Timeline Scrubber & Gantt Calculations', () => {
  it('correctly maps time offset to percentage of session duration', () => {
    const durationMs = 60000 // 60s
    const offsetMs = 15000 // 15s
    const pct = (offsetMs / durationMs) * 100
    expect(pct).toBe(25)
  })

  it('clamps progress percentage between 0% and 100%', () => {
    const durationMs = 10000
    const overTime = 15000
    const underTime = -2000

    const clampedOver = Math.min(100, Math.max(0, (overTime / durationMs) * 100))
    const clampedUnder = Math.min(100, Math.max(0, (underTime / durationMs) * 100))

    expect(clampedOver).toBe(100)
    expect(clampedUnder).toBe(0)
  })

  it('calculates screen and network span widths proportionally', () => {
    const sessionDurationMs = 120000 // 2 minutes
    const spanDurationMs = 30000 // 30 seconds
    const spanPctWidth = (spanDurationMs / sessionDurationMs) * 100

    expect(spanPctWidth).toBe(25)
  })
})
