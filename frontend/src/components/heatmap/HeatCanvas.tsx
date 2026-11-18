import { useCallback, useEffect, useRef } from 'react'
import type { HeatmapCell, HeatmapResponse } from '../../api'
import { cellAt, cellShare } from '../../heatmap/grid'

type Props = {
  background: string | null
  vw: number
  vh: number
  res: HeatmapResponse
  showRage: boolean
  onHover: (cell: HeatmapCell | null, pos: { x: number; y: number } | null) => void
}

// Colour ramp: blue (cold) through green/yellow to red (hot), by intensity 0..1.
function rampHue(t: number): number {
  return 240 - 240 * Math.min(1, Math.max(0, t))
}

export default function HeatCanvas({ background, vw, vh, res, showRage, onHover }: Props) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)

  const draw = useCallback(() => {
    const wrap = wrapRef.current
    const canvas = canvasRef.current
    if (!wrap || !canvas) return
    const w = wrap.clientWidth
    const h = wrap.clientHeight
    if (w <= 0 || h <= 0) return
    const dpr = window.devicePixelRatio || 1
    canvas.width = Math.round(w * dpr)
    canvas.height = Math.round(h * dpr)
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.clearRect(0, 0, w, h)

    const cellW = w / res.cols
    const cellH = h / res.rows
    const radius = Math.max(cellW, cellH) * 2.2
    let maxShare = 0
    for (const c of res.cells) maxShare = Math.max(maxShare, cellShare(c, res.screen_sessions))
    if (maxShare > 0) {
      // Accumulate alpha in an offscreen greyscale layer, then colourize.
      const layer = document.createElement('canvas')
      layer.width = canvas.width
      layer.height = canvas.height
      const lctx = layer.getContext('2d')
      if (!lctx) return
      lctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      for (const c of res.cells) {
        const a = cellShare(c, res.screen_sessions) / maxShare
        if (a <= 0) continue
        const cx = (c.c + 0.5) * cellW
        const cy = (c.r + 0.5) * cellH
        const g = lctx.createRadialGradient(cx, cy, 0, cx, cy, radius)
        g.addColorStop(0, `rgba(0,0,0,${0.55 * a + 0.1})`)
        g.addColorStop(1, 'rgba(0,0,0,0)')
        lctx.fillStyle = g
        lctx.fillRect(cx - radius, cy - radius, radius * 2, radius * 2)
      }
      const img = lctx.getImageData(0, 0, layer.width, layer.height)
      const d = img.data
      for (let i = 0; i < d.length; i += 4) {
        const a = d[i + 3] / 255
        if (a <= 0) continue
        const [r, gr, b] = hslToRgb(rampHue(Math.min(1, a / 0.65)) / 360, 0.95, 0.52)
        d[i] = r
        d[i + 1] = gr
        d[i + 2] = b
        d[i + 3] = Math.round(Math.min(1, a * 1.3) * 255 * 0.8)
      }
      lctx.putImageData(img, 0, 0)
      ctx.drawImage(layer, 0, 0, w, h)
    }

    if (showRage) {
      ctx.fillStyle = 'rgba(239,68,68,0.9)'
      ctx.strokeStyle = 'rgba(255,255,255,0.9)'
      ctx.lineWidth = 1.5
      for (const p of res.rage) {
        ctx.beginPath()
        ctx.arc(p.x * w, p.y * h, 5, 0, Math.PI * 2)
        ctx.fill()
        ctx.stroke()
      }
    }
  }, [res, showRage])

  useEffect(() => {
    draw()
    const wrap = wrapRef.current
    if (!wrap) return
    const ro = new ResizeObserver(() => draw())
    ro.observe(wrap)
    return () => ro.disconnect()
  }, [draw])

  const aspect = vw > 0 && vh > 0 ? vw / vh : 9 / 16

  return (
    <div
      ref={wrapRef}
      className={`hm-canvas${background ? '' : ' hm-canvas--blank'}`}
      style={{ aspectRatio: String(aspect) }}
      onMouseMove={(e) => {
        const rect = e.currentTarget.getBoundingClientRect()
        const x = (e.clientX - rect.left) / rect.width
        const y = (e.clientY - rect.top) / rect.height
        onHover(cellAt(res, x, y), { x: e.clientX - rect.left, y: e.clientY - rect.top })
      }}
      onMouseLeave={() => onHover(null, null)}
    >
      {background && <img className="hm-canvas__img" src={background} alt="Screen reference" draggable={false} />}
      <canvas ref={canvasRef} className="hm-canvas__overlay" />
    </div>
  )
}

function hslToRgb(h: number, s: number, l: number): [number, number, number] {
  const f = (n: number) => {
    const k = (n + h * 12) % 12
    const a = s * Math.min(l, 1 - l)
    return l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))
  }
  return [Math.round(f(0) * 255), Math.round(f(8) * 255), Math.round(f(4) * 255)]
}
