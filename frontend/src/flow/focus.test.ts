import { describe, expect, it } from 'vitest'
import type { FlowPathsResponse, FlowScreen } from '../api'
import { exitTone, heatmapHref, neighbourLabel, neighbours, rateOf, sortScreens } from './focus'

function paths(over: Partial<FlowPathsResponse>): FlowPathsResponse {
  return { screen: 'Home', direction: 'next', sessions: 10, nodes: [], links: [], ...over }
}

describe('neighbours', () => {
  it('lists the step-1 screens with their share of step-0 sessions, busiest first', () => {
    const got = neighbours(
      paths({
        links: [
          { source: '0:Home', target: '1:Cart', sessions: 3 },
          { source: '0:Home', target: '1:Search', sessions: 5 },
          { source: '0:Home', target: '1:Exit', sessions: 2 },
          { source: '1:Cart', target: '2:Pay', sessions: 3 },
        ],
      }),
    )
    expect(got).toEqual([
      { screen: 'Search', sessions: 5, share: 0.5 },
      { screen: 'Cart', sessions: 3, share: 0.3 },
      { screen: 'Exit', sessions: 2, share: 0.2 },
    ])
  })

  it('puts Other and the terminals after real screens even when larger', () => {
    const got = neighbours(
      paths({
        sessions: 10,
        links: [
          { source: '0:Home', target: '1:Start', sessions: 6 },
          { source: '0:Home', target: '1:Other', sessions: 3 },
          { source: '0:Home', target: '1:Login', sessions: 1 },
        ],
      }),
    )
    expect(got.map((n) => n.screen)).toEqual(['Login', 'Other', 'Start'])
  })

  it('keeps screen names that contain a colon', () => {
    const got = neighbours(paths({ links: [{ source: '0:Home', target: '1:android://Cart', sessions: 4 }] }))
    expect(got).toEqual([{ screen: 'android://Cart', sessions: 4, share: 0.4 }])
  })

  it('is empty without data or sessions', () => {
    expect(neighbours(null)).toEqual([])
    expect(neighbours(paths({ sessions: 0, links: [{ source: '0:Home', target: '1:Cart', sessions: 0 }] }))).toEqual([])
  })
})

describe('neighbourLabel', () => {
  it('names the synthetic nodes and drops the platform scheme', () => {
    expect(neighbourLabel('Exit')).toBe('Left the app')
    expect(neighbourLabel('Start')).toBe('Session start')
    expect(neighbourLabel('Other')).toBe('Other screens')
    expect(neighbourLabel('android://MainActivity')).toBe('MainActivity')
  })
})

function row(screen: string, sessions: number, median: number, exits: number): FlowScreen {
  return { screen, platform: 'android', sessions, views: sessions, median_ms: median, p75_ms: median, entry_sessions: 0, exit_sessions: exits }
}

describe('sortScreens', () => {
  const rows = [row('android://B', 5, 3000, 1), row('android://a', 9, 1000, 9), row('android://C', 1, 9000, 0)]

  it('sorts by sessions, median and exit rate, descending or ascending', () => {
    expect(sortScreens(rows, 'sessions', 'desc').map((r) => r.screen)).toEqual(['android://a', 'android://B', 'android://C'])
    expect(sortScreens(rows, 'median', 'desc').map((r) => r.screen)).toEqual(['android://C', 'android://B', 'android://a'])
    expect(sortScreens(rows, 'exit', 'desc').map((r) => r.screen)).toEqual(['android://a', 'android://B', 'android://C'])
    expect(sortScreens(rows, 'sessions', 'asc').map((r) => r.screen)).toEqual(['android://C', 'android://B', 'android://a'])
  })

  it('sorts names by the displayed name, case-insensitively', () => {
    expect(sortScreens(rows, 'screen', 'asc').map((r) => r.screen)).toEqual(['android://a', 'android://B', 'android://C'])
  })

  it('does not mutate its input', () => {
    const copy = [...rows]
    sortScreens(rows, 'median', 'asc')
    expect(rows).toEqual(copy)
  })
})

describe('rateOf / exitTone', () => {
  it('computes a 0..1 rate and is 0 without sessions', () => {
    expect(rateOf(1, 4)).toBe(0.25)
    expect(rateOf(3, 0)).toBe(0)
  })
  it('flags high exit rates', () => {
    expect(exitTone(0.6)).toBe('high')
    expect(exitTone(0.5)).toBe('high')
    expect(exitTone(0.3)).toBe('mid')
    expect(exitTone(0.1)).toBe('low')
  })
})

describe('heatmapHref', () => {
  it('links to the heatmap with the raw screen name encoded', () => {
    expect(heatmapHref('android://Main Activity')).toBe('/dashboard/heatmap?screen=android%3A%2F%2FMain%20Activity')
  })
})
