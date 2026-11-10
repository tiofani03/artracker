import { parseScreenChunk, screenFrameData, type ScreenFrameMeta } from './screen'
import { ScreenIndex } from './screenIndex'
import { ChunkCache, chunkSeqsInWindow } from './screenWindow'

export type ScreenSessionDeps = {
  /** Every chunk_seq of the session. */
  seqs: number[]
  sessionStartMs: number
  /** When each chunk was acknowledged (epoch ms); used to guess which chunk covers a time nobody loaded yet. */
  chunkCreatedAtMs: Map<number, number>
  fetchChunk: (seq: number, signal?: AbortSignal) => Promise<unknown | null>
  /** Clock for retry back-off; defaults to Date.now. */
  now?: () => number
}

const SCAN_CONCURRENCY = 3
/** A chunk whose download failed is not requested again for this long. */
const RETRY_BACKOFF_MS = 2_000

/**
 * Loads a screen session progressively. A scan pass indexes every chunk (metadata only); raw payloads, which
 * hold the base64 images, stay resident only for chunks inside the playback window around the playhead.
 * Framework-free so it can be unit-tested; the React player subscribes to it.
 */
export class ScreenReplaySession {
  readonly index: ScreenIndex
  private readonly deps: ScreenSessionDeps
  private readonly seqs: number[]
  private readonly resident = new ChunkCache<unknown>()
  private readonly listeners = new Set<() => void>()
  private readonly inflight = new Map<number, Promise<void>>()
  private readonly failedAt = new Map<number, number>()
  private playhead: number | null = null
  private signal: AbortSignal | undefined
  private versionCounter = 0

  constructor(deps: ScreenSessionDeps) {
    this.deps = deps
    this.seqs = [...deps.seqs].sort((a, b) => a - b)
    this.index = new ScreenIndex(this.seqs)
  }

  subscribe(fn: () => void): () => void {
    this.listeners.add(fn)
    return () => {
      this.listeners.delete(fn)
    }
  }

  /** Increases on every notification; a stable snapshot value for `useSyncExternalStore`. */
  get version(): number {
    return this.versionCounter
  }

  residentSeqs(): number[] {
    return this.resident.keys()
  }

  /** Resolves when no chunk request is in flight. */
  async whenIdle(): Promise<void> {
    while (this.inflight.size > 0) await Promise.all([...this.inflight.values()])
  }

  /** Scans every chunk not known yet (concurrency 3). Failed chunks are retried by `setPlayhead`. */
  async start(signal?: AbortSignal): Promise<void> {
    this.signal = signal
    const queue = this.seqs.filter((s) => !this.index.has(s))
    const worker = async () => {
      while (queue.length > 0 && !signal?.aborted) {
        const seq = queue.shift()!
        if (this.index.has(seq)) continue
        await this.load(seq, false)
      }
    }
    await Promise.all(Array.from({ length: SCAN_CONCURRENCY }, worker))
  }

  /** Moves the playhead (epoch ms): trims the resident window, fetches missing window chunks and, when the
   *  time is not covered by loaded chunks, the chunk estimated to contain it. Never notifies. */
  setPlayhead(absMs: number): void {
    this.playhead = absMs
    const windowSeqs = chunkSeqsInWindow(this.index.ranges(), absMs)
    this.resident.retainOnly(windowSeqs)
    for (const seq of windowSeqs) {
      if (this.resident.get(seq) === undefined) this.request(seq)
    }
    if (!this.index.isResolved(absMs)) {
      const est = this.estimateSeq(absMs)
      if (est !== null && !this.index.has(est)) this.request(est)
    }
  }

  /** Base64 image of a frame when its chunk is resident; otherwise null and the chunk is requested. */
  frameImage(f: ScreenFrameMeta): string | null {
    const payload = this.resident.get(f.chunkSeq)
    if (payload !== undefined) return screenFrameData(payload, f.index)
    this.request(f.chunkSeq)
    return null
  }

  private now(): number {
    return (this.deps.now ?? Date.now)()
  }

  private request(seq: number): void {
    if (this.inflight.has(seq)) return
    const failed = this.failedAt.get(seq)
    if (failed !== undefined && this.now() - failed < RETRY_BACKOFF_MS) return
    void this.load(seq, true)
  }

  /** First chunk acknowledged at or after `absMs`, else the last chunk. */
  private estimateSeq(absMs: number): number | null {
    for (const seq of this.seqs) {
      const at = this.deps.chunkCreatedAtMs.get(seq)
      if (at !== undefined && at >= absMs) return seq
    }
    return this.seqs.length > 0 ? this.seqs[this.seqs.length - 1] : null
  }

  private load(seq: number, force: boolean): Promise<void> {
    const existing = this.inflight.get(seq)
    if (existing) return existing
    const p = (async () => {
      let payload: unknown | null
      try {
        payload = await this.deps.fetchChunk(seq, this.signal)
      } catch {
        return // aborted
      }
      if (payload == null) {
        this.failedAt.set(seq, this.now())
        return
      }
      this.failedAt.delete(seq)
      const parsed = parseScreenChunk(payload, seq)
      if (!parsed) {
        this.index.markSkipped(seq)
        this.notify()
        return
      }
      this.index.addChunk(parsed)
      if (force || this.inWindow(seq)) this.resident.set(seq, payload)
      this.notify()
    })().finally(() => {
      this.inflight.delete(seq)
    })
    this.inflight.set(seq, p)
    return p
  }

  private inWindow(seq: number): boolean {
    const center = this.playhead ?? this.index.startMs
    return chunkSeqsInWindow(this.index.ranges(), center).includes(seq)
  }

  private notify(): void {
    this.versionCounter++
    for (const fn of [...this.listeners]) fn()
  }
}
