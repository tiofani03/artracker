import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { touchMarkerAt } from '../replay/screen'
import { frameKey, planScreenFrame } from '../replay/screenPlanner'
import type { ScreenReplaySession } from '../replay/screenSession'

/** Decoded images older than this behind the playhead are released. */
const KEEP_BEHIND_MS = 10_000

function base64ToBlob(b64: string, mime: string): Blob {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return new Blob([bytes], { type: mime })
}

type Props = {
  session: ScreenReplaySession
  /** Absolute playhead position, epoch milliseconds. */
  currentTs: number
  width: number
  height: number
}

/**
 * Draws the device's own pixels: the frame with the greatest ts <= playhead, letterboxed on a canvas.
 * Frames are decoded ahead of time; while the due frame is still decoding the previous one stays up.
 */
export function ScreenReplayRenderer({ session, currentTs, width, height }: Props) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const bitmaps = useRef(new Map<string, { bmp: ImageBitmap; ts: number }>())
  const pending = useRef(new Set<string>())
  const [, setDecodedVersion] = useState(0)

  const subscribe = (fn: () => void) => session.subscribe(fn)
  useSyncExternalStore(subscribe, () => session.version)

  const frames = session.index.frames
  const touches = session.index.touches
  const decodedKeys = new Set(bitmaps.current.keys())
  const plan = planScreenFrame({
    frames,
    playheadMs: currentTs,
    decoded: decodedKeys,
    resolved: session.index.isResolved(currentTs),
  })

  // Keep the session's resident chunk window centred on the playhead.
  useEffect(() => {
    session.setPlayhead(currentTs)
  }, [session, currentTs])

  // Decode the due frame and the look-ahead; re-runs when more chunk data arrives (session.version).
  const dueKey = plan.due ? frameKey(plan.due) : ''
  const prefetchKey = plan.prefetch.map(frameKey).join(',')
  useEffect(() => {
    const wanted = plan.due && !bitmaps.current.has(frameKey(plan.due)) ? [plan.due, ...plan.prefetch] : plan.prefetch
    for (const f of wanted) {
      const key = frameKey(f)
      if (bitmaps.current.has(key) || pending.current.has(key)) continue
      const b64 = session.frameImage(f)
      if (b64 === null) continue // chunk not resident yet: retried when the session notifies
      pending.current.add(key)
      createImageBitmap(base64ToBlob(b64, f.mime), { colorSpaceConversion: 'none', premultiplyAlpha: 'none' })
        .then((bmp) => {
          bitmaps.current.set(key, { bmp, ts: f.ts })
          setDecodedVersion((v) => v + 1)
        })
        .catch(() => {
          // undecodable frame: the previous frame stays up
        })
        .finally(() => pending.current.delete(key))
    }
    // `plan` is derived from the keys and session.version listed here
  }, [session, dueKey, prefetchKey, session.version])

  // Release decoded images that fell far behind the playhead.
  useEffect(() => {
    for (const [key, entry] of bitmaps.current) {
      if (entry.ts < currentTs - KEEP_BEHIND_MS) {
        entry.bmp.close()
        bitmaps.current.delete(key)
      }
    }
  }, [currentTs])

  useEffect(() => {
    const map = bitmaps.current
    return () => {
      for (const { bmp } of map.values()) bmp.close()
      map.clear()
    }
  }, [session])

  // Geometry of the letterboxed image inside the stage.
  const shown = plan.show
  const scale = shown ? Math.min(width / shown.w, height / shown.h) : 1
  const dw = shown ? Math.round(shown.w * scale) : 0
  const dh = shown ? Math.round(shown.h * scale) : 0
  const dx = Math.round((width - dw) / 2)
  const dy = Math.round((height - dh) / 2)

  const showKey = shown ? frameKey(shown) : ''
  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const dpr = window.devicePixelRatio || 1
    canvas.width = Math.round(width * dpr)
    canvas.height = Math.round(height * dpr)
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.clearRect(0, 0, canvas.width, canvas.height)
    const entry = showKey ? bitmaps.current.get(showKey) : undefined
    if (!entry) return
    ctx.imageSmoothingQuality = 'high'
    ctx.drawImage(entry.bmp, dx * dpr, dy * dpr, dw * dpr, dh * dpr)
  }, [showKey, width, height, dx, dy, dw, dh])

  const marker = shown ? touchMarkerAt(touches, currentTs, { vw: shown.vw, vh: shown.vh }, { width: dw, height: dh }) : null

  return (
    <div className="sr-stage" style={{ width, height }} data-testid="screen-replay-stage">
      <canvas ref={canvasRef} className="sr-canvas" style={{ width, height }} data-testid="screen-replay-canvas" />
      {marker && (
        <div
          className={`sr-touch sr-touch--${marker.kind}`}
          style={{ left: dx + marker.x, top: dy + marker.y }}
          aria-hidden="true"
        />
      )}
      {plan.buffering && (
        <div className="sr-badge" role="status">
          Buffering…
        </div>
      )}
      {!shown && !plan.buffering && <div className="sr-empty">No frames recorded yet</div>}
    </div>
  )
}
