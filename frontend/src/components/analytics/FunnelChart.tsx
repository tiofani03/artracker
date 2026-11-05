import type { FunnelBreakdown, FunnelStepResult } from '../../api'

type Props = {
  steps: FunnelStepResult[]
  breakdowns?: FunnelBreakdown[]
  overallConversionPct: number
  windowSeconds: number
  countingMode: string
  executionMs: number
}

function formatDuration(sec: number): string {
  if (sec <= 0) return '0s'
  if (sec < 60) return `${Math.round(sec)}s`
  if (sec < 3600) {
    const mins = Math.floor(sec / 60)
    const s = Math.round(sec % 60)
    return s > 0 ? `${mins}m ${s}s` : `${mins}m`
  }
  const hours = Math.floor(sec / 3600)
  const mins = Math.round((sec % 3600) / 60)
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`
}

export default function FunnelChart({
  steps,
  breakdowns,
  overallConversionPct,
  windowSeconds,
  countingMode,
  executionMs,
}: Props) {
  if (!steps || steps.length === 0) {
    return (
      <div className="dash-empty-state">
        <p>No funnel steps defined. Add at least 2 steps above and click Run.</p>
      </div>
    )
  }

  const entityLabel = countingMode === 'sessions' ? 'sessions' : 'users'
  const windowLabel = formatDuration(windowSeconds)

  return (
    <div className="funnel-chart-container">
      {/* Metrics Header Summary */}
      <div className="funnel-summary-bar">
        <div className="funnel-summary-item">
          <span className="funnel-summary-label">Overall Conversion</span>
          <strong className="funnel-summary-value funnel-summary-value--highlight">
            {overallConversionPct.toFixed(1)}%
          </strong>
        </div>
        <div className="funnel-summary-item">
          <span className="funnel-summary-label">Top of Funnel</span>
          <strong className="funnel-summary-value">
            {steps[0]?.count.toLocaleString() ?? 0} {entityLabel}
          </strong>
        </div>
        <div className="funnel-summary-item">
          <span className="funnel-summary-label">Final Conversion</span>
          <strong className="funnel-summary-value">
            {steps[steps.length - 1]?.count.toLocaleString() ?? 0} {entityLabel}
          </strong>
        </div>
        <div className="funnel-summary-item">
          <span className="funnel-summary-label">Window Duration</span>
          <strong className="funnel-summary-value">{windowLabel}</strong>
        </div>
        <div className="funnel-summary-item funnel-summary-item--speed">
          <span className="funnel-summary-label">Query Latency</span>
          <span className="badge badge--preview">⚡ {executionMs}ms</span>
        </div>
      </div>

      {/* Funnel Step Bars with Drop-off Connectors */}
      <div className="funnel-steps-list">
        {steps.map((st, idx) => {
          const widthPct = Math.max(st.overall_pct, 4) // minimum width for visibility
          const isFinal = idx === steps.length - 1

          return (
            <div key={st.step + '-' + st.event_name} className="funnel-step-block">
              {/* Main Step Bar */}
              <div className="funnel-bar-card">
                <div className="funnel-bar-header">
                  <div className="funnel-bar-title-group">
                    <span className="funnel-step-badge">Step {st.step}</span>
                    <strong className="funnel-step-name">{st.event_name}</strong>
                  </div>
                  <div className="funnel-bar-counts">
                    <strong className="funnel-bar-count-num">{st.count.toLocaleString()}</strong>
                    <span className="funnel-bar-count-unit">{entityLabel}</span>
                    <span className="funnel-bar-pct-pill">{st.overall_pct.toFixed(1)}%</span>
                  </div>
                </div>

                <div className="funnel-bar-track">
                  <div
                    className={`funnel-bar-fill${isFinal ? ' funnel-bar-fill--final' : ''}`}
                    style={{ width: `${widthPct}%` }}
                  />
                </div>
              </div>

              {/* Drop-off Card between steps */}
              {!isFinal && (
                <div className="funnel-dropoff-connector">
                  <div className="funnel-dropoff-line" />
                  <div className="funnel-dropoff-badge">
                    <span className="funnel-dropoff-arrow">↘</span>
                    <span className="funnel-dropoff-pct">
                      {(steps[idx + 1]?.step_conv_pct ?? 0).toFixed(1)}% conversion
                    </span>
                    <span className="funnel-dropoff-sep">·</span>
                    <span className="funnel-dropoff-loss">
                      {steps[idx + 1]?.drop_off_pct.toFixed(1)}% drop-off (
                      {steps[idx + 1]?.drop_off_count.toLocaleString()} dropped)
                    </span>
                    {steps[idx + 1]?.median_seconds > 0 && (
                      <>
                        <span className="funnel-dropoff-sep">·</span>
                        <span className="funnel-dropoff-time">
                          ~{formatDuration(steps[idx + 1].median_seconds)} avg time
                        </span>
                      </>
                    )}
                  </div>
                  <div className="funnel-dropoff-line" />
                </div>
              )}
            </div>
          )
        })}
      </div>

      {/* Segment Breakdown Comparison Table */}
      {breakdowns && breakdowns.length > 0 && (
        <div className="funnel-breakdown-section">
          <div className="funnel-breakdown-head">
            <h3 className="funnel-breakdown-title">Segment Performance Comparison</h3>
            <p className="funnel-breakdown-desc">
              Conversion rate breakdown attributed to the user's initial touchpoint at Step 1.
            </p>
          </div>

          <div className="dash-table-wrap">
            <table className="dash-table">
              <thead>
                <tr>
                  <th>Segment</th>
                  <th>Conv. Rate</th>
                  {steps.map((st) => (
                    <th key={'th-' + st.step} style={{ textAlign: 'right' }}>
                      Step {st.step}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {breakdowns.map((bd) => (
                  <tr key={bd.segment}>
                    <td data-label="Segment" className="dash-code">
                      {bd.segment}
                    </td>
                    <td data-label="Conv. Rate">
                      <span className="badge badge--role">
                        {bd.overall_conversion_pct.toFixed(1)}%
                      </span>
                    </td>
                    {bd.step_counts.map((cnt, i) => (
                      <td
                        key={'sc-' + i}
                        data-label={`Step ${i + 1}`}
                        style={{ textAlign: 'right' }}
                        className="dash-code"
                      >
                        {cnt.toLocaleString()}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}
