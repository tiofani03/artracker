import { describe, expect, it } from 'vitest'
import type { FlowScreen } from '../api'
import {
  defaultScreen,
  exitRateLabel,
  isSelectableNode,
  isTerminal,
  linkLabel,
  linkPath,
  mirrorX,
  percentLabel,
  resolveSelection,
  truncate,
} from './view'

function row(screen: string, sessions: number, entry: number, exit = 0, platform = 'ios'): FlowScreen {
  return { screen, platform, sessions, views: sessions, median_ms: 0, p75_ms: 0, entry_sessions: entry, exit_sessions: exit }
}

describe('percentLabel', () => {
  it('rounds to whole percent', () => {
    expect(percentLabel(412, 1200)).toBe('34%')
    expect(percentLabel(1, 1)).toBe('100%')
  })
  it('shows <1% for tiny non-zero shares', () => {
    expect(percentLabel(1, 1000)).toBe('<1%')
  })
  it('is 0% for zero part or zero total', () => {
    expect(percentLabel(0, 10)).toBe('0%')
    expect(percentLabel(5, 0)).toBe('0%')
  })
})

describe('exitRateLabel', () => {
  it('is exit sessions over sessions', () => {
    expect(exitRateLabel({ exit_sessions: 18, sessions: 100 })).toBe('18%')
    expect(exitRateLabel({ exit_sessions: 0, sessions: 0 })).toBe('0%')
  })
})

describe('linkLabel', () => {
  it('formats sessions and share of step 0', () => {
    expect(linkLabel(412, 1200)).toBe('412 sessions (34%)')
    expect(linkLabel(1, 4)).toBe('1 session (25%)')
  })
})

describe('defaultScreen', () => {
  it('picks the most entry sessions', () => {
    expect(defaultScreen([row('A', 100, 5), row('B', 50, 40), row('C', 80, 10)])?.screen).toBe('B')
  })
  it('breaks ties by sessions', () => {
    expect(defaultScreen([row('A', 10, 5), row('B', 50, 5)])?.screen).toBe('B')
  })
  it('is null for an empty list', () => {
    expect(defaultScreen([])).toBeNull()
  })
})

describe('resolveSelection', () => {
  const screens = [row('Home', 100, 80), row('Cart', 30, 1)]
  it('keeps a pick that is listed', () => {
    expect(resolveSelection(screens, { screen: 'Cart', platform: 'ios' })).toEqual({ screen: 'Cart', platform: 'ios' })
  })
  it('falls back to the default when the pick is gone', () => {
    expect(resolveSelection(screens, { screen: 'Gone', platform: 'ios' })).toEqual({ screen: 'Home', platform: 'ios' })
    expect(resolveSelection(screens, { screen: 'Cart', platform: 'android' })).toEqual({ screen: 'Home', platform: 'ios' })
  })
  it('uses the default without a pick and null without screens', () => {
    expect(resolveSelection(screens, null)?.screen).toBe('Home')
    expect(resolveSelection([], null)).toBeNull()
  })
})

describe('node kinds', () => {
  it('treats Exit and Start as terminals', () => {
    expect(isTerminal('Exit')).toBe(true)
    expect(isTerminal('Start')).toBe(true)
    expect(isTerminal('Other')).toBe(false)
  })
  it('only real screens are selectable', () => {
    expect(isSelectableNode('android://MainActivity')).toBe(true)
    expect(isSelectableNode('Exit')).toBe(false)
    expect(isSelectableNode('Start')).toBe(false)
    expect(isSelectableNode('Other')).toBe(false)
  })
})

describe('mirrorX', () => {
  it('puts the first column on the right edge', () => {
    expect(mirrorX(0, 700, 14)).toBe(686)
    expect(mirrorX(686, 700, 14)).toBe(0)
  })
})

describe('linkPath', () => {
  it('runs from the source right edge to the target left edge going forward', () => {
    expect(linkPath(0, 10, 100, 50, 14)).toBe('M14,10 C57,10 57,50 100,50')
  })
  it('runs from the source left edge to the target right edge when mirrored', () => {
    expect(linkPath(100, 10, 0, 50, 14)).toBe('M100,10 C57,10 57,50 14,50')
  })
})

describe('truncate', () => {
  it('keeps short text and shortens long text', () => {
    expect(truncate('Home', 10)).toBe('Home')
    expect(truncate('ProductViewController', 10)).toBe('ProductVi…')
  })
})
