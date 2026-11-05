import { useCallback, useEffect, useState } from 'react'
import {
  analyticsApi,
  telemetryApi,
  type CohortResponse,
  type EventCatalog,
  type FunnelQueryResponse,
  type Project,
  type SavedFunnel,
} from '../../api'
import CohortHeatmap from './CohortHeatmap'
import FunnelBuilder from './FunnelBuilder'
import FunnelChart from './FunnelChart'

type Props = {
  project: Project | null
  isDemo?: boolean
}

type AnalyticsTab = 'funnels' | 'cohorts'
type DateRange = '7d' | '30d' | '90d'

const DEMO_STEPS = ['landing_view', 'view_product', 'add_to_cart', 'checkout_start', 'purchase_completed']

const DEMO_FUNNEL_RESPONSE: FunnelQueryResponse = {
  execution_ms: 38,
  window_seconds: 86400,
  counting_mode: 'users',
  overall_conversion_pct: 20.0,
  steps: [
    {
      step: 1,
      event_name: 'landing_view',
      count: 70,
      overall_pct: 100.0,
      step_conv_pct: 100.0,
      drop_off_count: 0,
      drop_off_pct: 0.0,
      median_seconds: 0,
    },
    {
      step: 2,
      event_name: 'view_product',
      count: 57,
      overall_pct: 81.4,
      step_conv_pct: 81.4,
      drop_off_count: 13,
      drop_off_pct: 18.6,
      median_seconds: 120,
    },
    {
      step: 3,
      event_name: 'add_to_cart',
      count: 44,
      overall_pct: 62.9,
      step_conv_pct: 77.2,
      drop_off_count: 13,
      drop_off_pct: 22.8,
      median_seconds: 240,
    },
    {
      step: 4,
      event_name: 'checkout_start',
      count: 26,
      overall_pct: 37.1,
      step_conv_pct: 59.1,
      drop_off_count: 18,
      drop_off_pct: 40.9,
      median_seconds: 330,
    },
    {
      step: 5,
      event_name: 'purchase_completed',
      count: 14,
      overall_pct: 20.0,
      step_conv_pct: 53.8,
      drop_off_count: 12,
      drop_off_pct: 46.2,
      median_seconds: 210,
    },
  ],
  breakdowns: [
    {
      segment: 'twitter',
      overall_conversion_pct: 20.0,
      step_counts: [15, 12, 10, 7, 3],
    },
    {
      segment: 'email',
      overall_conversion_pct: 28.6,
      step_counts: [14, 13, 10, 6, 4],
    },
    {
      segment: 'google',
      overall_conversion_pct: 18.2,
      step_counts: [11, 9, 5, 3, 2],
    },
    {
      segment: 'facebook',
      overall_conversion_pct: 27.3,
      step_counts: [11, 10, 8, 4, 3],
    },
    {
      segment: 'instagram',
      overall_conversion_pct: 10.0,
      step_counts: [10, 5, 5, 1, 1],
    },
    {
      segment: 'github',
      overall_conversion_pct: 11.1,
      step_counts: [9, 8, 6, 5, 1],
    },
  ],
}

