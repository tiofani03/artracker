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
  execution_ms: 28,
  window_seconds: 86400,
  counting_mode: 'users',
  overall_conversion_pct: 19.8,
  steps: [
    {
      step: 1,
      event_name: 'landing_view',
      count: 12480,
      overall_pct: 100.0,
      step_conv_pct: 100.0,
      drop_off_count: 0,
      drop_off_pct: 0.0,
      median_seconds: 0,
    },
    {
      step: 2,
      event_name: 'view_product',
      count: 8620,
      overall_pct: 69.1,
      step_conv_pct: 69.1,
      drop_off_count: 3860,
      drop_off_pct: 30.9,
      median_seconds: 45,
    },
    {
      step: 3,
      event_name: 'add_to_cart',
      count: 5120,
      overall_pct: 41.0,
      step_conv_pct: 59.4,
      drop_off_count: 3500,
      drop_off_pct: 40.6,
      median_seconds: 120,
    },
    {
      step: 4,
      event_name: 'checkout_start',
      count: 3410,
      overall_pct: 27.3,
      step_conv_pct: 66.6,
      drop_off_count: 1710,
      drop_off_pct: 33.4,
      median_seconds: 180,
    },
    {
      step: 5,
      event_name: 'purchase_completed',
      count: 2470,
      overall_pct: 19.8,
      step_conv_pct: 72.4,
      drop_off_count: 940,
      drop_off_pct: 27.6,
      median_seconds: 320,
    },
  ],
  breakdowns: [
    {
      segment: 'google',
      overall_conversion_pct: 24.8,
      step_counts: [4800, 3550, 2300, 1600, 1190],
    },
    {
      segment: 'meta_ads',
      overall_conversion_pct: 18.2,
      step_counts: [3600, 2400, 1400, 920, 655],
    },
    {
      segment: 'organic',
      overall_conversion_pct: 17.5,
      step_counts: [2480, 1720, 950, 580, 434],
    },
    {
      segment: 'email',
      overall_conversion_pct: 12.1,
      step_counts: [1600, 950, 470, 310, 191],
    },
  ],
}

