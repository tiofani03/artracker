import { describe, expect, it } from 'vitest'
import { adjacentEventOffset, resolveHotkey, SEEK_STEP_MS, type HotkeyInput } from './hotkeys'

const key = (k: string, extra: Partial<HotkeyInput> = {}): HotkeyInput => ({
  key: k,
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  target: { tagName: 'DIV' },
  ...extra,
})

describe('resolveHotkey', () => {
  it('maps the documented keys', () => {
    expect(resolveHotkey(key(' '))).toBe('toggle-play')
    expect(resolveHotkey(key('ArrowLeft'))).toBe('seek-back')
    expect(resolveHotkey(key('ArrowRight'))).toBe('seek-forward')
    expect(resolveHotkey(key('['))).toBe('prev-event')
    expect(resolveHotkey(key(']'))).toBe('next-event')
    expect(resolveHotkey(key('s'))).toBe('cycle-speed')
  })

  it('returns null for unmapped keys', () => {
    expect(resolveHotkey(key('x'))).toBeNull()
  })

  it('ignores keys typed into inputs', () => {
    for (const target of [
      { tagName: 'INPUT' },
      { tagName: 'TEXTAREA' },
      { tagName: 'SELECT' },
      { tagName: 'DIV', isContentEditable: true },
    ]) {
      expect(resolveHotkey(key(' ', { target }))).toBeNull()
    }
  })

  it('ignores combos with Ctrl, Meta or Alt', () => {
    expect(resolveHotkey(key('s', { ctrlKey: true }))).toBeNull()
    expect(resolveHotkey(key('s', { metaKey: true }))).toBeNull()
    expect(resolveHotkey(key('ArrowLeft', { altKey: true }))).toBeNull()
  })

  it('works when there is no target', () => {
    expect(resolveHotkey(key(' ', { target: null }))).toBe('toggle-play')
  })

  it('seeks in 5 second steps', () => {
    expect(SEEK_STEP_MS).toBe(5000)
  })
})

describe('adjacentEventOffset', () => {
  const markers = [{ offsetMs: 1000 }, { offsetMs: 5000 }, { offsetMs: 9000 }]

  it('next goes to the first marker more than 50 ms ahead', () => {
    expect(adjacentEventOffset(markers, 5000, 1)).toBe(9000)
    expect(adjacentEventOffset(markers, 4960, 1)).toBe(9000)
    expect(adjacentEventOffset(markers, 4940, 1)).toBe(5000)
  })

  it('previous goes to the latest marker more than 500 ms behind', () => {
    expect(adjacentEventOffset(markers, 5000, -1)).toBe(1000)
    expect(adjacentEventOffset(markers, 5600, -1)).toBe(5000)
  })

  it('returns null at either end', () => {
    expect(adjacentEventOffset(markers, 9000, 1)).toBeNull()
    expect(adjacentEventOffset(markers, 1200, -1)).toBeNull()
    expect(adjacentEventOffset([], 0, 1)).toBeNull()
  })

  it('does not depend on marker order', () => {
    expect(adjacentEventOffset([{ offsetMs: 9000 }, { offsetMs: 1000 }, { offsetMs: 5000 }], 5000, 1)).toBe(9000)
  })
})