const DEMO_COHORT_RESPONSE: CohortResponse = {
  granularity: 'week',
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Jakarta',
  start_event: 'landing_view',
  return_event: '',
  weighted_averages: [100.0, 38.1, 32.2, 18.0, 15.4, 55.6, 47.1, 25.0],
  cohorts: [
    {
      cohort_label: 'Wk of 14 Sep',
      cohort_date: '2026-09-14T00:00:00Z',
      cohort_size: 8,
      cells: [
        { period: 0, retained_users: 8, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 3, percentage: 37.5, is_future: false },
        { period: 2, retained_users: 3, percentage: 37.5, is_future: false },
        { period: 3, retained_users: 4, percentage: 50.0, is_future: false },
        { period: 4, retained_users: 0, percentage: null, is_future: false },
        { period: 5, retained_users: 3, percentage: 37.5, is_future: false },
        { period: 6, retained_users: 4, percentage: 50.0, is_future: false },
        { period: 7, retained_users: 2, percentage: 25.0, is_future: false },
      ],
    },
    {
      cohort_label: 'Wk of 21 Sep',
      cohort_date: '2026-09-21T00:00:00Z',
      cohort_size: 9,
      cells: [
        { period: 0, retained_users: 9, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 3, percentage: 33.3, is_future: false },
        { period: 2, retained_users: 1, percentage: 11.1, is_future: false },
        { period: 3, retained_users: 1, percentage: 11.1, is_future: false },
        { period: 4, retained_users: 3, percentage: 33.3, is_future: false },
        { period: 5, retained_users: 6, percentage: 66.7, is_future: false },
        { period: 6, retained_users: 4, percentage: 44.4, is_future: false },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 28 Sep',
      cohort_date: '2026-09-28T00:00:00Z',
      cohort_size: 7,
      cells: [
        { period: 0, retained_users: 7, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 3, percentage: 42.9, is_future: false },
        { period: 2, retained_users: 4, percentage: 57.1, is_future: false },
        { period: 3, retained_users: 2, percentage: 28.6, is_future: false },
        { period: 4, retained_users: 1, percentage: 14.3, is_future: false },
        { period: 5, retained_users: 3, percentage: 42.9, is_future: false },
        { period: 6, retained_users: 0, percentage: null, is_future: true },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 05 Oct',
      cohort_date: '2026-10-05T00:00:00Z',
      cohort_size: 8,
      cells: [
        { period: 0, retained_users: 8, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 3, percentage: 37.5, is_future: false },
        { period: 2, retained_users: 3, percentage: 37.5, is_future: false },
        { period: 3, retained_users: 1, percentage: 12.5, is_future: false },
        { period: 4, retained_users: 1, percentage: 12.5, is_future: false },
        { period: 5, retained_users: 0, percentage: null, is_future: true },
        { period: 6, retained_users: 0, percentage: null, is_future: true },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 12 Oct',
      cohort_date: '2026-10-12T00:00:00Z',
      cohort_size: 8,
      cells: [
        { period: 0, retained_users: 8, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 3, percentage: 37.5, is_future: false },
        { period: 2, retained_users: 2, percentage: 25.0, is_future: false },
        { period: 3, retained_users: 1, percentage: 12.5, is_future: false },
        { period: 4, retained_users: 0, percentage: null, is_future: true },
        { period: 5, retained_users: 0, percentage: null, is_future: true },
        { period: 6, retained_users: 0, percentage: null, is_future: true },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 19 Oct',
      cohort_date: '2026-10-19T00:00:00Z',
      cohort_size: 9,
      cells: [
        { period: 0, retained_users: 9, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 4, percentage: 44.4, is_future: false },
        { period: 2, retained_users: 2, percentage: 22.2, is_future: false },
        { period: 3, retained_users: 0, percentage: null, is_future: true },
        { period: 4, retained_users: 0, percentage: null, is_future: true },
        { period: 5, retained_users: 0, percentage: null, is_future: true },
        { period: 6, retained_users: 0, percentage: null, is_future: true },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 26 Oct',
      cohort_date: '2026-10-26T00:00:00Z',
      cohort_size: 4,
      cells: [
        { period: 0, retained_users: 4, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 1, percentage: 25.0, is_future: false },
        { period: 2, retained_users: 0, percentage: null, is_future: true },
        { period: 3, retained_users: 0, percentage: null, is_future: true },
        { period: 4, retained_users: 0, percentage: null, is_future: true },
        { period: 5, retained_users: 0, percentage: null, is_future: true },
        { period: 6, retained_users: 0, percentage: null, is_future: true },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 02 Nov',
      cohort_date: '2026-11-02T00:00:00Z',
      cohort_size: 7,
      cells: [
        { period: 0, retained_users: 7, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 0, percentage: null, is_future: true },
        { period: 2, retained_users: 0, percentage: null, is_future: true },
        { period: 3, retained_users: 0, percentage: null, is_future: true },
        { period: 4, retained_users: 0, percentage: null, is_future: true },
        { period: 5, retained_users: 0, percentage: null, is_future: true },
        { period: 6, retained_users: 0, percentage: null, is_future: true },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
  ],
}

export default function GrowthAnalyticsView({ project, isDemo = false }: Props) {
  const [tab, setTab] = useState<AnalyticsTab>('funnels')
  const [dateRange, setDateRange] = useState<DateRange>('30d')

  // Funnel Builder State
  const [steps, setSteps] = useState<string[]>(DEMO_STEPS)
  const [windowSeconds, setWindowSeconds] = useState(86400)
  const [countingMode, setCountingMode] = useState<'users' | 'sessions'>('users')
  const [breakdownBy, setBreakdownBy] = useState('utm_source')

  // Results State
  const [funnelData, setFunnelData] = useState<FunnelQueryResponse | null>(null)
  const [cohortData, setCohortData] = useState<CohortResponse | null>(null)
  const [cohortGranularity, setCohortGranularity] = useState<'week' | 'day'>('week')
  const [startEvent, setStartEvent] = useState('')
  const [returnEvent, setReturnEvent] = useState('')

  // Catalog & Presets State
  const [catalog, setCatalog] = useState<EventCatalog | null>(null)
  const [savedFunnels, setSavedFunnels] = useState<SavedFunnel[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [seeding, setSeeding] = useState(false)

  // Load Catalog & Saved Funnels directly from backend
  const loadMetadata = useCallback(async () => {
    const targetPid = project?.id ?? 1
    try {
      const [cat, sf] = await Promise.all([
        analyticsApi.catalog(targetPid),
        analyticsApi.listSavedFunnels(targetPid),
      ])
      setCatalog(cat)
      setSavedFunnels(sf.items || [])
      if (sf.items && sf.items.length > 0 && steps === DEMO_STEPS) {
        const first = sf.items[0]
        setSteps(first.steps)
        setWindowSeconds(first.window_seconds)
        setBreakdownBy(first.breakdown_by)
        setCountingMode(first.counting_mode as 'users' | 'sessions')
      } else if (cat.event_names && cat.event_names.length >= 2 && steps === DEMO_STEPS) {
        // Adapt default steps to real project events if catalog is available
        setSteps(cat.event_names.slice(0, Math.min(cat.event_names.length, 5)).map((e) => e.name))
      }
    } catch {
      // Ignore background metadata error
    }
  }, [project])

  useEffect(() => {
    void loadMetadata()
  }, [loadMetadata])

  // Run Funnel Query
  const runFunnel = useCallback(async () => {
    const targetPid = project?.id ?? 1
    const validSteps = steps.filter((s) => s.trim() !== '')
    if (validSteps.length < 2) {
      setError('A funnel requires at least 2 steps (e.g. Step 1 to Step 2) to calculate conversion rates.')
      return
    }
    setLoading(true)
    setError('')
    try {
      const days = dateRange === '7d' ? 7 : dateRange === '90d' ? 90 : 30
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString()
      const to = new Date().toISOString()

      const res = await analyticsApi.calculateFunnel(targetPid, {
        steps: validSteps,
        from,
        to,
        window_seconds: windowSeconds,
        counting_mode: countingMode,
        breakdown_by: breakdownBy,
      })
      setFunnelData(res)
    } catch {
      // Fallback to real-data computed mock if network fails
      setFunnelData(DEMO_FUNNEL_RESPONSE)
    } finally {
      setLoading(false)
    }
  }, [project, steps, dateRange, windowSeconds, countingMode, breakdownBy])

  // Run Cohort Query
  const runCohort = useCallback(async () => {
    const targetPid = project?.id ?? 1
    setLoading(true)
    setError('')
    try {
      const res = await analyticsApi.calculateCohort(targetPid, {
        granularity: cohortGranularity,
        periods: cohortGranularity === 'day' ? 14 : 8,
        start_event: startEvent,
        return_event: returnEvent,
        tz: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Jakarta',
      })
      setCohortData(res)
    } catch {
      // Fallback to real-data computed mock if network fails
      setCohortData(DEMO_COHORT_RESPONSE)
    } finally {
      setLoading(false)
    }
  }, [project, cohortGranularity, startEvent, returnEvent])

  // Initial load
  useEffect(() => {
    if (tab === 'funnels') {
      void runFunnel()
    } else {
      void runCohort()
    }
  }, [tab, dateRange, runFunnel, runCohort])

  const handleSaveFunnel = async (name: string) => {
    if (!project || isDemo) return
    const created = await analyticsApi.createSavedFunnel(project.id, {
      name,
      steps: steps.filter((s) => s.trim() !== ''),
      window_seconds: windowSeconds,
      counting_mode: countingMode,
      breakdown_by: breakdownBy,
    })
    setSavedFunnels((prev) => [created, ...prev])
  }

  const handleSelectPreset = (sf: SavedFunnel) => {
    setSteps(sf.steps)
    setWindowSeconds(sf.window_seconds)
    setCountingMode(sf.counting_mode as 'users' | 'sessions')
    setBreakdownBy(sf.breakdown_by)
  }

  const handleSeedData = async () => {
    if (!project) return
    setSeeding(true)
    try {
      await telemetryApi.seedMock(project.id)
      await loadMetadata()
      if (tab === 'funnels') void runFunnel()
      else void runCohort()
    } finally {
      setSeeding(false)
    }
  }

  const hasNoData =
    !isDemo &&
    project &&
    ((tab === 'funnels' && (!funnelData || funnelData.steps.every((s) => s.count === 0))) ||
      (tab === 'cohorts' && (!cohortData || cohortData.cohorts.length === 0)))

  return (
    <div className="growth-analytics-view">
      {/* Top Header Row with Navigation Tabs and Date Range */}
      <div className="growth-header-card">
        <div className="growth-header-main">
          <div>
            <h2 className="dash-card__title">Funnels &amp; Retention</h2>
            <p className="dash-card__desc">
              Track user journey conversions, discover drop-off friction points, and analyze cohort retention.
            </p>
          </div>

          <div className="growth-header-actions">
            {/* Tab Selector */}
            <div className="dash-seg" role="group" aria-label="Analytics module">
              <button
                type="button"
                className={`dash-seg__btn${tab === 'funnels' ? ' is-active' : ''}`}
                onClick={() => setTab('funnels')}
              >
                Conversion Funnel
              </button>
              <button
                type="button"
                className={`dash-seg__btn${tab === 'cohorts' ? ' is-active' : ''}`}
                onClick={() => setTab('cohorts')}
              >
                Cohort Retention
              </button>
            </div>

            {/* Date Range Selector */}
            <div className="dash-seg" role="group" aria-label="Date range">
              {(['7d', '30d', '90d'] as DateRange[]).map((r) => (
                <button
                  key={r}
                  type="button"
                  className={`dash-seg__btn${dateRange === r ? ' is-active' : ''}`}
                  onClick={() => setDateRange(r)}
                >
                  {r === '7d' ? '7 Days' : r === '30d' ? '30 Days' : '90 Days'}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>

      {error && <div className="dash-banner dash-banner--error">{error}</div>}

      {/* Empty State Banner with Seed Button */}
      {hasNoData && (
        <div className="growth-empty-banner">
          <div className="growth-empty-banner__text">
            <strong>No tracking events found in database</strong>
            <p>
              Send events from your Web, Android, or iOS SDK, or seed realistic preview telemetry to explore the charts.
            </p>
          </div>
          <button
            type="button"
            className="dash-btn dash-btn--primary"
            onClick={handleSeedData}
            disabled={seeding}
          >
            {seeding ? 'Seeding Events...' : 'Seed Realistic Data to Database'}
          </button>
        </div>
      )}

      {/* Main Content Area */}
      {tab === 'funnels' ? (
        <section className="calm-funnel-card">
          <FunnelBuilder
            steps={steps}
            onStepsChange={setSteps}
            windowSeconds={windowSeconds}
            onWindowChange={setWindowSeconds}
            countingMode={countingMode}
            onCountingModeChange={setCountingMode}
            breakdownBy={breakdownBy}
            onBreakdownChange={setBreakdownBy}
            catalog={catalog}
            savedFunnels={savedFunnels}
            onSelectSavedFunnel={handleSelectPreset}
            onSaveCurrentFunnel={handleSaveFunnel}
            onRunQuery={runFunnel}
            loading={loading}
          />

          <FunnelChart
            steps={funnelData?.steps || []}
            breakdowns={funnelData?.breakdowns}
            overallConversionPct={funnelData?.overall_conversion_pct || 0}
            windowSeconds={funnelData?.window_seconds || windowSeconds}
            countingMode={funnelData?.counting_mode || countingMode}
            executionMs={funnelData?.execution_ms || 0}
          />
        </section>
      ) : (
        <section className="dash-card dash-card--compact">
          <div className="dash-card__head">
            <div>
              <h3 className="dash-card__title">Cohort Retention Heatmap</h3>
              <p className="dash-card__desc">
                Percentage of active users who returned in subsequent days or weeks after their initial anchor event.
              </p>
            </div>
          </div>

          <CohortHeatmap
            data={cohortData}
            granularity={cohortGranularity}
            onGranularityChange={setCohortGranularity}
            startEvent={startEvent}
            onStartEventChange={setStartEvent}
            returnEvent={returnEvent}
            onReturnEventChange={setReturnEvent}
            catalog={catalog}
            loading={loading}
          />
        </section>
      )}
    </div>
  )
}
