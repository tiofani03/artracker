import type { SessionPerfResponse } from '../../api'
import { describeBreach, detectBreaches, perfValueAt, summarizePerf } from '../../replay/perf'

export type PerformancePanelProps = {
  perf: SessionPerfResponse | null
  /** Playhead as an absolute epoch-ms timestamp (session start + current offset). */
  currentAbsTs: number
  startMs: number
  onSeek(offsetMs: number): void
}

const fmtOffset = (ms: number) => {
  const total = Math.max(0, Math.round(ms / 1000))
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="or-perf-stat">
      <span className="or-perf-stat__label">{label}</span>
      <span className="or-perf-stat__value">{value}</span>
      {sub && <span className="or-perf-stat__sub">{sub}</span>}
    </div>
  )
}

export function PerformancePanel({ perf, currentAbsTs, startMs, onSeek }: PerformancePanelProps) {
  if (!perf || perf.samples.length === 0) {
    return (
      <p className="dash-empty" style={{ padding: '1.5rem 1rem', margin: 0 }}>
        No profiling data for this session. Enable <code>performance</code> in the SDK config.
      </p>
    )
  }

  const refresh = perf.device.refresh_hz
  const now = perfValueAt(perf.samples, currentAbsTs, perf.interval_ms)
  const summary = summarizePerf(perf.samples)
  const breaches = detectBreaches(perf.samples, startMs, perf.interval_ms, refresh)
  const ram = perf.device.ram_mb
  // Downsampled series keep each bucket's worst value, so sums and averages over them are only approximate
  const approx = perf.downsampled ? ' (approx.)' : ''

  return (
    <div className="or-perf">
      <section aria-label="At playhead">
        <h3 className="or-perf__title">At playhead</h3>
        {now ? (
          <div className="or-perf-grid">
            {now.cpu !== undefined && <Stat label="CPU" value={`${Math.round(now.cpu)}%`} />}
            {now.mem_mb !== undefined && (
              <Stat
                label="Memory"
                value={`${Math.round(now.mem_mb)} MB`}
                sub={ram ? `${Math.round((now.mem_mb / ram) * 100)}% of ${ram} MB` : undefined}
              />
            )}
            {now.fps !== undefined && (
              <Stat label="FPS" value={String(Math.round(now.fps))} sub={refresh ? `of ${Math.round(refresh)} Hz` : undefined} />
            )}
            {now.jank !== undefined && <Stat label="Jank" value={String(now.jank)} />}
            {now.threads !== undefined && <Stat label="Threads" value={String(now.threads)} />}
            {now.thermal && <Stat label="Thermal" value={now.thermal} />}
          </div>
        ) : (
          <p className="or-perf__hint">No sample at this position.</p>
        )}
      </section>

      <section aria-label="Session summary">
        <h3 className="or-perf__title">Session summary</h3>
        <div className="or-perf-grid">
          {summary.cpuPeak !== undefined && (
            <Stat label={`CPU peak / avg${approx}`} value={`${Math.round(summary.cpuPeak)}% / ${Math.round(summary.cpuAvg ?? 0)}%`} />
          )}
          {summary.memPeak !== undefined && <Stat label="Memory peak" value={`${Math.round(summary.memPeak)} MB`} />}
          <Stat label={`Jank frames${approx}`} value={String(summary.jankTotal)} />
          <Stat label={`Frozen frames${approx}`} value={String(summary.frozenTotal)} />
        </div>
      </section>

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
                  <span className="badge badge--console">{d.badge}</span>
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
