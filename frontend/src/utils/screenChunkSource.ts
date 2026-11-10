import type { ReplayChunkDetail } from '../api'
import { chunkFetchUrl, parseChunkBytes } from '../replay/parseChunk'

/** Asks the API for freshly signed URLs of chunks `fromSeq..toSeq`. */
export type RefreshUrls = (fromSeq: number, toSeq: number) => Promise<ReplayChunkDetail[]>

type Attempt = { kind: 'ok'; payload: unknown | null } | { kind: 'denied' } | { kind: 'error' }

/**
 * Downloads and parses single chunks. A 401/403 (expired signed URL) triggers one URL refresh and one retry;
 * every other failure resolves to `null`. Only an aborted signal rejects.
 */
export class ScreenChunkSource {
  private readonly urls = new Map<number, string>()
  private readonly refresh: RefreshUrls
  private readonly fetchImpl: typeof fetch

  constructor(chunks: ReplayChunkDetail[], refresh: RefreshUrls, fetchImpl: typeof fetch = fetch) {
    this.refresh = refresh
    this.fetchImpl = fetchImpl
    for (const c of chunks) {
      if (c.url && c.url.trim()) this.urls.set(c.chunk_seq, c.url)
    }
  }

  async fetchChunk(seq: number, signal?: AbortSignal): Promise<unknown | null> {
    const url = this.urls.get(seq)
    if (!url) return null

    let result = await this.attempt(url, signal)
    if (result.kind === 'denied') {
      let fresh: string | undefined
      try {
        const list = await this.refresh(seq, seq)
        fresh = list.find((c) => c.chunk_seq === seq)?.url
      } catch (err) {
        if (signal?.aborted) throw err
        return null
      }
      if (!fresh || !fresh.trim()) return null
      this.urls.set(seq, fresh)
      result = await this.attempt(fresh, signal)
    }
    return result.kind === 'ok' ? result.payload : null
  }

  private async attempt(url: string, signal?: AbortSignal): Promise<Attempt> {
    try {
      const doFetch = this.fetchImpl // not called as a method: bare `fetch` throws "Illegal invocation" otherwise
      const res = await doFetch(chunkFetchUrl(url), { signal })
      if (res.status === 401 || res.status === 403) return { kind: 'denied' }
      if (!res.ok) return { kind: 'error' }
      return { kind: 'ok', payload: await parseChunkBytes(await res.arrayBuffer()) }
    } catch (err) {
      if (signal?.aborted) throw err
      return { kind: 'error' }
    }
  }
}
