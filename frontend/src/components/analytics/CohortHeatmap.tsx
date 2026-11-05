import type { CohortResponse, EventCatalog } from '../../api'

type Props = {
  data: CohortResponse | null
  granularity: 'week' | 'day'
  onGranularityChange: (g: 'week' | 'day') => void
  startEvent: string
  onStartEventChange: (e: string) => void
  returnEvent: string
  onReturnEventChange: (e: string) => void
  catalog?: EventCatalog | null
  loading: boolean
}

export default function CohortHeatmap({
  data,
  granularity,
  onGranularityChange,
  startEvent,
  onStartEventChange,
  returnEvent,
  onReturnEventChange,
  catalog,
  loading,
}: Props) {
  const periodLabel = granularity === 'day' ? 'Day' : 'Week'
  const timezone = data?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'

  if (!data || data.cohorts.length === 0) {
    return (
      <div className="dash-empty-state">
        <p>No cohort retention records found for this period. Try sending events or seeding data.</p>
      </div>
    )
  }

  const periodsCount = data.weighted_averages.length

  return (
    <div className="cohort-heatmap-panel">
      {/* Top Filter & Control Row */}
      <div className="cohort-controls-bar">
        <div className="cohort-controls-left">
          {/* Granularity Toggle */}
          <div className="dash-seg" role="group" aria-label="Cohort time granularity">
            <button
              type="button"
              className={`dash-seg__btn${granularity === 'week' ? ' is-active' : ''}`}
              onClick={() => onGranularityChange('week')}
            >
              Weekly Retention
            </button>
            <button
              type="button"
              className={`dash-seg__btn${granularity === 'day' ? ' is-active' : ''}`}
              onClick={() => onGranularityChange('day')}
            >
              Daily Retention
            </button>
          </div>

          {/* Start Event */}
          <div className="cohort-event-filter">
            <label htmlFor="cohort-start-event" className="funnel-ctrl-label">
              Anchor:
            </label>
            <input
              id="cohort-start-event"
              type="text"
              className="dash-input dash-input--sm"
              placeholder="First seen (any event)"
              value={startEvent}
              onChange={(e) => onStartEventChange(e.target.value)}
              list="cohort-start-events-list"
            />
            <datalist id="cohort-start-events-list">
              {catalog?.event_names?.map((en) => (
                <option key={'start-' + en.name} value={en.name}>
                  {en.name}
                </option>
              ))}
            </datalist>
          </div>

          {/* Return Event */}
          <div className="cohort-event-filter">
            <label htmlFor="cohort-return-event" className="funnel-ctrl-label">
              Return:
            </label>
            <input
              id="cohort-return-event"
              type="text"
              className="dash-input dash-input--sm"
              placeholder="Any active event"
              value={returnEvent}
              onChange={(e) => onReturnEventChange(e.target.value)}
              list="cohort-return-events-list"
            />
            <datalist id="cohort-return-events-list">
              {catalog?.event_names?.map((en) => (
                <option key={'return-' + en.name} value={en.name}>
                  {en.name}
                </option>
              ))}
            </datalist>
          </div>
        </div>

        <div className="cohort-controls-right">
          <span className="badge badge--preview" title="Cohort boundary cutoffs align to local midnight">
            🌐 {timezone}
          </span>
          {loading && <span className="dash-code">Calculating...</span>}
        </div>
      </div>

      {/* Cohort Matrix Table with OKLCH Gradient Shading */}
      <div className="cohort-table-wrapper">
        <table className="cohort-matrix-table">
          <thead>
            <tr>
              <th className="cohort-th-sticky">Cohort</th>
              <th className="cohort-th-size">Users</th>
              {Array.from({ length: periodsCount }).map((_, p) => (
                <th key={'head-p-' + p} className="cohort-th-period">
                  {periodLabel} {p}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {/* Weighted Average Summary Row */}
            <tr className="cohort-row-average">
              <td className="cohort-td-sticky">
                <strong>Weighted Average</strong>
              </td>
              <td className="cohort-td-size">
                {data.cohorts
                  .reduce((sum, c) => sum + c.cohort_size, 0)
                  .toLocaleString()}
              </td>
              {data.weighted_averages.map((avgPct, p) => (
                <td
                  key={'avg-cell-' + p}
                  className="cohort-cell"
                  style={{
                    backgroundColor: `color-mix(in oklch, var(--color-accent) ${Math.min(
                      Math.max(avgPct * 0.85, 4),
                      90,
                    )}%, var(--color-paper-2))`,
                    color: avgPct > 52 ? 'var(--color-paper-hi)' : 'var(--color-ink)',
                  }}
                  title={`Average retention at ${periodLabel} ${p}: ${avgPct.toFixed(1)}%`}
                >
                  <strong>{avgPct.toFixed(1)}%</strong>
                </td>
              ))}
            </tr>

            {/* Individual Cohort Rows */}
            {data.cohorts.map((row) => (
              <tr key={'cohort-' + row.cohort_label}>
                <td className="cohort-td-sticky">
                  <span className="cohort-label-name">{row.cohort_label}</span>
                </td>
                <td className="cohort-td-size">{row.cohort_size.toLocaleString()}</td>
                {row.cells.map((cell) => {
                  if (cell.is_future) {
                    return (
                      <td
                        key={'cell-p-' + cell.period}
                        className="cohort-cell cohort-cell--future"
                        title="Period is in the future"
                      >
                        <span className="cohort-dash">—</span>
                      </td>
                    )
                  }

                  const pct = cell.percentage ?? 0
                  // High contrast: switch to white text if background saturation/lightness is high
                  const textColor = pct > 52 ? 'var(--color-paper-hi)' : 'var(--color-ink)'
                  const bgOpacity = Math.min(Math.max(pct * 0.85, 3), 90)

                  return (
                    <td
                      key={'cell-p-' + cell.period}
                      className="cohort-cell"
                      style={{
                        backgroundColor: `color-mix(in oklch, var(--color-accent) ${bgOpacity}%, var(--color-paper-2))`,
                        color: textColor,
                      }}
                      title={`${cell.retained_users.toLocaleString()} of ${row.cohort_size.toLocaleString()} users returned (${pct.toFixed(
                        1,
                      )}%)`}
                    >
                      <span className="cohort-cell-pct">{pct.toFixed(1)}%</span>
                      <span className="cohort-cell-sub">
                        {cell.retained_users.toLocaleString()}
                      </span>
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
