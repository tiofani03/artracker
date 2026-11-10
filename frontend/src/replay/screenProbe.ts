import type { ReplayChunkDetail } from '../api'
import { isScreenSession } from './screen'
import { ScreenReplaySession } from './screenSession'

/**
 * Decides whether a session is a screenshot session by fetching only its first and last chunk (a session that
 * starts in wireframe mode and switches to screenshots, or the reverse, has one of each). Returns the
 * session ready to `start()`, or null for rrweb / wireframe sessions and when the probes are unreadable.
 * Only chunks that have a download URL take part.
 */
export async function probeScreenSession(input: {
  chunks: ReplayChunkDetail[]
  sessionStartMs: number
  fetchChunk: (seq: number, signal?: AbortSignal) => Promise<unknown | null>
  signal?: AbortSignal
}): Promise<ScreenReplaySession | null> {
  const fetchable = input.chunks
    .filter((c) => c.url && c.url.trim())
    .sort((a, b) => a.chunk_seq - b.chunk_seq)
  if (fetchable.length === 0) return null

  const seqs = fetchable.map((c) => c.chunk_seq)
  const probeSeqs = seqs.length === 1 ? [seqs[0]] : [seqs[0], seqs[seqs.length - 1]]
  const probes = (await Promise.all(probeSeqs.map((seq) => input.fetchChunk(seq, input.signal)))).filter(
    (p): p is NonNullable<typeof p> => p != null,
  )
  if (!isScreenSession(probes)) return null

  const createdAt = new Map<number, number>()
  for (const c of fetchable) {
    const t = Date.parse(c.created_at)
    if (Number.isFinite(t)) createdAt.set(c.chunk_seq, t)
  }
  return new ScreenReplaySession({
    seqs,
    sessionStartMs: input.sessionStartMs,
    chunkCreatedAtMs: createdAt,
    fetchChunk: input.fetchChunk,
  })
}
