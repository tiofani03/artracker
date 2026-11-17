import { describe, expect, it } from 'vitest'
import { detailStatusForError, pickSelectedSession, replayEmptyMessage, replayViewState, resolveActiveSession } from './viewState'

const s = (loading: boolean, sessionCount: number, filter: string) => replayViewState({ loading, sessionCount, filter })

describe('replayViewState', () => {
  it('shows the workspace whenever there are sessions', () => {
    expect(s(false, 6, 'all')).toBe('workspace')
    expect(s(true, 6, 'error')).toBe('workspace')
  })

  it('keeps the workspace (and its filters) when a filter matches nothing, so the user can switch back', () => {
    expect(s(false, 0, 'error')).toBe('workspace')
    expect(s(false, 0, 'rage')).toBe('workspace')
    expect(s(false, 0, 'dead')).toBe('workspace')
  })

  it('keeps the workspace while a non-default filter is loading from an empty result', () => {
    expect(s(true, 0, 'dead')).toBe('workspace')
  })

  it('keeps the workspace for a requested session even when the list is empty', () => {
    expect(replayViewState({ loading: false, sessionCount: 0, filter: 'all', selectedId: 'x' })).toBe('workspace')
    expect(replayViewState({ loading: true, sessionCount: 0, filter: 'all', selectedId: 'x' })).toBe('workspace')
    expect(replayViewState({ loading: false, sessionCount: 0, filter: 'all', selectedId: '' })).toBe('onboarding')
  })

  it('shows the skeleton only for the first load with no filter', () => {
    expect(s(true, 0, 'all')).toBe('skeleton')
  })

  it('shows onboarding only when the project truly has no sessions', () => {
    expect(s(false, 0, 'all')).toBe('onboarding')
  })
})

describe('replayEmptyMessage', () => {
  it('names the filter that matched nothing', () => {
    expect(replayEmptyMessage('error', false)).toBe('No sessions match “Errors”')
    expect(replayEmptyMessage('rage', false)).toBe('No sessions match “Rage clicks”')
    expect(replayEmptyMessage('dead', false)).toBe('No sessions match “Dead clicks”')
  })

  it('says loading while a request is in flight', () => {
    expect(replayEmptyMessage('error', true)).toBe('Loading sessions…')
  })

  it('has a generic message for the unfiltered case', () => {
    expect(replayEmptyMessage('all', false)).toBe('No sessions yet')
  })
})

describe('replayEmptyMessage with a user search', () => {
  it('names the user search when there is no other filter', () => {
    expect(replayEmptyMessage('all', false, 'user_4')).toBe('No sessions for user “user_4”')
  })

  it('names both when a filter and a search are active', () => {
    expect(replayEmptyMessage('error', false, 'user_4')).toBe('No sessions match “Errors” for user “user_4”')
  })

  it('still says loading first', () => {
    expect(replayEmptyMessage('error', true, 'user_4')).toBe('Loading sessions…')
  })
})

describe('replayViewState with a user search', () => {
  it('keeps the workspace so the search box stays on screen', () => {
    expect(replayViewState({ loading: false, sessionCount: 0, filter: 'all', userQuery: 'user_4' })).toBe('workspace')
  })
})

describe('pickSelectedSession', () => {
  it('keeps a selection that is on the page', () => expect(pickSelectedSession('b', ['a', 'b'], '')).toBe('b'))
  it('keeps a deep-linked session that is not on the page', () => expect(pickSelectedSession('x', ['a', 'b'], 'x')).toBe('x'))
  it('falls back to the first item only when nothing explicit was asked for', () => {
    expect(pickSelectedSession('', ['a', 'b'], '')).toBe('a')
    expect(pickSelectedSession('x', ['a', 'b'], '')).toBe('a') // stale selection after a filter change
  })
  it('returns empty for an empty page with no deep link', () => expect(pickSelectedSession('', [], '')).toBe(''))
})

describe('resolveActiveSession', () => {
  const a = { id: 'a' }, b = { id: 'b' }
  it('prefers the loaded detail', () => expect(resolveActiveSession({ detail: b, detailStatus: 'ok', selectedId: 'b', list: [a] })).toBe(b))
  it('uses the list item while detail loads', () => expect(resolveActiveSession({ detail: null, detailStatus: 'loading', selectedId: 'a', list: [a, b] })).toBe(a))
  it('never substitutes another session for a selected id', () => {
    expect(resolveActiveSession({ detail: null, detailStatus: 'not_found', selectedId: 'zzz', list: [a, b] })).toBeNull()
    expect(resolveActiveSession({ detail: null, detailStatus: 'loading', selectedId: 'zzz', list: [a, b] })).toBeNull()
    expect(resolveActiveSession({ detail: null, detailStatus: 'error', selectedId: 'zzz', list: [a, b] })).toBeNull()
  })
  it('defaults to the first item when nothing is selected', () => expect(resolveActiveSession({ detail: null, detailStatus: 'idle', selectedId: '', list: [a, b] })).toBe(a))
})

describe('detailStatusForError', () => {
  it('maps only an HTTP 404 to not_found', () => {
    expect(detailStatusForError({ status: 404 })).toBe('not_found')
  })
  it('treats server errors, other statuses and non-HTTP failures as a load error', () => {
    expect(detailStatusForError({ status: 500 })).toBe('error')
    expect(detailStatusForError({ status: 403 })).toBe('error')
    expect(detailStatusForError(new Error('Failed to fetch'))).toBe('error')
    expect(detailStatusForError('404 not found')).toBe('error')
    expect(detailStatusForError(null)).toBe('error')
  })
})
