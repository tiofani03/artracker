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

const DEFAULT_STEPS = ['landing_view', 'view_product', 'add_to_cart', 'checkout_start', 'purchase_completed']

export default function GrowthAnalyticsView({ project, isDemo = false }: Props) {
  const [tab, setTab] = useState<AnalyticsTab>('funnels')
  const [dateRange, setDateRange] = useState<DateRange>('30d')

  // Funnel Builder State
  const [steps, setSteps] = useState<string[]>(DEFAULT_STEPS)
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
    if (!project) return
    const targetPid = project.id
    try {
      const [cat, sf] = await Promise.all([
        analyticsApi.catalog(targetPid),
        analyticsApi.listSavedFunnels(targetPid),
      ])
      setCatalog(cat)
      setSavedFunnels(sf.items || [])
      if (sf.items && sf.items.length > 0 && steps === DEFAULT_STEPS) {
        const first = sf.items[0]
        setSteps(first.steps)
        setWindowSeconds(first.window_seconds)
        setBreakdownBy(first.breakdown_by)
        setCountingMode(first.counting_mode as 'users' | 'sessions')
      } else if (cat.event_names && cat.event_names.length >= 2 && steps === DEFAULT_STEPS) {
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
    if (!project) return
    const targetPid = project.id
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
    } catch (err) {
      setFunnelData(null)
      setError(err instanceof Error ? err.message : 'Failed to calculate conversion funnel')
    } finally {
      setLoading(false)
    }
  }, [project, steps, dateRange, windowSeconds, countingMode, breakdownBy])

  // Run Cohort Query
  const runCohort = useCallback(async () => {
    if (!project) return
    const targetPid = project.id
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
    } catch (err) {
      setCohortData(null)
      setError(err instanceof Error ? err.message : 'Failed to calculate cohort retention')
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
