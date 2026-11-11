export type MarkerCategory = 'error' | 'network' | 'gesture' | 'perf' | 'other'
export type MarkerFilter = 'all' | 'error' | 'network' | 'gesture' | 'perf'

export function filterMarkers<T extends { category: MarkerCategory }>(markers: T[], filter: MarkerFilter): T[] {
  return filter === 'all' ? markers : markers.filter((m) => m.category === filter)
}

export function countByFilter(markers: { category: MarkerCategory }[]): Record<MarkerFilter, number> {
  const counts: Record<MarkerFilter, number> = { all: markers.length, error: 0, network: 0, gesture: 0, perf: 0 }
  for (const m of markers) {
    if (m.category !== 'other') counts[m.category] += 1
  }
  return counts
}

const CATEGORY_FILTERS: MarkerFilter[] = ['error', 'network', 'gesture', 'perf']

/** Filter chips worth showing: none until at least two categories exist, then All + each non-empty category. */
export function selectableFilters(counts: Record<MarkerFilter, number>): MarkerFilter[] {
  const present = CATEGORY_FILTERS.filter((f) => counts[f] > 0)
  return present.length < 2 ? [] : ['all', ...present]
}
