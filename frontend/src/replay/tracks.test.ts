import { describe, expect, it } from 'vitest'
import { mergeScreenRuns, overflowCount, seriesPath, TRACK_ORDER, visibleTracks, type TrackDef } from './tracks'

const def = (id: TrackDef['id']): TrackDef => ({ id, label: id, kind: 'spans' })

describe('visibleTracks', () => {
  it('orders by TRACK_ORDER regardless of input order', () => {
    const out = visibleTracks([def('events'), def('cpu'), def('screens')], new Set())
    expect(out.map((d) => d.id)).toEqual(['screens', 'cpu', 'events'])
  })

  it('drops hidden tracks', () => {
    const out = visibleTracks([def('screens'), def('cpu'), def('events')], new Set(['cpu']))
    expect(out.map((d) => d.id)).toEqual(['screens', 'events'])
  })

  it('never hides the events track', () => {
    const out = visibleTracks([def('cpu'), def('events')], new Set(['events', 'cpu']))
    expect(out.map((d) => d.id)).toEqual(['events'])
  })

  it('exposes the documented order', () => {
    expect(TRACK_ORDER).toEqual(['screens', 'network', 'cpu', 'mem', 'fps', 'events'])
  })
})

describe('seriesPath', () => {
  it('maps points into the box with y inverted', () => {
    expect(
      seriesPath(
        [
          { tMs: 0, v: 0 },
          { tMs: 1000, v: 50 },
        ],
        1000,
        100,
        20,
        100,
      ),
    ).toBe('M0 20 L100 10')
  })

  it('starts a new subpath after a gap larger than 2.5 x the median step', () => {
    const path = seriesPath(
      [
        { tMs: 0, v: 10 },
        { tMs: 1000, v: 10 },
        { tMs: 2000, v: 10 },
        { tMs: 9000, v: 10 },
      ],
      10000,
      100,
      20,
      100,
    )
    expect(path.match(/M/g)).toHaveLength(2)
  })

  it('does not split evenly spaced points', () => {
    const pts = [0, 1000, 2000, 3000].map((tMs) => ({ tMs, v: 5 }))
    expect(seriesPath(pts, 3000, 90, 10, 10).match(/M/g)).toHaveLength(1)
  })

  it('clamps values to the box', () => {
    expect(seriesPath([{ tMs: 0, v: 500 }, { tMs: 1000, v: -5 }], 1000, 100, 20, 100)).toBe('M0 0 L100 20')
  })

  it('draws an isolated point as a zero-length segment so it can render as a dot', () => {
    expect(seriesPath([{ tMs: 500, v: 100 }], 1000, 100, 10, 100)).toBe('M50 0 L50 0')
  })

  it('is empty for no points or a zero duration', () => {
    expect(seriesPath([], 1000, 100, 20, 100)).toBe('')
    expect(seriesPath([{ tMs: 0, v: 1 }], 0, 100, 20, 100)).toBe('')
  })
})

describe('overflowCount', () => {
  it('is the number of hidden items', () => {
    expect(overflowCount(20, 15)).toBe(5)
    expect(overflowCount(3, 15)).toBe(0)
  })
})

describe('mergeScreenRuns', () => {
  it('collapses consecutive frames of the same screen into one span', () => {
    const merged = mergeScreenRuns([
      { screen: 'home', startMs: 0, endMs: 500 },
      { screen: 'home', startMs: 500, endMs: 1000 },
      { screen: 'home', startMs: 1000, endMs: 1500 },
      { screen: 'detail', startMs: 1500, endMs: 2000 },
      { screen: 'detail', startMs: 2000, endMs: 4000 },
    ])
    expect(merged).toEqual([
      { screen: 'home', startMs: 0, endMs: 1500 },
      { screen: 'detail', startMs: 1500, endMs: 4000 },
    ])
  })

  it('keeps a screen that is revisited as a separate span', () => {
    const merged = mergeScreenRuns([
      { screen: 'home', startMs: 0, endMs: 500 },
      { screen: 'detail', startMs: 500, endMs: 1000 },
      { screen: 'home', startMs: 1000, endMs: 1500 },
    ])
    expect(merged.map((s) => s.screen)).toEqual(['home', 'detail', 'home'])
  })

  it('handles empty input', () => {
    expect(mergeScreenRuns([])).toEqual([])
  })
})
