export type ReplayViewState = 'skeleton' | 'onboarding' | 'workspace'

/**
 * Which replay page to show. The session list (with its filters) must stay on screen whenever a filter is
 * active, even if it matches nothing; otherwise the user is stranded with no way back to "All". The onboarding
 * card is only for a project that has no sessions at all.
 */
export function replayViewState(a: {
  loading: boolean
  sessionCount: number
  filter: string
  /** Active user search; like a filter, it must keep the list and its search box on screen. */
  userQuery?: string
  /** A requested session (deep link) keeps the workspace on screen so the player can resolve it, even with an empty list. */
  selectedId?: string
}): ReplayViewState {
  if (a.sessionCount > 0) return 'workspace'
  if (a.selectedId) return 'workspace'
  if (a.filter !== 'all' || a.userQuery) return 'workspace'
  return a.loading ? 'skeleton' : 'onboarding'
}

const FILTER_LABEL: Record<string, string> = {
  all: 'All',
  error: 'Errors',
  rage: 'Rage clicks',
  dead: 'Dead clicks',
}

/** The one sentence the empty replay panel shows, naming the filter that matched nothing. */
export function replayEmptyMessage(filter: string, loading: boolean, userQuery = ''): string {
  if (loading) return 'Loading sessions…'
  if (userQuery) {
    return filter === 'all'
      ? `No sessions for user “${userQuery}”`
      : `No sessions match “${FILTER_LABEL[filter] ?? filter}” for user “${userQuery}”`
  }
  if (filter === 'all') return 'No sessions yet'
  return `No sessions match “${FILTER_LABEL[filter] ?? filter}”`
}

export type ReplayDetailStatus = 'idle' | 'loading' | 'ok' | 'not_found' | 'error'

/** Only an HTTP 404 means the session does not exist; any other failure is a load error that can be retried. */
export function detailStatusForError(err: unknown): 'not_found' | 'error' {
  const status = typeof err === 'object' && err !== null ? (err as { status?: unknown }).status : undefined
  return status === 404 ? 'not_found' : 'error'
}

/**
 * Which session is selected after the list (re)loads. A selection that is on the page stays; a deep-linked one
 * that is not on this page stays too (it is loaded by id); anything else falls back to the first item.
 */
export function pickSelectedSession(cur: string, itemIds: string[], urlSession: string): string {
  if (itemIds.includes(cur)) return cur
  if (cur !== '' && cur === urlSession) return cur
  return itemIds[0] ?? ''
}

/**
 * The session the player shows. A selected id is never swapped for another session: if it is neither the loaded
 * detail nor on the current page, there is nothing to show yet (or it does not exist).
 */
export function resolveActiveSession<T extends { id: string }>(a: {
  detail: T | null
  detailStatus: ReplayDetailStatus
  selectedId: string
  list: T[]
}): T | null {
  if (a.detail && a.detail.id === a.selectedId) return a.detail
  const found = a.list.find((s) => s.id === a.selectedId)
  if (found) return found
  if (a.selectedId !== '') return null
  return a.list[0] ?? null
}
