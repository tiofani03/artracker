import { useMemo, useState } from 'react'
import type { FunnelBreakdown, FunnelStepResult } from '../../api'
import { formatEventName } from './friendlyNames'

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
  countingMode,
  executionMs,
}: Props) {
  const [viewMode, setViewMode] = useState<'columns' | 'rows'>('columns')
  const [showMatrix, setShowMatrix] = useState(false)

  const entityLabel = countingMode === 'sessions' ? 'sessions' : 'users'

  // Biggest drop-off transition
  const bottleneck = useMemo(() => {
    if (!steps || steps.length < 2) return null
    let maxDropPct = -1
    let bottleneckIdx = -1

    for (let i = 1; i < steps.length; i++) {
      const dropPct = steps[i].drop_off_pct
      if (dropPct > maxDropPct) {
        maxDropPct = dropPct
        bottleneckIdx = i
      }
    }

    if (bottleneckIdx === -1) return null

    const fromStep = steps[bottleneckIdx - 1]
    const toStep = steps[bottleneckIdx]

    return {
      stepIndex: bottleneckIdx,
      fromName: formatEventName(fromStep.event_name),
      toName: formatEventName(toStep.event_name),
      dropPct: maxDropPct,
      droppedCount: toStep.drop_off_count,
    }
  }, [steps])

  // Total median journey duration
  const totalDurationSeconds = useMemo(() => {
    if (!steps || steps.length < 2) return 0
    return steps.reduce((acc, curr) => acc + (curr.median_seconds || 0), 0)
  }, [steps])

  // Sorted breakdowns for ranked bar list
  const sortedBreakdowns = useMemo(() => {
    if (!breakdowns) return []
    return [...breakdowns].sort(
      (a, b) => b.overall_conversion_pct - a.overall_conversion_pct,
    )
  }, [breakdowns])

  if (!steps || steps.length === 0) {
    return (
      <div className="dash-empty-state">
        <p>No funnel steps defined. Add at least two steps above and click Run Query.</p>
      </div>
    )
  }

  const topCount = steps[0]?.count ?? 0
  const finalCount = steps[steps.length - 1]?.count ?? 0

  return (
    <div className="calm-funnel-view">
      {/* Calm 3-Metric Summary Strip */}
      <div className="calm-metrics-strip">
        <div className="calm-metric-card">
          <span className="calm-metric-card__value">
            {overallConversionPct.toFixed(1)}%
          </span>
          <span className="calm-metric-card__label">
            Overall Conversion ({finalCount.toLocaleString()} of {topCount.toLocaleString()} {entityLabel})
          </span>
        </div>

        {totalDurationSeconds > 0 && (
          <div className="calm-metric-card">
            <span className="calm-metric-card__value">
              {formatDuration(totalDurationSeconds)}
            </span>
            <span className="calm-metric-card__label">
              Total Median Time to Convert
            </span>
          </div>
        )}

        {bottleneck && (
          <div className="calm-metric-card calm-metric-card--alert">
            <span className="calm-metric-card__value calm-metric-card__value--alert">
              -{bottleneck.dropPct.toFixed(1)}%
            </span>
            <span className="calm-metric-card__label">
              Top Drop-off: {bottleneck.fromName} to {bottleneck.toName} ({bottleneck.droppedCount.toLocaleString()} lost)
            </span>
          </div>
        )}

        <div className="calm-metrics-strip__meta">
          <div className="dash-seg" role="group" aria-label="Funnel chart view mode">
            <button
              type="button"
              className={`dash-seg__btn${viewMode === 'columns' ? ' is-active' : ''}`}
              onClick={() => setViewMode('columns')}
            >
              Columns
            </button>
            <button
              type="button"
              className={`dash-seg__btn${viewMode === 'rows' ? ' is-active' : ''}`}
              onClick={() => setViewMode('rows')}
            >
              Rows
            </button>
          </div>
          <span className="calm-exec-pill">Query: {executionMs}ms</span>
        </div>
      </div>

      {/* Primary Chart Area */}
      {viewMode === 'columns' ? (
        /* Amplitude Waterfall Columns View */
        <div className="calm-waterfall-container">
          <div className="calm-waterfall-stage">
            {steps.map((st, idx) => {
              const isFinal = idx === steps.length - 1
              const nextStep = steps[idx + 1]
              const isBottleneckStep = bottleneck?.stepIndex === idx + 1
              const eventTitle = formatEventName(st.event_name)
              const heightPct = Math.max(st.overall_pct, 6)

              return (
                <div key={'col-node-' + st.step} className="calm-waterfall-node">
                  {/* Step Column */}
                  <div className="calm-waterfall-col">
                    <div className="calm-waterfall-col__top">
                      <strong className="calm-waterfall-col__pct">{st.overall_pct.toFixed(1)}%</strong>
                      <span className="calm-waterfall-col__count">
                        {st.count.toLocaleString()} {entityLabel}
                      </span>
                    </div>

                    <div className="calm-waterfall-col__track">
                      <div
                        className={`calm-waterfall-col__fill${isFinal ? ' is-final' : ''}`}
                        style={{ height: `${heightPct}%` }}
                      />
                    </div>

                    <div className="calm-waterfall-col__footer">
                      <span className="calm-waterfall-col__badge">Step {st.step}</span>
                      <strong className="calm-waterfall-col__title" title={st.event_name}>
                        {eventTitle}
                      </strong>
                    </div>
                  </div>

                  {/* Transition Connector Between Columns */}
                  {!isFinal && nextStep && (
                    <div className={`calm-waterfall-bridge${isBottleneckStep ? ' is-bottleneck' : ''}`}>
                      <div className="calm-waterfall-bridge__card">
                        <span className="calm-waterfall-bridge__drop">
                          -{nextStep.drop_off_pct.toFixed(1)}%
                        </span>
                        <span className="calm-waterfall-bridge__conv">
                          {nextStep.step_conv_pct.toFixed(1)}% conv
                        </span>
                        {isBottleneckStep && (
                          <span className="calm-waterfall-bridge__tag">Top Drop</span>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      ) : (
        /* Minimalist Rows View */
        <div className="calm-rows-container">
          {steps.map((st, idx) => {
            const isFinal = idx === steps.length - 1
            const nextStep = steps[idx + 1]
            const isBottleneckStep = bottleneck?.stepIndex === idx + 1
            const eventTitle = formatEventName(st.event_name)
            const widthPct = Math.max(st.overall_pct, 4)

            return (
              <div key={'row-node-' + st.step} className="calm-row-node">
                <div className="calm-row-header">
                  <div className="calm-row-header__title">
                    <span className="calm-step-pill__num">{st.step}</span>
                    <strong className="calm-row-name">{eventTitle}</strong>
                    <span className="calm-row-code">{st.event_name}</span>
                  </div>
                  <div className="calm-row-header__numbers">
                    <strong className="calm-row-count">{st.count.toLocaleString()}</strong>
                    <span className="calm-row-unit">{entityLabel}</span>
                    <span className="calm-row-pct">{st.overall_pct.toFixed(1)}%</span>
                  </div>
                </div>

                <div className="calm-row-track">
                  <div
                    className={`calm-row-fill${isFinal ? ' is-final' : ''}`}
                    style={{ width: `${widthPct}%` }}
                  />
                </div>

                {!isFinal && nextStep && (
                  <div className={`calm-row-bridge${isBottleneckStep ? ' is-bottleneck' : ''}`}>
                    <span>
                      {nextStep.step_conv_pct.toFixed(1)}% converted · -{nextStep.drop_off_pct.toFixed(1)}% drop ({nextStep.drop_off_count.toLocaleString()} lost)
                      {nextStep.median_seconds > 0 && ` · ~${formatDuration(nextStep.median_seconds)}`}
                    </span>
                    {isBottleneckStep && (
                      <span className="calm-row-bridge__tag">Top Drop</span>
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {/* Segment Breakdown: Clean Ranked Channel List */}
      {sortedBreakdowns.length > 0 && (
        <div className="calm-breakdown-card">
          <div className="calm-breakdown-card__header">
            <div>
              <h4 className="calm-breakdown-card__title">Segment Conversion Comparison</h4>
              <p className="calm-breakdown-card__sub">
                Overall funnel completion rate ranked by initial acquisition segment.
              </p>
            </div>
            <button
              type="button"
              className="dash-btn dash-btn--sm"
              onClick={() => setShowMatrix((prev) => !prev)}
            >
              {showMatrix ? 'Hide Step Matrix' : 'Show Step Matrix'}
            </button>
          </div>

          <div className="calm-breakdown-bars">
            {sortedBreakdowns.map((bd) => {
              const finalStepCount = bd.step_counts[bd.step_counts.length - 1] ?? 0
              const initialStepCount = bd.step_counts[0] ?? 0
              const barWidth = Math.max(bd.overall_conversion_pct, 4)

              return (
                <div key={bd.segment} className="calm-breakdown-row">
                  <div className="calm-breakdown-row__meta">
                    <span className="calm-breakdown-row__name">{bd.segment}</span>
                    <span className="calm-breakdown-row__stat">
                      <strong>{bd.overall_conversion_pct.toFixed(1)}%</strong> ({finalStepCount} of {initialStepCount} {entityLabel})
                    </span>
                  </div>
                  <div className="calm-breakdown-row__track">
                    <div
                      className="calm-breakdown-row__fill"
                      style={{ width: `${barWidth}%` }}
                    />
                  </div>
                </div>
              )
            })}
          </div>

          {/* Optional Step-by-Step Raw Matrix (Power User Toggle) */}
          {showMatrix && (
            <div className="calm-breakdown-matrix">
              <table className="dash-table">
                <thead>
                  <tr>
                    <th>Segment</th>
                    <th>Conversion Rate</th>
                    {steps.map((st) => (
                      <th key={'mat-th-' + st.step} style={{ textAlign: 'right' }}>
                        Step {st.step}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {sortedBreakdowns.map((bd) => (
                    <tr key={'mat-row-' + bd.segment}>
                      <td className="dash-code" style={{ fontWeight: 600 }}>
                        {bd.segment}
                      </td>
                      <td>
                        <span className="badge badge--role">
                          {bd.overall_conversion_pct.toFixed(1)}%
                        </span>
                      </td>
                      {bd.step_counts.map((cnt, i) => (
                        <td
                          key={'mat-cnt-' + i}
                          style={{ textAlign: 'right', fontFamily: 'var(--font-mono)' }}
                        >
                          {cnt.toLocaleString()}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
