import { describe, expect, it } from 'vitest'
import { countByFilter, filterMarkers, selectableFilters } from './markerFilter'

const m = (id: string, category: 'error' | 'network' | 'gesture' | 'perf' | 'other') => ({ id, category })

const markers = [m('a', 'error'), m('b', 'network'), m('c', 'gesture'), m('d', 'network'), m('e', 'perf'), m('f', 'other')]

describe('filterMarkers', () => {
  it('all returns every marker, including uncategorised ones', () => {
    expect(filterMarkers(markers, 'all').map((x) => x.id)).toEqual(['a', 'b', 'c', 'd', 'e', 'f'])
  })

  it('returns only the matching category, keeping order', () => {
    expect(filterMarkers(markers, 'network').map((x) => x.id)).toEqual(['b', 'd'])
    expect(filterMarkers(markers, 'perf').map((x) => x.id)).toEqual(['e'])
  })
})

describe('countByFilter', () => {
  it('counts per filter', () => {
    expect(countByFilter(markers)).toEqual({ all: 6, error: 1, network: 2, gesture: 1, perf: 1 })
  })

  it('is all zero for no markers', () => {
    expect(countByFilter([])).toEqual({ all: 0, error: 0, network: 0, gesture: 0, perf: 0 })
  })
})

describe('selectableFilters', () => {
  it('offers no chips when at most one category exists (they would only repeat All)', () => {
    expect(selectableFilters(countByFilter([]))).toEqual([])
    expect(selectableFilters(countByFilter([m('a', 'perf'), m('b', 'perf')]))).toEqual([])
  })

  it('offers All plus every non-empty category once there are two or more', () => {
    const counts = countByFilter([m('a', 'error'), m('b', 'network'), m('c', 'network'), m('d', 'other')])
    expect(selectableFilters(counts)).toEqual(['all', 'error', 'network'])
  })
})
