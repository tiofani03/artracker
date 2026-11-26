import type { ReplayChunkDetail, ReplaySessionItem, SessionPerfResponse, TelemetryEvent } from '../api'
import type { ReplayModel } from './model'
import type { ExportOptions, ExportProgress } from './exportTypes'
import {
  resolveExportRange,
  sliceMobileData,
  slicePerfSamples,
  sliceScreenData,
  sliceTimelineEvents,
} from './exportRange'
import { fetchExportChunks, parseExportScreenChunks } from './exportChunkFetcher'
import { exportRawSessionBundle } from './bundleExporter'
import { exportImagesZip } from './imagesExporter'
import { exportReplayVideo } from './videoExporter'
import { exportOfflineHtmlPlayer } from './htmlExporter'
import { saveExportFile } from '../utils/saveExportFile'
import type { RefreshUrls } from '../utils/screenChunkSource'
import { screenFrameData } from './screen'

export type RunExportParams = {
  options: ExportOptions
  session: ReplaySessionItem
  model: ReplayModel
  chunks: ReplayChunkDetail[]
  timelineEvents: TelemetryEvent[]
  perf: SessionPerfResponse | null
  baseTimestampMs: number
  durationMs: number
  refreshUrls: RefreshUrls
  onProgress: (progress: ExportProgress) => void
  signal: AbortSignal
}

function sanitizeFilenamePart(str: string): string {
  return str.replace(/[^a-zA-Z0-9_-]/g, '_')
}

/**
 * Orchestrates the complete export flow across any format and replay mode.
 */
