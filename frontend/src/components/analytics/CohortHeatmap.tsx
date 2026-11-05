import { useMemo } from 'react'
import type { CohortResponse, EventCatalog } from '../../api'
import CustomSelect, { type CustomSelectOption } from '../CustomSelect'
import { formatEventName } from './friendlyNames'

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

  // Summary retention numbers
  const retentionStats = useMemo(() => {
    if (!data || !data.weighted_averages || data.weighted_averages.length === 0) {
      return null
    }
    const week1Avg = data.weighted_averages[1] ?? 0
    const week4Avg =
      data.weighted_averages[4] ??
      (data.weighted_averages[data.weighted_averages.length - 1] ?? 0)
    const totalTrackedUsers = data.cohorts.reduce((sum, c) => sum + c.cohort_size, 0)

    return {
      week1Avg,
      week4Avg,
      totalTrackedUsers,
      totalCohorts: data.cohorts.length,
    }
  }, [data])

  if (!data || data.cohorts.length === 0) {
    return (
      <div className="dash-empty-state">
        <p>No cohort retention records found for this period. Send events or seed data to compute.</p>
      </div>
    )
  }

  const periodsCount = data.weighted_averages.length

  const startEventOptions: CustomSelectOption<string>[] = [
    { value: '', label: 'First Seen (Any Event)', meta: 'All new users' },
    ...(catalog?.event_names?.map((en) => ({
      value: en.name,
      label: `${formatEventName(en.name)} (${en.name})`,
      badge: `${en.count.toLocaleString()} occurrences`,
    })) || []),
  ]

  const returnEventOptions: CustomSelectOption<string>[] = [
    { value: '', label: 'Any Active Return Event', meta: 'Any subsequent user activity' },
    ...(catalog?.event_names?.map((en) => ({
      value: en.name,
      label: `${formatEventName(en.name)} (${en.name})`,
      badge: `${en.count.toLocaleString()} occurrences`,
    })) || []),
  ]

  return (
    <div className="amplitude-cohort-container">
      {/* MoEngage / Amplitude KPI Scorecards */}
      {retentionStats && (
        <div className="amplitude-kpi-grid">
          <div className="amplitude-kpi-card amplitude-kpi-card--primary">
            <span className="amplitude-kpi-label">{periodLabel} 1 Retention</span>
            <div className="amplitude-kpi-value">
              {retentionStats.week1Avg.toFixed(1)}%
            </div>
            <span className="amplitude-kpi-sub">
              Weighted average return rate across all cohorts in {periodLabel.toLowerCase()} 1
            </span>
          </div>

          <div className="amplitude-kpi-card">
            <span className="amplitude-kpi-label">Day 30+ Retention</span>
            <div className="amplitude-kpi-value">
              {retentionStats.week4Avg.toFixed(1)}%
            </div>
            <span className="amplitude-kpi-sub">
              Long-term retained active user baseline
            </span>
          </div>

          <div className="amplitude-kpi-card">
            <span className="amplitude-kpi-label">Tracked User Cohorts</span>
            <div className="amplitude-kpi-value">
              {retentionStats.totalTrackedUsers.toLocaleString()}
            </div>
            <span className="amplitude-kpi-sub">
              Across {retentionStats.totalCohorts} {granularity}ly cohorts
            </span>
          </div>
        </div>
      )}

      {/* Query Bar (MoEngage Style) */}
      <div className="amplitude-cohort-query-bar">
        <div className="amplitude-cohort-query-left">
          {/* Granularity Toggle */}
          <div className="dash-seg" role="group" aria-label="Cohort time granularity">
            <button
              type="button"
              className={`dash-seg__btn${granularity === 'week' ? ' is-active' : ''}`}
              onClick={() => onGranularityChange('week')}
            >
              Weekly Cohorts
            </button>
            <button
              type="button"
              className={`dash-seg__btn${granularity === 'day' ? ' is-active' : ''}`}
              onClick={() => onGranularityChange('day')}
            >
              Daily Cohorts
            </button>
          </div>

          {/* Anchor Event */}
          <div className="amplitude-query-item">
            <span className="amplitude-query-label">First Performed:</span>
            <CustomSelect
              size="sm"
              searchable
              options={startEventOptions}
              value={startEvent}
              onChange={onStartEventChange}
              placeholder="First seen event"
              ariaLabel="Select anchor event"
            />
          </div>

          {/* Return Event */}
          <div className="amplitude-query-item">
            <span className="amplitude-query-label">Then Returned:</span>
            <CustomSelect
              size="sm"
              searchable
              options={returnEventOptions}
              value={returnEvent}
              onChange={onReturnEventChange}
              placeholder="Any return event"
              ariaLabel="Select return event"
            />
          </div>
        </div>

        <div className="amplitude-cohort-query-right">
          <span className="badge badge--preview">
            Timezone: {timezone}
          </span>
          {loading && <span className="dash-code">Evaluating...</span>}
        </div>
      </div>

      {/* Cohort Retention Heatmap Table */}
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
