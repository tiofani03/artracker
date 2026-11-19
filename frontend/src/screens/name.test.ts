import { describe, expect, it } from 'vitest'
import { displayScreen, screenMatches } from './name'

describe('displayScreen', () => {
  it('drops the android:// and ios:// schemes', () => {
    expect(displayScreen('android://MainActivity')).toBe('MainActivity')
    expect(displayScreen('ios://HomeViewController')).toBe('HomeViewController')
  })
  it('keeps names without a scheme, and a bare scheme', () => {
    expect(displayScreen('HomePageViewController')).toBe('HomePageViewController')
    expect(displayScreen('Exit')).toBe('Exit')
    expect(displayScreen('android://')).toBe('android://')
  })
})

describe('screenMatches', () => {
  it('matches the displayed name case-insensitively', () => {
    expect(screenMatches('android://MainActivity', 'mainact')).toBe(true)
    expect(screenMatches('android://MainActivity', 'basket')).toBe(false)
    expect(screenMatches('android://MainActivity', '  ')).toBe(true)
  })
  it('does not match on the scheme', () => {
    expect(screenMatches('android://MainActivity', 'android')).toBe(false)
  })
})
