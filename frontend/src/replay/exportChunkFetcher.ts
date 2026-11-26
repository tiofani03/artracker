import type { ReplayChunkDetail } from '../api'
import { parseScreenChunk, type ParsedScreenChunk } from './screen'
import { ScreenChunkSource, type RefreshUrls } from '../utils/screenChunkSource'

export type FetchChunksProgress = (loaded: number, total: number) => void

export type ExportChunkPayload = {
  seq: number
  payload: unknown
}

/**
 * Downloads and decodes the required chunks for export.
 * Supports progress notification and cancellation via AbortSignal.
 */
export async function fetchExportChunks(opts: {
  chunks: ReplayChunkDetail[]
  targetSeqs: number[]
  refreshUrls: RefreshUrls
  onProgress?: FetchChunksProgress
  signal?: AbortSignal
}): Promise<ExportChunkPayload[]> {
  const { chunks, targetSeqs, refreshUrls, onProgress, signal } = opts
  const sortedSeqs = [...new Set(targetSeqs)].sort((a, b) => a - b)

  if (sortedSeqs.length === 0) return []

  const source = new ScreenChunkSource(chunks, refreshUrls)
  const results: ExportChunkPayload[] = []
  let loaded = 0

  for (const seq of sortedSeqs) {
    if (signal?.aborted) {
      throw new DOMException('Aborted', 'AbortError')
    }

    const payload = await source.fetchChunk(seq, signal)
    if (payload !== null) {
      results.push({ seq, payload })
    }
    loaded++
    onProgress?.(loaded, sortedSeqs.length)
  }

  return results
}

/**
 * Parses screen chunks from raw payloads, keeping only valid parsed chunks.
 */
export function parseExportScreenChunks(rawList: ExportChunkPayload[]): ParsedScreenChunk[] {
  const parsedList: ParsedScreenChunk[] = []
  for (const item of rawList) {
    const parsed = parseScreenChunk(item.payload, item.seq)
    if (parsed) {
      parsedList.push(parsed)
    }
  }
  return parsedList
}
