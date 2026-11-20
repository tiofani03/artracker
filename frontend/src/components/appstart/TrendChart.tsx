import { useMemo } from 'react'
import type { AppStartSummary } from '../../api'
import { formatMs } from '../../appstart/format'
import { seriesPath, type SeriesPoint } from '../../replay/tracks'

const W = 600
const H = 160
const DAY_MS = 24 * 60 * 60 * 1000

function dayLabel(day: string): string {
  const d = new Date(day)
  return Number.isNaN(d.getTime()) ? day : d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

/** Daily p50 and p90 start time as two lines; days without starts are gaps, not zeros. */
export default function TrendChart({ daily }: { daily: AppStartSummary['daily'] }) {
  const chart = useMemo(() => {
    const days = daily.filter((d) => d.count > 0 && !Number.isNaN(Date.parse(d.day))).sort((a, b) => Date.parse(a.day) - Date.parse(b.day))
    if (days.length === 0) return null
    const t0 = Date.parse(days[0].day)
    const duration = Math.max(Date.parse(days[days.length - 1].day) - t0, DAY_MS)
    const max = Math.max(...days.map((d) => d.p90), 1) * 1.1
    const pts = (pick: (d: (typeof days)[number]) => number): SeriesPoint[] =>
      days.map((d) => ({ tMs: Date.parse(d.day) - t0, v: pick(d) }))
    return {
      p50: seriesPath(pts((d) => d.p50), duration, W, H, max),
      p90: seriesPath(pts((d) => d.p90), duration, W, H, max),
      max,
      // One day has no line to draw: show it as two dots in the middle instead of a speck on the edge.
      single: days.length === 1 ? { p50: days[0].p50, p90: days[0].p90 } : null,
      first: days[0].day,
      last: days[days.length - 1].day,
    }
  }, [daily])

  if (!chart) return <p className="ppl-muted">No starts in this range.</p>
  return (
    <figure className="as-trend">
      <div className="as-trend__plot">
        <span className="as-trend__y as-trend__y--top">{formatMs(chart.max)}</span>
        <span className="as-trend__y as-trend__y--bottom">0</span>
        <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label="Daily p50 and p90 start time">
          <line className="as-trend__grid" x1="0" y1={H / 2} x2={W} y2={H / 2} vectorEffect="non-scaling-stroke" />
          {!chart.single && (
            <>
              <path className="as-trend__line as-trend__line--p90" d={chart.p90} vectorEffect="non-scaling-stroke" />
              <path className="as-trend__line as-trend__line--p50" d={chart.p50} vectorEffect="non-scaling-stroke" />
            </>
          )}
        </svg>
        {chart.single && (
          <div className="as-trend__dots" aria-hidden="true">
            <span className="as-trend__dot as-trend__dot--p90" style={{ top: `${(1 - chart.single.p90 / chart.max) * 100}%` }} />
            <span className="as-trend__dot as-trend__dot--p50" style={{ top: `${(1 - chart.single.p50 / chart.max) * 100}%` }} />
          </div>
        )}
      </div>
      <div className={`as-trend__x${chart.single ? ' as-trend__x--single' : ''}`}>
        <span>{dayLabel(chart.first)}</span>
        {!chart.single && <span>{dayLabel(chart.last)}</span>}
      </div>
      <figcaption className="as-trend__legend">
        <span className="as-trend__key as-trend__key--p50">p50</span>
        <span className="as-trend__key as-trend__key--p90">p90</span>
      </figcaption>
    </figure>
  )
}