export async function runReplayExport(params: RunExportParams): Promise<void> {
  const {
    options,
    session,
    model,
    chunks,
    timelineEvents,
    perf,
    baseTimestampMs,
    durationMs,
    refreshUrls,
    onProgress,
    signal,
  } = params

  if (signal.aborted) {
    throw new DOMException('Aborted', 'AbortError')
  }

  // 1. Resolve range
  const clipStart = options.rangeMode === 'clip' ? options.clipStartMs : 0
  const clipEnd = options.rangeMode === 'clip' ? options.clipEndMs : durationMs
  const range = resolveExportRange(baseTimestampMs, durationMs, clipStart, clipEnd)

  onProgress({
    phase: 'fetching',
    percent: 5,
    message: 'Analyzing replay chunks…',
  })

  // 2. Determine target chunks to fetch
  const allSeqs = chunks.map((c) => c.chunk_seq).sort((a, b) => a - b)
  let targetSeqs = allSeqs

  // For screen sessions, if the session index has ranges, we can filter relevant seqs
  if (model.kind === 'screen') {
    const screenRanges = model.session.index.ranges()
    if (screenRanges.length > 0) {
      const needed = screenRanges
        .filter((r) => r.endMs >= range.startAbsMs && r.startMs <= range.endAbsMs)
        .map((r) => r.seq)
      if (needed.length > 0) {
        targetSeqs = needed
      }
    }
  }

  // 3. Download required chunk payloads
  const fetchedChunks = await fetchExportChunks({
    chunks,
    targetSeqs,
    refreshUrls,
    onProgress: (loaded, total) => {
      const pct = Math.round(5 + (loaded / Math.max(1, total)) * 30)
      onProgress({
        phase: 'fetching',
        percent: pct,
        message: `Downloading replay chunk ${loaded}/${total}…`,
      })
    },
    signal,
  })

  if (signal.aborted) {
    throw new DOMException('Aborted', 'AbortError')
  }

  onProgress({
    phase: 'processing',
    percent: 40,
    message: 'Slicing timeline events and samples…',
  })

  // 4. Slice timeline events & perf
  const slicedEvents = sliceTimelineEvents(timelineEvents, range.startAbsMs, range.endAbsMs)
  const slicedPerf = slicePerfSamples(perf, range.startAbsMs, range.endAbsMs)

  // Filename base
  const idPrefix = sanitizeFilenamePart(session.id.slice(0, 12))
  const rangeSuffix =
    options.rangeMode === 'clip'
      ? `_clip_${Math.round(range.startOffsetMs / 1000)}s-${Math.round(range.endOffsetMs / 1000)}s`
      : ''

  // 5. Route to format exporter
  if (options.format === 'raw-bundle') {
    onProgress({
      phase: 'packaging',
      percent: 50,
      message: 'Assembling raw session bundle…',
    })
    const blob = await exportRawSessionBundle({
      session,
      range,
      timelineEvents: slicedEvents,
      perf: slicedPerf,
      chunkPayloads: fetchedChunks,
      onProgress: (pct, msg) => {
        onProgress({ phase: 'packaging', percent: 40 + Math.round(pct * 0.55), message: msg })
      },
      signal,
    })
    const filename = `session_${idPrefix}${rangeSuffix}_bundle.zip`
    await saveExportFile(blob, filename)
    onProgress({ phase: 'done', percent: 100, message: 'Export complete!' })
    return
  }

  if (options.format === 'images-zip') {
    onProgress({
      phase: 'packaging',
      percent: 50,
      message: 'Preparing images archive…',
    })

    const payloadsMap = new Map<number, unknown>()
    for (const c of fetchedChunks) {
      payloadsMap.set(c.seq, c.payload)
    }

    let screenFrames: ReturnType<typeof sliceScreenData>['frames'] = []
    let screenTouches: ReturnType<typeof sliceScreenData>['touches'] = []

    if (model.kind === 'screen') {
      const parsedScreenChunks = parseExportScreenChunks(fetchedChunks)
      const allFrames = parsedScreenChunks.flatMap((c) => c.frames)
      const allTouches = parsedScreenChunks.flatMap((c) => c.touches)
      const sliced = sliceScreenData(allFrames, allTouches, range.startAbsMs, range.endAbsMs)
      screenFrames = sliced.frames
      screenTouches = sliced.touches
    }

    let mobileFrames: ReturnType<typeof sliceMobileData>['frames'] = []
    let mobileTouches: ReturnType<typeof sliceMobileData>['touches'] = []
    if (model.kind === 'mobile') {
      const sliced = sliceMobileData(model.frames, model.touches, range.startAbsMs, range.endAbsMs)
      mobileFrames = sliced.frames
      mobileTouches = sliced.touches
    }

    const blob = await exportImagesZip({
      session,
      mode: model.kind === 'screen' ? 'screen' : model.kind === 'mobile' ? 'mobile' : 'web',
      screenFrames,
      screenTouches,
      screenPayloadsMap: payloadsMap,
      mobileFrames,
      mobileTouches,
      wireframeOptions: { theme: options.wireframeTheme ?? 'dark' },
      onProgress: (pct, msg) => {
        onProgress({ phase: 'packaging', percent: 40 + Math.round(pct * 0.55), message: msg })
      },
      signal,
    })

    const filename = `session_${idPrefix}${rangeSuffix}_images.zip`
    await saveExportFile(blob, filename)
    onProgress({ phase: 'done', percent: 100, message: 'Export complete!' })
    return
  }

  if (options.format === 'offline-html') {
    onProgress({
      phase: 'encoding',
      percent: 50,
      message: 'Generating offline interactive player…',
    })

    const screenImagesMap = new Map<number, string>()
    let screenFrames: ReturnType<typeof sliceScreenData>['frames'] = []
    let screenTouches: ReturnType<typeof sliceScreenData>['touches'] = []

    if (model.kind === 'screen') {
      const parsedScreenChunks = parseExportScreenChunks(fetchedChunks)
      const allFrames = parsedScreenChunks.flatMap((c) => c.frames)
      const allTouches = parsedScreenChunks.flatMap((c) => c.touches)
      const sliced = sliceScreenData(allFrames, allTouches, range.startAbsMs, range.endAbsMs)
      screenFrames = sliced.frames
      screenTouches = sliced.touches

      for (const f of screenFrames) {
        const payload = fetchedChunks.find((c) => c.seq === f.chunkSeq)?.payload
        if (payload) {
          const b64 = screenFrameData(payload, f.index)
          if (b64) screenImagesMap.set(f.ts, b64)
        }
      }
    }

    let mobileFrames: ReturnType<typeof sliceMobileData>['frames'] = []
    let mobileTouches: ReturnType<typeof sliceMobileData>['touches'] = []
    if (model.kind === 'mobile') {
      const sliced = sliceMobileData(model.frames, model.touches, range.startAbsMs, range.endAbsMs)
      mobileFrames = sliced.frames
      mobileTouches = sliced.touches
    }

    const blob = await exportOfflineHtmlPlayer({
      session,
      mode: model.kind,
      startAbsMs: range.startAbsMs,
      endAbsMs: range.endAbsMs,
      timelineEvents: slicedEvents,
      perf: slicedPerf,
      screenFrames,
      screenTouches,
      screenImagesMap,
      mobileFrames,
      mobileTouches,
      onProgress: (pct, msg) => {
        onProgress({ phase: 'encoding', percent: 40 + Math.round(pct * 0.55), message: msg })
      },
      signal,
    })

    const filename = `session_${idPrefix}${rangeSuffix}_offline.html`
    await saveExportFile(blob, filename)
    onProgress({ phase: 'done', percent: 100, message: 'Export complete!' })
    return
  }

  if (options.format === 'video') {
    onProgress({
      phase: 'encoding',
      percent: 45,
      message: 'Encoding session video…',
    })

    let screenFrames: ReturnType<typeof sliceScreenData>['frames'] = []
    let screenTouches: ReturnType<typeof sliceScreenData>['touches'] = []
    const screenImagesMap = new Map<number, ImageBitmap>()

    if (model.kind === 'screen') {
      const parsedScreenChunks = parseExportScreenChunks(fetchedChunks)
      const allFrames = parsedScreenChunks.flatMap((c) => c.frames)
      const allTouches = parsedScreenChunks.flatMap((c) => c.touches)
      const sliced = sliceScreenData(allFrames, allTouches, range.startAbsMs, range.endAbsMs)
      screenFrames = sliced.frames
      screenTouches = sliced.touches

      // Pre-decode frames if createImageBitmap is available
      if (typeof createImageBitmap === 'function') {
        for (const f of screenFrames) {
          const payload = fetchedChunks.find((c) => c.seq === f.chunkSeq)?.payload
          if (payload) {
            const b64 = screenFrameData(payload, f.index)
            if (b64) {
              const bin = atob(b64)
              const bytes = new Uint8Array(bin.length)
              for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
              const bmp = await createImageBitmap(new Blob([bytes], { type: f.mime }))
              screenImagesMap.set(f.ts, bmp)
            }
          }
        }
      }
    }

    let mobileFrames: ReturnType<typeof sliceMobileData>['frames'] = []
    let mobileTouches: ReturnType<typeof sliceMobileData>['touches'] = []
    if (model.kind === 'mobile') {
      const sliced = sliceMobileData(model.frames, model.touches, range.startAbsMs, range.endAbsMs)
      mobileFrames = sliced.frames
      mobileTouches = sliced.touches
    }

    const { blob, extension } = await exportReplayVideo({
      mode: model.kind === 'screen' ? 'screen' : model.kind === 'mobile' ? 'mobile' : 'web',
      startAbsMs: range.startAbsMs,
      endAbsMs: range.endAbsMs,
      fps: options.videoFps ?? 10,
      screenFrames,
      screenTouches,
      screenImagesMap,
      mobileFrames,
      mobileTouches,
      wireframeOptions: { theme: options.wireframeTheme ?? 'dark' },
      onProgress: (pct, msg) => {
        onProgress({ phase: 'encoding', percent: 40 + Math.round(pct * 0.55), message: msg })
      },
      signal,
    })

    // Clean up bitmaps
    for (const bmp of screenImagesMap.values()) {
      bmp.close?.()
    }

    const filename = `session_${idPrefix}${rangeSuffix}.${extension}`
    await saveExportFile(blob, filename)
    onProgress({ phase: 'done', percent: 100, message: 'Export complete!' })
  }
}