const DEMO_COHORT_RESPONSE: CohortResponse = {
  granularity: 'week',
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Jakarta',
  start_event: 'landing_view',
  return_event: '',
  weighted_averages: [100.0, 36.4, 25.8, 19.2, 14.8, 11.2, 8.9, 7.2],
  cohorts: [
    {
      cohort_label: 'Wk of 15 Sep',
      cohort_date: '2026-09-15T00:00:00Z',
      cohort_size: 2840,
      cells: [
        { period: 0, retained_users: 2840, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 1080, percentage: 38.0, is_future: false },
        { period: 2, retained_users: 790, percentage: 27.8, is_future: false },
        { period: 3, retained_users: 580, percentage: 20.4, is_future: false },
        { period: 4, retained_users: 450, percentage: 15.8, is_future: false },
        { period: 5, retained_users: 340, percentage: 12.0, is_future: false },
        { period: 6, retained_users: 260, percentage: 9.2, is_future: false },
        { period: 7, retained_users: 210, percentage: 7.4, is_future: false },
      ],
    },
    {
      cohort_label: 'Wk of 22 Sep',
      cohort_date: '2026-09-22T00:00:00Z',
      cohort_size: 3120,
      cells: [
        { period: 0, retained_users: 3120, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 1140, percentage: 36.5, is_future: false },
        { period: 2, retained_users: 810, percentage: 26.0, is_future: false },
        { period: 3, retained_users: 610, percentage: 19.5, is_future: false },
        { period: 4, retained_users: 460, percentage: 14.7, is_future: false },
        { period: 5, retained_users: 350, percentage: 11.2, is_future: false },
        { period: 6, retained_users: 270, percentage: 8.6, is_future: false },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 29 Sep',
      cohort_date: '2026-09-29T00:00:00Z',
      cohort_size: 3450,
      cells: [
        { period: 0, retained_users: 3450, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 1280, percentage: 37.1, is_future: false },
        { period: 2, retained_users: 890, percentage: 25.8, is_future: false },
        { period: 3, retained_users: 640, percentage: 18.5, is_future: false },
        { period: 4, retained_users: 480, percentage: 13.9, is_future: false },
        { period: 5, retained_users: 360, percentage: 10.4, is_future: false },
        { period: 6, retained_users: 0, percentage: null, is_future: true },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 06 Oct',
      cohort_date: '2026-10-06T00:00:00Z',
      cohort_size: 3820,
      cells: [
        { period: 0, retained_users: 3820, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 1390, percentage: 36.4, is_future: false },
        { period: 2, retained_users: 940, percentage: 24.6, is_future: false },
        { period: 3, retained_users: 710, percentage: 18.6, is_future: false },
        { period: 4, retained_users: 510, percentage: 13.3, is_future: false },
        { period: 5, retained_users: 0, percentage: null, is_future: true },
        { period: 6, retained_users: 0, percentage: null, is_future: true },
        { period: 7, retained_users: 0, percentage: null, is_future: true },
      ],
    },
    {
      cohort_label: 'Wk of 13 Oct',
      cohort_date: '2026-10-13T00:00:00Z',
      cohort_size: 4100,
      cells: [
        { period: 0, retained_users: 4100, percentage: 100.0, is_future: false },
        { period: 1, retained_users: 1480, percentage: 36.1, is_future: false },
        { period: 2, retained_users: 1020, percentage: 24.9, is_future: false },
        { period: 3, retained_users: 750, percentage: 18.3, is_future: false },
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

  // Load Catalog & Saved Funnels
  const loadMetadata = useCallback(async () => {
    if (!project || isDemo) return
    try {
      const [cat, sf] = await Promise.all([
        analyticsApi.catalog(project.id),
        analyticsApi.listSavedFunnels(project.id),
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
        setSteps(cat.event_names.slice(0, Math.min(cat.event_names.length, 4)).map((e) => e.name))
      }
    } catch {
      // Ignore background metadata error
    }
  }, [project, isDemo])

  useEffect(() => {
    void loadMetadata()
  }, [loadMetadata])

  // Run Funnel Query
  const runFunnel = useCallback(async () => {
    if (isDemo || !project) {
      setFunnelData(DEMO_FUNNEL_RESPONSE)
      return
    }

    setLoading(true)
    setError('')
    try {
      const days = dateRange === '7d' ? 7 : dateRange === '90d' ? 90 : 30
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString()
      const to = new Date().toISOString()

      const res = await analyticsApi.calculateFunnel(project.id, {
        steps: steps.filter((s) => s.trim() !== ''),
        from,
        to,
        window_seconds: windowSeconds,
        counting_mode: countingMode,
        breakdown_by: breakdownBy,
      })
      setFunnelData(res)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Funnel calculation failed')
    } finally {
      setLoading(false)
    }
  }, [project, isDemo, steps, dateRange, windowSeconds, countingMode, breakdownBy])

  // Run Cohort Query
  const runCohort = useCallback(async () => {
    if (isDemo || !project) {
      setCohortData(DEMO_COHORT_RESPONSE)
      return
    }

    setLoading(true)
    setError('')
    try {
      const res = await analyticsApi.calculateCohort(project.id, {
        granularity: cohortGranularity,
        periods: cohortGranularity === 'day' ? 14 : 8,
        start_event: startEvent,
        return_event: returnEvent,
        tz: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Jakarta',
      })
      setCohortData(res)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cohort calculation failed')
    } finally {
      setLoading(false)
    }
  }, [project, isDemo, cohortGranularity, startEvent, returnEvent])

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
      <div className="dash-card dash-card--compact growth-header-card">
        <div className="growth-header-main">
          <div>
            <h2 className="dash-card__title">Growth Marketing &amp; Product Analytics</h2>
            <p className="dash-card__desc">
              Multi-step funnel conversion drop-offs and N-day / N-week cohort retention analysis.
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
                Funnel Analysis
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
                  {r}
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
            <strong>No tracking events found in PostgreSQL yet</strong>
            <p>
              Send events from your Web, Android, or iOS SDK to <code>POST /api/v1/events</code>, or
              seed live realistic multi-step funnel and cohort data to try the visualization.
            </p>
          </div>
          <button
            type="button"
            className="dash-btn dash-btn--primary"
            onClick={handleSeedData}
            disabled={seeding}
          >
            {seeding ? 'Seeding Events...' : '⚡ Seed Live Preview Data into DB'}
          </button>
        </div>
      )}

      {/* Main Content Area */}
      {tab === 'funnels' ? (
        <div className="growth-funnels-layout">
          {/* Funnel Builder Controls */}
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

          {/* Funnel Bar Chart & Drop-off Visualization */}
          <section className="dash-card dash-card--compact">
            <div className="dash-card__head">
              <div>
                <h3 className="dash-card__title">Funnel Conversion &amp; Drop-off Analysis</h3>
                <p className="dash-card__desc">
                  Sequential conversion tracking with window timeout detection and drop-offs.
                </p>
              </div>
            </div>

            <FunnelChart
              steps={funnelData?.steps || []}
              breakdowns={funnelData?.breakdowns}
              overallConversionPct={funnelData?.overall_conversion_pct || 0}
              windowSeconds={funnelData?.window_seconds || windowSeconds}
              countingMode={funnelData?.counting_mode || countingMode}
              executionMs={funnelData?.execution_ms || 0}
            />
          </section>
        </div>
      ) : (
        <section className="dash-card dash-card--compact">
          <div className="dash-card__head">
            <div>
              <h3 className="dash-card__title">Cohort Retention Heatmap</h3>
              <p className="dash-card__desc">
                Percentage of active users who returned in subsequent days or weeks after their
                initial anchor event.
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
