import { describe, expect, it } from 'vitest'
import { readWorkspaceFromSearch, stripWorkspaceParams } from './deepLink'

describe('readWorkspaceFromSearch', () => {
  it('reads org and project', () => {
    expect(readWorkspaceFromSearch('?org=3&project=12')).toEqual({ org: 3, project: 12 })
  })

  it('ignores non-numeric values', () => {
    expect(readWorkspaceFromSearch('?project=abc')).toEqual({ org: null, project: null })
  })

  it('returns nulls for an empty query', () => {
    expect(readWorkspaceFromSearch('')).toEqual({ org: null, project: null })
  })

  it('accepts positive integers only', () => {
    expect(readWorkspaceFromSearch('?org=-1&project=0')).toEqual({ org: null, project: null })
  })
})

describe('stripWorkspaceParams', () => {
  it('strips only org and project', () => {
    expect(stripWorkspaceParams('?org=3&session=abc&project=12&demo=1')).toBe('?session=abc&demo=1')
  })

  it('returns an empty string when only org/project were present', () => {
    expect(stripWorkspaceParams('?org=3&project=12')).toBe('')
  })

  it('leaves an unrelated query unchanged', () => {
    expect(stripWorkspaceParams('?session=abc&demo=1')).toBe('?session=abc&demo=1')
  })
})
