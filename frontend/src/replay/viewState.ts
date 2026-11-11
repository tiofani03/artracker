export type ReplayViewState = 'skeleton' | 'onboarding' | 'workspace'

/**
 * Which replay page to show. The session list (with its filters) must stay on screen whenever a filter is
 * active, even if it matches nothing; otherwise the user is stranded with no way back to "All". The onboarding
 * card is only for a project that has no sessions at all.
 */
export function replayViewState(a: { loading: boolean; sessionCount: number; filter: string }): ReplayViewState {
  if (a.sessionCount > 0) return 'workspace'
  if (a.filter !== 'all') return 'workspace'
  return a.loading ? 'skeleton' : 'onboarding'
}

const FILTER_LABEL: Record<string, string> = {
  all: 'All',
  error: 'Errors',
  rage: 'Rage clicks',
  dead: 'Dead clicks',
}

/** The one sentence the empty replay panel shows, naming the filter that matched nothing. */
export function replayEmptyMessage(filter: string, loading: boolean): string {
  if (loading) return 'Loading sessions…'
  if (filter === 'all') return 'No sessions yet'
  return `No sessions match “${FILTER_LABEL[filter] ?? filter}”`
}
