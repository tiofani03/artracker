import type { ReplaySessionItem } from '../api'
import type { MobileFrame, TouchPoint } from './model'
import { screenFrameData, type ScreenFrameMeta, type ScreenTouch } from './screen'
import { renderWireframeToCanvas, type WireframeCanvasOptions } from './wireframeCanvas'
import { ZipWriter } from './zipWriter'

export type ImagesExportOptions = {
  session: ReplaySessionItem
  mode: 'screen' | 'mobile' | 'web'
  screenFrames?: ScreenFrameMeta[]
  screenTouches?: ScreenTouch[]
  screenPayloadsMap?: Map<number, unknown> // chunkSeq -> rawPayload
  mobileFrames?: MobileFrame[]
  mobileTouches?: TouchPoint[]
  burnTouchOverlays?: boolean
  wireframeOptions?: WireframeCanvasOptions
  onProgress?: (percent: number, message: string) => void
  signal?: AbortSignal
}

function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) {
    bytes[i] = bin.charCodeAt(i)
  }
  return bytes
}

function formatOffsetFilename(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000))
  const min = Math.floor(totalSec / 60)
  const sec = totalSec % 60
  return `${String(min).padStart(2, '0')}m${String(sec).padStart(2, '0')}s`
}

export type ExportedImageManifest = {
  session_id: string
  total_images: number
  exported_at: string
  images: Array<{
    filename: string
    timestamp: number
    screen: string
  }>
}

/**
 * Exports session visual frames into a zipped images collection (.zip).
 */
export async function exportImagesZip(opts: ImagesExportOptions): Promise<Blob> {
  const {
    session,
    mode,
    screenFrames = [],
    screenPayloadsMap = new Map(),
    mobileFrames = [],
    mobileTouches = [],
    wireframeOptions = {},
    onProgress,
    signal,
  } = opts

  const zip = new ZipWriter()
  const manifest: ExportedImageManifest = {
    session_id: session.id,
    total_images: 0,
    exported_at: new Date().toISOString(),
    images: [],
  }

  if (signal?.aborted) {
    throw new DOMException('Aborted', 'AbortError')
  }

  // 1. Mobile Screenshot mode (artracker-screen-v1)
  if (mode === 'screen') {
    const total = screenFrames.length
    for (let i = 0; i < total; i++) {
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError')
      const f = screenFrames[i]
      const payload = screenPayloadsMap.get(f.chunkSeq)
      if (!payload) continue

      const b64 = screenFrameData(payload, f.index)
      if (!b64) continue

      const numStr = String(i + 1).padStart(4, '0')
      const filename = `frame_${numStr}_${formatOffsetFilename(f.ts)}.jpg`
      const bytes = base64ToBytes(b64)

      await zip.add({
        name: filename,
        data: bytes,
        compress: false, // JPEGs are already compressed
      })

      manifest.images.push({
        filename,
        timestamp: f.ts,
        screen: f.screen,
      })

      const pct = Math.round(((i + 1) / Math.max(1, total)) * 85)
      onProgress?.(pct, `Packing image ${i + 1}/${total}…`)
    }
  }

  // 2. Mobile Wireframe mode
  if (mode === 'mobile') {
    // Generate event-driven keyframes (per frame change, and per touch)
    const keyframes: Array<{ frame: MobileFrame; touch: TouchPoint | null; ts: number }> = []
    for (const f of mobileFrames) {
      keyframes.push({ frame: f, touch: null, ts: f.ts })
    }

    for (const t of mobileTouches) {
      // Find active frame at touch time
      let activeF: MobileFrame | null = null
      for (const f of mobileFrames) {
        if (f.ts <= t.ts && (!activeF || f.ts >= activeF.ts)) {
          activeF = f
        }
      }
      if (activeF) {
        keyframes.push({ frame: activeF, touch: t, ts: t.ts })
      }
    }

    // Sort by timestamp and deduplicate within 150ms
    keyframes.sort((a, b) => a.ts - b.ts)
    const deduped: typeof keyframes = []
    let lastTs = -Infinity
    for (const kf of keyframes) {
      if (kf.ts - lastTs >= 150) {
        deduped.push(kf)
        lastTs = kf.ts
      }
    }

    const total = deduped.length
    let canvas: HTMLCanvasElement | null = null
    let ctx: CanvasRenderingContext2D | null = null

    if (typeof document !== 'undefined' && typeof document.createElement === 'function') {
      canvas = document.createElement('canvas')
      canvas.width = 402
      canvas.height = 874
      ctx = canvas.getContext('2d')
    }

    for (let i = 0; i < total; i++) {
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError')
      const kf = deduped[i]
      const numStr = String(i + 1).padStart(4, '0')
      const filename = `wireframe_${numStr}_${formatOffsetFilename(kf.ts)}.png`

      if (ctx && canvas) {
        renderWireframeToCanvas(ctx, kf.frame, kf.touch, wireframeOptions)
        // Convert canvas to data URL / Blob
        const dataUrl = canvas.toDataURL('image/png')
        const b64 = dataUrl.split(',')[1]
        if (b64) {
          await zip.add({
            name: filename,
            data: base64ToBytes(b64),
            compress: false,
          })
        }
      }

      manifest.images.push({
        filename,
        timestamp: kf.ts,
        screen: kf.frame.screen,
      })

      const pct = Math.round(((i + 1) / Math.max(1, total)) * 85)
      onProgress?.(pct, `Rendering wireframe ${i + 1}/${total}…`)
    }
  }

  manifest.total_images = manifest.images.length
  await zip.add({
    name: 'manifest.json',
    data: JSON.stringify(manifest, null, 2),
    compress: true,
  })

  onProgress?.(95, 'Finalizing images archive…')
  const archive = zip.build()
  onProgress?.(100, 'Complete!')

  return new Blob([archive as unknown as BlobPart], { type: 'application/zip' })
}
