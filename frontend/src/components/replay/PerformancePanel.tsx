import type { PerfSample, SessionPerfResponse } from '../../api'
import { describeBreach, detectBreaches, memTrackMax, perfValueAt, summarizePerf, toSeries } from '../../replay/perf'
import { seriesPath } from '../../replay/tracks'

export type PerformancePanelProps = {
  perf: SessionPerfResponse | null
  /** Playhead as an absolute epoch-ms timestamp (session start + current offset). */
  currentAbsTs: number
  startMs: number
  onSeek(offsetMs: number): void
}

const SPARK_H = 20

const fmtOffset = (ms: number) => {
  const total = Math.max(0, Math.round(ms / 1000))
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}

type SparkProps = {
  samples: PerfSample[]
  metric: 'cpu' | 'mem_mb' | 'fps'
  max: number
  threshold?: number
  playheadAbsTs: number
}

/** Whole-session trend of one metric with the playhead marked, so "now" is read against the whole run. */
function Sparkline({ samples, metric, max, threshold, playheadAbsTs }: SparkProps) {
  const t0 = samples[0].t
  const span = Math.max(1, samples[samples.length - 1].t - t0)
  const d = seriesPath(toSeries(samples, metric, t0), span, 100, SPARK_H, max)
  const y = threshold === undefined ? null : SPARK_H - Math.min(1, Math.max(0, threshold / max)) * SPARK_H
  const x = Math.min(100, Math.max(0, ((playheadAbsTs - t0) / span) * 100))
  return (
    <svg className="or-spark" viewBox={`0 0 100 ${SPARK_H}`} preserveAspectRatio="none" aria-hidden="true">
      {y !== null && <line className="or-track-threshold" x1={0} x2={100} y1={y} y2={y} />}
      <path d={d} className="or-spark__line" />
      <line className="or-spark__now" x1={x} x2={x} y1={0} y2={SPARK_H} />
    </svg>
  )
}

type Row = { key: string; label: string; now: string; session: string; spark?: SparkProps }

export function PerformancePanel({ perf, currentAbsTs, startMs, onSeek }: PerformancePanelProps) {
  if (!perf || perf.samples.length === 0) {
    return (
      <p className="dash-empty" style={{ padding: '1.5rem 1rem', margin: 0 }}>
        No profiling data for this session. Enable <code>performance</code> in the SDK config.
      </p>
    )
  }

  const samples = perf.samples
  const refresh = perf.device.refresh_hz
  const ram = perf.device.ram_mb
  const at = perfValueAt(samples, currentAbsTs, perf.interval_ms)
  const sum = summarizePerf(samples)
  const breaches = detectBreaches(samples, startMs, perf.interval_ms, refresh)
  const has = (m: string) => perf.metrics.includes(m)
  const dash = '–'
  const round = (n: number | undefined) => (n === undefined ? dash : String(Math.round(n)))

  const rows: Row[] = []
  if (has('cpu')) {
    rows.push({
      key: 'cpu',
      label: 'CPU',
      now: at?.cpu === undefined ? dash : `${Math.round(at.cpu)}%`,
      session: `peak ${round(sum.cpuPeak)}% · avg ${round(sum.cpuAvg)}%`,
      spark: { samples, metric: 'cpu', max: 100, threshold: 80, playheadAbsTs: currentAbsTs },
    })
  }
  if (has('mem_mb')) {
    const pct = at?.mem_mb !== undefined && ram ? ` (${Math.round((at.mem_mb / ram) * 100)}%)` : ''
    rows.push({
      key: 'mem',
      label: 'Memory',
      now: at?.mem_mb === undefined ? dash : `${Math.round(at.mem_mb)} MB${pct}`,
      session: `peak ${round(sum.memPeak)} MB`,
      spark: { samples, metric: 'mem_mb', max: memTrackMax(samples), playheadAbsTs: currentAbsTs },
    })
  }
  if (has('fps')) {
    const hz = refresh ?? 60
    rows.push({
      key: 'fps',
      label: 'FPS',
      now: round(at?.fps),
      session: `lowest ${round(sum.fpsMin)}`,
      spark: { samples, metric: 'fps', max: hz, threshold: hz * 0.5, playheadAbsTs: currentAbsTs },
    })
  }
  if (has('jank')) rows.push({ key: 'jank', label: 'Jank frames', now: round(at?.jank), session: `total ${sum.jankTotal}` })
  if (has('frozen')) rows.push({ key: 'frozen', label: 'Frozen frames', now: round(at?.frozen), session: `total ${sum.frozenTotal}` })
  if (has('threads')) rows.push({ key: 'threads', label: 'Threads', now: round(at?.threads), session: `peak ${round(sum.threadsPeak)}` })
  if (has('thermal')) rows.push({ key: 'thermal', label: 'Thermal', now: at?.thermal ?? dash, session: dash })

  return (
    <div className="or-perf">
      <table className="or-perf-table">
        <thead>
          <tr>
            <th scope="col">Metric</th>
            <th scope="col">Now</th>
            <th scope="col">Session</th>
            <th scope="col">Trend</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.key}>
              <th scope="row">{r.label}</th>
              <td className="or-perf-table__num">{r.now}</td>
              <td className="or-perf-table__session">{r.session}</td>
              <td className="or-perf-table__trend">{r.spark ? <Sparkline {...r.spark} /> : null}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {perf.downsampled && (
        <p className="or-perf__note">Averages and frame totals are approximate: this long session was downsampled.</p>
      )}

      <section aria-label="Breaches">
        <h3 className="or-perf__title">Breaches ({breaches.length})</h3>
        {breaches.length === 0 ? (
          <p className="or-perf__hint">No thresholds were crossed.</p>
        ) : (
          <div className="or-perf-breaches">
            {breaches.map((b) => {
              const d = describeBreach(b, refresh)
              return (
                <button
                  key={`${b.kind}-${b.startMs}`}
                  type="button"
                  className="or-perf-breach"
                  onClick={() => onSeek(b.startMs)}
                >
                  <span className="dash-code or-activity-time">{fmtOffset(b.startMs)}</span>
                  <span className="or-perf-breach__kind">{d.badge}</span>
                  <span className="or-activity-text">{d.text}</span>
                </button>
              )
            })}
          </div>
        )}
      </section>
    </div>
  )
}
