import type { MobileFrame, TouchPoint } from './model'
import { activeFrameAt, activeTouchAt } from './playback'
import { activeScreenFrameAt, touchMarkerAt, type ScreenFrameMeta, type ScreenTouch } from './screen'
import { renderWireframeToCanvas, type WireframeCanvasOptions } from './wireframeCanvas'

export type VideoExportOptions = {
  mode: 'screen' | 'mobile' | 'web'
  startAbsMs: number
  endAbsMs: number
  fps?: number
  width?: number
  height?: number
  screenFrames?: ScreenFrameMeta[]
  screenTouches?: ScreenTouch[]
  screenImagesMap?: Map<number, ImageBitmap | HTMLImageElement>
  mobileFrames?: MobileFrame[]
  mobileTouches?: TouchPoint[]
  wireframeOptions?: WireframeCanvasOptions
  onProgress?: (percent: number, message: string) => void
  signal?: AbortSignal
}

export type VideoExportResult = {
  blob: Blob
  extension: 'mp4' | 'webm'
  mimeType: string
}

/**
 * Detects the best video container supported by the current browser.
 */
export function getSupportedVideoMimeType(): { mimeType: string; extension: 'mp4' | 'webm' } {
  if (typeof MediaRecorder === 'undefined') {
    return { mimeType: 'video/webm', extension: 'webm' }
  }

  const candidates = [
    { mime: 'video/mp4;codecs=avc1', ext: 'mp4' as const },
    { mime: 'video/mp4', ext: 'mp4' as const },
    { mime: 'video/webm;codecs=vp9', ext: 'webm' as const },
    { mime: 'video/webm;codecs=vp8', ext: 'webm' as const },
    { mime: 'video/webm', ext: 'webm' as const },
  ]

  for (const c of candidates) {
    if (MediaRecorder.isTypeSupported(c.mime)) {
      return { mimeType: c.mime, extension: c.ext }
    }
  }

  return { mimeType: 'video/webm', extension: 'webm' }
}

/**
 * Exports replay visual frames into a playable video (.mp4 or .webm) client-side.
 */
export async function exportReplayVideo(opts: VideoExportOptions): Promise<VideoExportResult> {
  const {
    mode,
    startAbsMs,
    endAbsMs,
    fps = 10,
    width: initialW = 402,
    height: initialH = 874,
    screenFrames = [],
    screenTouches = [],
    screenImagesMap = new Map(),
    mobileFrames = [],
    mobileTouches = [],
    wireframeOptions = {},
    onProgress,
    signal,
  } = opts

  if (signal?.aborted) {
    throw new DOMException('Aborted', 'AbortError')
  }

  // Ensure even dimensions required by H.264 / VP9 encoders
  const width = initialW & ~1
  const height = initialH & ~1

  const { mimeType, extension } = getSupportedVideoMimeType()

  let canvas: HTMLCanvasElement
  if (typeof document !== 'undefined' && typeof document.createElement === 'function') {
    canvas = document.createElement('canvas')
  } else {
    // Fallback for node test environments
    canvas = {
      width,
      height,
      getContext: () => null,
      captureStream: () => ({}),
    } as unknown as HTMLCanvasElement
  }
  canvas.width = width
  canvas.height = height

  const ctx = canvas.getContext('2d')
  if (!ctx) {
    throw new Error('Canvas 2D context not available for video export')
  }

  const durationMs = Math.max(100, endAbsMs - startAbsMs)
  const frameIntervalMs = 1000 / fps
  const totalFrames = Math.max(1, Math.ceil(durationMs / frameIntervalMs))

  const stream = (canvas as HTMLCanvasElement).captureStream(fps)
  const chunks: BlobPart[] = []

  const recorder = new MediaRecorder(stream, {
    mimeType: MediaRecorder.isTypeSupported(mimeType) ? mimeType : undefined,
    videoBitsPerSecond: 2_500_000,
  })

  recorder.ondataavailable = (e) => {
    if (e.data && e.data.size > 0) {
      chunks.push(e.data)
    }
  }

  const recordPromise = new Promise<Blob>((resolve, reject) => {
    recorder.onstop = () => {
      resolve(new Blob(chunks, { type: mimeType }))
    }
    recorder.onerror = (err) => {
      reject(err)
    }
  })

  recorder.start()

  try {
    for (let frameIdx = 0; frameIdx < totalFrames; frameIdx++) {
      if (signal?.aborted) {
        recorder.stop()
        throw new DOMException('Aborted', 'AbortError')
      }

      const currentTs = startAbsMs + frameIdx * frameIntervalMs

      if (mode === 'screen') {
        ctx.fillStyle = '#090d16'
        ctx.fillRect(0, 0, width, height)

        const activeFrame = activeScreenFrameAt(screenFrames, currentTs)
        if (activeFrame) {
          const img = screenImagesMap.get(activeFrame.ts)
          if (img) {
            const scale = Math.min(width / activeFrame.w, height / activeFrame.h)
            const dw = Math.round(activeFrame.w * scale)
            const dh = Math.round(activeFrame.h * scale)
            const dx = Math.round((width - dw) / 2)
            const dy = Math.round((height - dh) / 2)
            ctx.drawImage(img, dx, dy, dw, dh)

            // Touch marker
            const marker = touchMarkerAt(
              screenTouches,
              currentTs,
              { vw: activeFrame.vw, vh: activeFrame.vh },
              { width: dw, height: dh },
            )
            if (marker) {
              ctx.fillStyle = '#38bdf8'
              ctx.beginPath()
              ctx.arc(dx + marker.x, dy + marker.y, 6, 0, Math.PI * 2)
              ctx.fill()
            }
          }
        }
      } else if (mode === 'mobile') {
        const activeF = activeFrameAt(mobileFrames, currentTs)
        const activeT = activeTouchAt(mobileTouches, currentTs)
        renderWireframeToCanvas(ctx, activeF, activeT, {
          ...wireframeOptions,
          width,
          height,
        })
      } else {
        // Web / Telemetry fallback stage
        ctx.fillStyle = '#0f172a'
        ctx.fillRect(0, 0, width, height)
        ctx.fillStyle = '#38bdf8'
        ctx.font = 'bold 16px -apple-system, sans-serif'
        ctx.textAlign = 'center'
        ctx.fillText('Session Replay Recording', width / 2, height / 2 - 20)
        ctx.fillStyle = '#94a3b8'
        ctx.font = '12px -apple-system, sans-serif'
        ctx.fillText(`Offset: ${Math.round((currentTs - startAbsMs) / 1000)}s`, width / 2, height / 2 + 10)
      }

      // Allow event loop to process frame drawing
      await new Promise((resolve) => setTimeout(resolve, 0))

      const pct = Math.round(((frameIdx + 1) / totalFrames) * 90)
      onProgress?.(pct, `Encoding frame ${frameIdx + 1}/${totalFrames}…`)
    }

    onProgress?.(95, 'Finalizing video stream…')
    recorder.stop()
    const videoBlob = await recordPromise
    onProgress?.(100, 'Video ready!')

    return {
      blob: videoBlob,
      extension,
      mimeType,
    }
  } catch (err) {
    if (recorder.state !== 'inactive') {
      try {
        recorder.stop()
      } catch {
        // ignore
      }
    }
    throw err
  }
}
