import type { ChunkRange } from './screenIndex'

/** Chunks overlapping `[playheadMs - behindMs, playheadMs + aheadMs]`, ascending by seq. */
export function chunkSeqsInWindow(
  ranges: ChunkRange[],
  playheadMs: number,
  behindMs = 10_000,
  aheadMs = 30_000,
): number[] {
  const from = playheadMs - behindMs
  const to = playheadMs + aheadMs
  return ranges
    .filter((r) => r.endMs >= from && r.startMs <= to)
    .map((r) => r.seq)
    .sort((a, b) => a - b)
}

/** Resident chunk payloads by seq; the owner trims it to the playback window with `retainOnly`. */
export class ChunkCache<T> {
  private readonly map = new Map<number, T>()

  get(seq: number): T | undefined {
    return this.map.get(seq)
  }

  set(seq: number, value: T): void {
    this.map.set(seq, value)
  }

  /** Drops every entry whose seq is not in `seqs`; returns the dropped seqs. */
  retainOnly(seqs: Iterable<number>): number[] {
    const keep = new Set(seqs)
    const evicted: number[] = []
    for (const seq of [...this.map.keys()]) {
      if (!keep.has(seq)) {
        this.map.delete(seq)
        evicted.push(seq)
      }
    }
    return evicted
  }

  keys(): number[] {
    return [...this.map.keys()].sort((a, b) => a - b)
  }

  get size(): number {
    return this.map.size
  }
}
