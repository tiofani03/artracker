import { describe, expect, it } from 'vitest'
import { replayViewState } from './viewState'

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

  it('shows the skeleton only for the first load with no filter', () => {
    expect(s(true, 0, 'all')).toBe('skeleton')
  })

  it('shows onboarding only when the project truly has no sessions', () => {
    expect(s(false, 0, 'all')).toBe('onboarding')
  })
})
