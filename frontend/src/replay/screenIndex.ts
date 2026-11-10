import type { ParsedScreenChunk, ScreenFrameMeta, ScreenTouch } from './screen'

/** The time span one chunk covers (epoch ms), from its frames and touches. */
export type ChunkRange = { seq: number; startMs: number; endMs: number }

/**
 * Metadata index of a screen session, filled chunk by chunk in any order. It never holds image data.
 * `allSeqs` is every chunk_seq of the session; it lets `isResolved` tell "no frame here" from "not loaded yet".
 */
export class ScreenIndex {
  private readonly allSeqs: number[]
  private readonly stored = new Map<number, { chunk: ParsedScreenChunk; range: ChunkRange }>()
  private readonly skipped = new Set<number>()
  private framesCache: ScreenFrameMeta[] | null = null
  private touchesCache: ScreenTouch[] | null = null

  constructor(allSeqs: number[]) {
    this.allSeqs = [...new Set(allSeqs)].sort((a, b) => a - b)
  }

  /** Idempotent per seq: a second chunk with the same seq is ignored. */
  addChunk(c: ParsedScreenChunk): void {
    if (this.stored.has(c.seq)) return
    const stamps = [...c.frames.map((f) => f.ts), ...c.touches.map((t) => t.ts)]
    if (stamps.length === 0) return
    this.skipped.delete(c.seq)
    this.stored.set(c.seq, {
      chunk: c,
      range: { seq: c.seq, startMs: Math.min(...stamps), endMs: Math.max(...stamps) },
    })
    this.framesCache = null
    this.touchesCache = null
  }

  /** Marks a chunk that holds no screen content (e.g. a wireframe chunk of a mixed session). */
  markSkipped(seq: number): void {
    if (!this.stored.has(seq)) this.skipped.add(seq)
  }

  /** Whether the chunk is known: indexed or skipped. */
  has(seq: number): boolean {
    return this.stored.has(seq) || this.skipped.has(seq)
  }

  /** Frames sorted by ts; when two frames share a ts the one from the higher chunk seq wins. */
  get frames(): ScreenFrameMeta[] {
    if (!this.framesCache) {
      const all = [...this.stored.values()].flatMap((s) => s.chunk.frames)
      all.sort((a, b) => a.ts - b.ts || a.chunkSeq - b.chunkSeq || a.index - b.index)
      const out: ScreenFrameMeta[] = []
      for (const f of all) {
        if (out.length > 0 && out[out.length - 1].ts === f.ts) out[out.length - 1] = f
        else out.push(f)
      }
      this.framesCache = out
    }
    return this.framesCache
  }

  get touches(): ScreenTouch[] {
    if (!this.touchesCache) {
      this.touchesCache = [...this.stored.values()].flatMap((s) => s.chunk.touches).sort((a, b) => a.ts - b.ts)
    }
    return this.touchesCache
  }

  get startMs(): number {
    return this.frames[0]?.ts ?? this.touches[0]?.ts ?? 0
  }

  get endMs(): number {
    const lastFrame = this.frames[this.frames.length - 1]?.ts ?? -Infinity
    const lastTouch = this.touches[this.touches.length - 1]?.ts ?? -Infinity
    const end = Math.max(lastFrame, lastTouch)
    return Number.isFinite(end) ? end : 0
  }

  get platform(): 'android' | 'ios' | null {
    for (const seq of [...this.stored.keys()].sort((a, b) => a - b)) {
      const p = this.stored.get(seq)!.chunk.platform
      if (p) return p
    }
    return null
  }

  ranges(): ChunkRange[] {
    return [...this.stored.values()].map((s) => s.range).sort((a, b) => a.seq - b.seq)
  }

  /**
   * True when the frame shown at `ts` is known for certain: `ts` lies inside a loaded chunk, or between two
   * loaded chunks that are neighbours in the session (skipped chunks do not count), or before the first /
   * after the last loaded chunk when that chunk is the session's first / last.
   */
  isResolved(ts: number): boolean {
    const rs = this.ranges()
    if (rs.length === 0) return false
    if (rs.some((r) => ts >= r.startMs && ts <= r.endMs)) return true

    let prev: ChunkRange | null = null
    let next: ChunkRange | null = null
    for (const r of rs) {
      if (r.endMs < ts) prev = r
      else if (!next) next = r
    }
    const effective = this.allSeqs.filter((s) => !this.skipped.has(s))
    const pos = new Map(effective.map((s, i) => [s, i]))
    if (!prev) return pos.get(next!.seq) === 0
    if (!next) return pos.get(prev.seq) === effective.length - 1
    const p = pos.get(prev.seq)
    return p !== undefined && pos.get(next.seq) === p + 1
  }
}
