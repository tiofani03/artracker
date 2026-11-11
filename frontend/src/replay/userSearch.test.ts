import { describe, expect, it } from 'vitest'
import { normalizeUserQuery, USER_QUERY_MAX } from './userSearch'

describe('normalizeUserQuery', () => {
  it('trims surrounding whitespace', () => {
    expect(normalizeUserQuery('  user_42  ')).toBe('user_42')
  })

  it('keeps inner characters as typed (the server treats % and _ literally)', () => {
    expect(normalizeUserQuery('100%_x')).toBe('100%_x')
  })

  it('is empty for blank input', () => {
    expect(normalizeUserQuery('   ')).toBe('')
    expect(normalizeUserQuery('')).toBe('')
  })

  it('caps at the 128 characters ids may have', () => {
    expect(USER_QUERY_MAX).toBe(128)
    expect(normalizeUserQuery('a'.repeat(300))).toHaveLength(128)
  })
})
