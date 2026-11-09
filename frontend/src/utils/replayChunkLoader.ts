import type { ReplayChunkDetail } from '../api'
import { fetchChunkPayloads } from '../replay/parseChunk'

export type ChunkLoadResult = { payloads: unknown[]; failed: number }

let sharedWorker: Worker | null | undefined
let nextReqId = 1
const pendingMap = new Map<
  number,
  { resolve: (r: ChunkLoadResult) => void; reject: (err: Error) => void }
>()

function getDecompressWorker(): Worker | null {
  if (sharedWorker !== undefined) return sharedWorker
  if (typeof Worker === 'undefined') {
    sharedWorker = null
    return null
  }
  try {
    const w = new Worker(new URL('./replayChunkWorker.ts', import.meta.url), { type: 'module' })
    w.onmessage = (
      ev: MessageEvent<
        | { id: number; ok: true; payloads: unknown[]; failed: number }
        | { id: number; ok: false; error: string }
      >,
    ) => {
      const entry = pendingMap.get(ev.data.id)
      if (!entry) return
      pendingMap.delete(ev.data.id)
      if (ev.data.ok) entry.resolve({ payloads: ev.data.payloads, failed: ev.data.failed })
      else entry.reject(new Error(ev.data.error))
    }
    w.onerror = () => {
      for (const [, entry] of pendingMap) entry.reject(new Error('Replay decompression worker failed'))
      pendingMap.clear()
      sharedWorker = null
    }
    sharedWorker = w
    return w
  } catch {
    sharedWorker = null
    return null
  }
}

/**
 * Downloads every chunk (ordered by chunk_seq) and returns the raw parsed JSON payloads.
 * Interpretation (rrweb vs mobile frames) happens in `buildReplayModel`. Unreadable chunks
 * are `null` and counted in `failed`; the promise only rejects when every chunk fails.
 */
export async function loadChunkPayloads(
  chunks: ReplayChunkDetail[],
  signal?: AbortSignal,
): Promise<ChunkLoadResult> {
  const urls = [...chunks]
    .sort((a, b) => a.chunk_seq - b.chunk_seq)
    .map((c) => c.url)
    .filter((u): u is string => Boolean(u && u.trim()))

  if (urls.length === 0) return { payloads: [], failed: 0 }

  const worker = getDecompressWorker()
  if (!worker) return fetchChunkPayloads(urls, signal)

  return new Promise<ChunkLoadResult>((resolve, reject) => {
    const reqId = nextReqId++
    const onAbort = () => {
      pendingMap.delete(reqId)
      reject(new DOMException('Aborted', 'AbortError'))
    }
    if (signal?.aborted) {
      onAbort()
      return
    }
    signal?.addEventListener('abort', onAbort, { once: true })
    pendingMap.set(reqId, {
      resolve: (r) => {
        signal?.removeEventListener('abort', onAbort)
        resolve(r)
      },
      reject: (err) => {
        signal?.removeEventListener('abort', onAbort)
        reject(err)
      },
    })
    worker.postMessage({ id: reqId, urls })
  })
}
