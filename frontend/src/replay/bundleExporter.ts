import type { ReplaySessionItem, SessionPerfResponse, TelemetryEvent } from '../api'
import type { ExportChunkPayload } from './exportChunkFetcher'
import type { ResolvedRange } from './exportRange'
import { ZipWriter } from './zipWriter'

export type BundleExportOptions = {
  session: ReplaySessionItem
  range: ResolvedRange
  timelineEvents: TelemetryEvent[]
  perf: SessionPerfResponse | null
  chunkPayloads: ExportChunkPayload[]
  onProgress?: (percent: number, message: string) => void
  signal?: AbortSignal
}

/**
 * Creates a raw session diagnostic bundle archive (.zip).
 */
export async function exportRawSessionBundle(opts: BundleExportOptions): Promise<Blob> {
  const { session, range, timelineEvents, perf, chunkPayloads, onProgress, signal } = opts
  const zip = new ZipWriter()

  if (signal?.aborted) {
    throw new DOMException('Aborted', 'AbortError')
  }

  onProgress?.(10, 'Preparing session manifest…')

  const manifest = {
    schema: 'artracker-session-bundle-v1',
    session_id: session.id,
    project_id: session.project_id,
    user_id: session.user_id,
    environment: session.environment,
    start_time: session.start_time,
    clip_start_offset_ms: range.startOffsetMs,
    clip_end_offset_ms: range.endOffsetMs,
    exported_at: new Date().toISOString(),
    chunk_count: chunkPayloads.length,
    timeline_event_count: timelineEvents.length,
  }

  await zip.add({
    name: 'manifest.json',
    data: JSON.stringify(manifest, null, 2),
    compress: true,
  })

  await zip.add({
    name: 'session.json',
    data: JSON.stringify(session, null, 2),
    compress: true,
  })

  await zip.add({
    name: 'timeline_events.json',
    data: JSON.stringify(timelineEvents, null, 2),
    compress: true,
  })

  await zip.add({
    name: 'perf.json',
    data: JSON.stringify(perf ?? {}, null, 2),
    compress: true,
  })

  let i = 0
  for (const item of chunkPayloads) {
    if (signal?.aborted) {
      throw new DOMException('Aborted', 'AbortError')
    }
    const chunkName = `chunks/chunk_${String(item.seq).padStart(4, '0')}.json`
    await zip.add({
      name: chunkName,
      data: JSON.stringify(item.payload, null, 2),
      compress: true,
    })
    i++
    const pct = Math.round(30 + (i / Math.max(1, chunkPayloads.length)) * 60)
    onProgress?.(pct, `Packaging chunk ${i}/${chunkPayloads.length}…`)
  }

  onProgress?.(95, 'Finalizing zip bundle…')
  const archiveBytes = zip.build()
  onProgress?.(100, 'Complete!')

  return new Blob([archiveBytes as unknown as BlobPart], { type: 'application/zip' })
}
