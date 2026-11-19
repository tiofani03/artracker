import { describe, expect, it } from 'vitest'
import { formatDuration } from './format'

describe('formatDuration', () => {
  it('formats zero', () => {
    expect(formatDuration(0)).toBe('0:00')
  })
  it('formats seconds', () => {
    expect(formatDuration(4200)).toBe('0:04')
  })
  it('formats minutes', () => {
    expect(formatDuration(70000)).toBe('1:10')
  })
  it('formats hours', () => {
    expect(formatDuration(3723000)).toBe('1:02:03')
  })
  it('treats negative or non-finite as zero', () => {
    expect(formatDuration(-5)).toBe('0:00')
    expect(formatDuration(Number.NaN)).toBe('0:00')
  })
})
