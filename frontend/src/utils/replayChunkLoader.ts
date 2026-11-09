import type { eventWithTime } from 'rrweb'
import type { ReplayChunkDetail } from '../api'

async function gunzipBufferMainThread(buf: ArrayBuffer): Promise<string> {
  const bytes = new Uint8Array(buf)
  if (bytes.byteLength >= 2 && bytes[0] === 0x1f && bytes[1] === 0x8b) {
    try {
      const ds = new DecompressionStream('gzip')
      const decompressedStream = new Response(buf).body!.pipeThrough(ds)
      return await new Response(decompressedStream).text()
    } catch {
      // fallback
    }
  }
  return new TextDecoder().decode(bytes)
}

async function fetchAndDecompressOnMainThread(
  urls: string[],
  signal?: AbortSignal,
): Promise<eventWithTime[]> {
  const parts = await Promise.all(
    urls.map(async (rawUrl) => {
      let url = rawUrl
      try {
        const u = new URL(rawUrl)
        if (u.pathname.includes('/api/v1/replay/download-chunk')) {
          url = `${u.pathname}${u.search}`
        }
      } catch {
        // keep original
      }
      const res = await fetch(url, { signal })
      if (!res.ok) {
        throw new Error(`Chunk fetch failed (${res.status} ${res.statusText})`)
      }
      const buf = await res.arrayBuffer()
      const text = await gunzipBufferMainThread(buf)
      const parsed: unknown = JSON.parse(text)
      if (Array.isArray(parsed)) return parsed as eventWithTime[]
      if (
        parsed &&
        typeof parsed === 'object' &&
        'events' in parsed &&
        Array.isArray((parsed as { events: unknown }).events)
      ) {
        return (parsed as { events: eventWithTime[] }).events
      }
      return []
    }),
  )
  const merged = parts.flat()
  merged.sort((a, b) => a.timestamp - b.timestamp)
  return merged
}

let sharedWorker: Worker | null | undefined
let nextReqId = 1
const pendingMap = new Map<
  number,
  { resolve: (events: eventWithTime[]) => void; reject: (err: Error) => void }
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
        | { id: number; ok: true; events: eventWithTime[] }
        | { id: number; ok: false; error: string }
      >,
    ) => {
      const entry = pendingMap.get(ev.data.id)
      if (!entry) return
      pendingMap.delete(ev.data.id)
      if (ev.data.ok) {
        entry.resolve(ev.data.events)
      } else {
        entry.reject(new Error(ev.data.error))
      }
    }
    w.onerror = () => {
      for (const [, entry] of pendingMap) {
        entry.reject(new Error('Replay decompression worker failed'))
      }
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
 * Fetches ordered .gz chunks via presigned R2 URLs and decompresses + merges rrweb events
 * off the main thread in a Web Worker (with stream fallback).
 */
export async function loadSessionChunks(
  chunks: ReplayChunkDetail[],
  signal?: AbortSignal,
): Promise<eventWithTime[]> {
  const orderedUrls = [...chunks]
    .sort((a, b) => a.chunk_seq - b.chunk_seq)
    .map((c) => c.url)
    .filter((u): u is string => Boolean(u && u.trim()))

  if (orderedUrls.length === 0) {
    return []
  }

  const worker = getDecompressWorker()
  if (!worker) {
    return fetchAndDecompressOnMainThread(orderedUrls, signal)
  }

  return new Promise<eventWithTime[]>((resolve, reject) => {
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
      resolve: (events) => {
        signal?.removeEventListener('abort', onAbort)
        resolve(events)
      },
      reject: (err) => {
        signal?.removeEventListener('abort', onAbort)
        reject(err)
      },
    })
    worker.postMessage({ id: reqId, urls: orderedUrls })
  })
}
