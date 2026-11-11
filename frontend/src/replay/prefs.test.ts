import { describe, expect, it } from 'vitest'
import {
  defaultQueueOpen,
  readBool,
  readStringSet,
  writeBool,
  writeStringSet,
  type KV,
} from './prefs'

function memoryKV(): KV & { data: Map<string, string> } {
  const data = new Map<string, string>()
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
  }
}

const throwingKV: KV = {
  getItem: () => {
    throw new Error('storage blocked')
  },
  setItem: () => {
    throw new Error('storage blocked')
  },
}

describe('bool prefs', () => {
  it('round-trips true and false', () => {
    const kv = memoryKV()
    writeBool(kv, 'k', true)
    expect(readBool(kv, 'k', false)).toBe(true)
    writeBool(kv, 'k', false)
    expect(readBool(kv, 'k', true)).toBe(false)
  })

  it('returns the fallback when nothing is stored', () => {
    expect(readBool(memoryKV(), 'missing', true)).toBe(true)
    expect(readBool(memoryKV(), 'missing', false)).toBe(false)
  })

  it('returns the fallback for an unrecognised stored value', () => {
    const kv = memoryKV()
    kv.data.set('k', 'banana')
    expect(readBool(kv, 'k', true)).toBe(true)
  })

  it('survives blocked storage and a null KV', () => {
    expect(readBool(throwingKV, 'k', true)).toBe(true)
    expect(() => writeBool(throwingKV, 'k', true)).not.toThrow()
    expect(readBool(null, 'k', false)).toBe(false)
    expect(() => writeBool(null, 'k', true)).not.toThrow()
  })
})

describe('string set prefs', () => {
  it('round-trips a set', () => {
    const kv = memoryKV()
    writeStringSet(kv, 'k', new Set(['cpu', 'mem']))
    expect(readStringSet(kv, 'k')).toEqual(new Set(['cpu', 'mem']))
  })

  it('is empty when missing, corrupt, or not an array of strings', () => {
    const kv = memoryKV()
    expect(readStringSet(kv, 'k')).toEqual(new Set())
    kv.data.set('k', '{not json')
    expect(readStringSet(kv, 'k')).toEqual(new Set())
    kv.data.set('k', '{"a":1}')
    expect(readStringSet(kv, 'k')).toEqual(new Set())
    kv.data.set('k', '["cpu", 3, null]')
    expect(readStringSet(kv, 'k')).toEqual(new Set(['cpu']))
  })

  it('survives blocked storage and a null KV', () => {
    expect(readStringSet(throwingKV, 'k')).toEqual(new Set())
    expect(() => writeStringSet(throwingKV, 'k', new Set(['a']))).not.toThrow()
    expect(readStringSet(null, 'k')).toEqual(new Set())
  })
})

describe('defaultQueueOpen', () => {
  it('opens the session rail only on wide viewports', () => {
    expect(defaultQueueOpen(1599)).toBe(false)
    expect(defaultQueueOpen(1600)).toBe(true)
  })
})
