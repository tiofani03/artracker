import { describe, expect, it } from 'vitest'
import { ChunkCache, chunkSeqsInWindow } from './screenWindow'
import type { ChunkRange } from './screenIndex'

// five 10 s chunks: [0,9500] [10000,19500] ... [40000,49500]
const ranges: ChunkRange[] = [0, 1, 2, 3, 4].map((i) => ({ seq: i, startMs: i * 10_000, endMs: i * 10_000 + 9_500 }))

describe('chunkSeqsInWindow', () => {
  it('returns the chunks overlapping [playhead - 10 s, playhead + 30 s], ascending', () => {
    expect(chunkSeqsInWindow(ranges, 25_000)).toEqual([1, 2, 3, 4])
    expect(chunkSeqsInWindow(ranges, 0)).toEqual([0, 1, 2, 3])
    expect(chunkSeqsInWindow(ranges, 100_000)).toEqual([])
  })

  it('honours custom behind / ahead', () => {
    expect(chunkSeqsInWindow(ranges, 25_000, 0, 0)).toEqual([2])
    expect(chunkSeqsInWindow(ranges, 25_000, 5_000, 5_000)).toEqual([2, 3])
  })

  it('sorts by seq even when the ranges are unsorted', () => {
    expect(chunkSeqsInWindow([...ranges].reverse(), 25_000, 0, 0)).toEqual([2])
  })
})

describe('ChunkCache', () => {
  it('retainOnly evicts everything outside the kept seqs and returns the evicted seqs', () => {
    const cache = new ChunkCache<string>()
    cache.set(1, 'a')
    cache.set(2, 'b')
    cache.set(3, 'c')
    expect(cache.retainOnly([2, 3])).toEqual([1])
    expect(cache.get(1)).toBeUndefined()
    expect(cache.get(2)).toBe('b')
    expect(cache.size).toBe(2)
  })
})
