import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { SymbolicatedProfile } from '../../api'
import { boxStats, layoutFlame, toPixels, type FlameBox } from '../../appstart/flame'
import { formatMs } from '../../appstart/format'

const ROW_H = 18
const LABEL_MIN_PX = 40
const CHAR_PX = 6.5
const FALLBACK_WIDTH = 800

type Hover = { box: FlameBox; x: number; y: number }

/** Icicle-style flamegraph (depth 0 on top). Click a box to zoom to it; double-click or Esc resets. */
export default function Flamegraph({
  profile,
  onSelect,
}: {
  profile: SymbolicatedProfile
  onSelect?: (box: FlameBox | null) => void
}) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(FALLBACK_WIDTH)
  const [zoom, setZoom] = useState<FlameBox | null>(null)
  const [hover, setHover] = useState<Hover | null>(null)

  const boxes = useMemo(() => layoutFlame(profile), [profile])
  const [origin, end] = useMemo(() => {
    let lo = Infinity
    let hi = -Infinity
    for (const b of boxes) {
      lo = Math.min(lo, b.start)
      hi = Math.max(hi, b.end)
    }
    return boxes.length ? [lo, hi] : [0, 0]
  }, [boxes])
  const maxDepth = useMemo(() => boxes.reduce((m, b) => Math.max(m, b.depth + 1), 0), [boxes])

  const viewStart = zoom ? zoom.start : origin
  const viewEnd = zoom ? zoom.end : end
  const pixels = useMemo(() => toPixels(boxes, viewStart, viewEnd, width), [boxes, viewStart, viewEnd, width])

  useEffect(() => {
    const el = wrapRef.current
    if (!el) return
    const measure = () => setWidth(Math.max(1, Math.floor(el.clientWidth)) || FALLBACK_WIDTH)
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const reset = useCallback(() => {
    setZoom(null)
    onSelect?.(null)
  }, [onSelect])

  useEffect(() => {
    if (!zoom) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') reset()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [zoom, reset])

  const stats = useMemo(() => (hover ? boxStats(profile, hover.box) : null), [profile, hover])
  const hoverFrame = hover ? profile.frames[hover.box.frame] : null

  if (boxes.length === 0) return <p className="ppl-muted">The profile has no samples.</p>

  return (
    <div className="as-flame" ref={wrapRef}>
      <svg
        width={width}
        height={maxDepth * ROW_H}
        role="img"
        aria-label="Startup flamegraph"
        onDoubleClick={reset}
        onMouseLeave={() => setHover(null)}
      >
        {pixels.map((b) => {
          const frame = profile.frames[b.frame]
          const maxChars = Math.floor((b.w - 8) / CHAR_PX)
          const name = frame?.function ?? '?'
          return (
            <g
              key={`${b.depth}:${b.start}:${b.frame}`}
              className={`as-flame__box ${frame?.is_app ? 'is-app' : 'is-lib'}${zoom && zoom.depth === b.depth && zoom.start === b.start && zoom.frame === b.frame ? ' is-zoomed' : ''}`}
              onClick={() => {
                setZoom(b)
                onSelect?.(b)
              }}
              onMouseMove={(e) => {
                const rect = wrapRef.current?.getBoundingClientRect()
                if (rect) setHover({ box: b, x: e.clientX - rect.left, y: e.clientY - rect.top })
              }}
            >
              <rect x={b.x} y={b.depth * ROW_H} width={Math.max(b.w - 1, 0.5)} height={ROW_H - 1} />
              {b.w >= LABEL_MIN_PX && maxChars > 0 && (
                <text x={b.x + 4} y={b.depth * ROW_H + ROW_H / 2 + 0.5} dominantBaseline="middle">
                  {name.length > maxChars ? `${name.slice(0, Math.max(maxChars - 1, 1))}…` : name}
                </text>
              )}
            </g>
          )
        })}
      </svg>
      {hover && hoverFrame && stats && (
        <div className="as-flame__tip" style={{ left: Math.min(hover.x + 12, Math.max(width - 260, 0)), top: hover.y + 16 }}>
          <strong>{hoverFrame.function}</strong>
          {hoverFrame.file && (
            <span>
              {hoverFrame.file}
              {hoverFrame.line ? `:${hoverFrame.line}` : ''}
            </span>
          )}
          <span>
            {formatMs(stats.totalMs)} total · {formatMs(stats.selfMs)} self
            {hoverFrame.is_app ? '' : ' · library'}
          </span>
        </div>
      )}
      {zoom && <p className="ppl-muted as-flame__hint">Zoomed. Double-click or press Esc to reset.</p>}
    </div>
  )
}
