import { useEffect, useMemo, useState } from 'react'
import {
  appStartApi,
  type AppStartFunctionsResponse,
  type AppStartRow,
  type AppStartSort,
  type AppStartSummary,
  type AppStartType,
  type Platform,
  type RamTier,
} from '../api'
import { formatMs } from '../appstart/format'
import { slowPreInitPhase } from '../appstart/hints'
import CustomSelect from '../components/CustomSelect'
import DashboardShell from '../components/DashboardShell'
import PhaseBar from '../components/appstart/PhaseBar'
import { PreInitHint, ProfilingSetup } from '../components/appstart/ProfilingHints'
import TypeOverview from '../components/appstart/TypeOverview'
import TrendChart from '../components/appstart/TrendChart'
import { Link, navigate } from '../router'

const DAY_MS = 24 * 60 * 60 * 1000
const PAGE_SIZE = 20
// Sent as environment=all: every environment, instead of the backend's production default.
const ALL_ENVIRONMENTS = 'all'

const RANGE_OPTIONS = [
  { value: 7, label: 'Last 7 days' },
  { value: 30, label: 'Last 30 days' },
  { value: 90, label: 'Last 90 days' },
]
const TYPE_LABEL: Record<AppStartType, string> = { cold: 'Cold', warm: 'Warm', hot: 'Hot' }
const SORT_OPTIONS: { value: AppStartSort; label: string }[] = [
  { value: 'slowest', label: 'Slowest first' },
  { value: 'recent', label: 'Most recent' },
]
// The starts list can widen the page's start type to every type; the charts above stay on one type.
type ListScope = 'page' | 'all'
const RAM_OPTIONS:{ value: RamTier | ''; label: string }[] = [
  { value: '', label: 'All devices' },
  { value: 'low', label: 'Low RAM (< 4 GB)' },
  { value: 'mid', label: 'Mid RAM (4–8 GB)' },
  { value: 'high', label: 'High RAM (> 8 GB)' },
]

const startHref = (id: string) => `/dashboard/app-start/${encodeURIComponent(id)}`
const errMsg = (e: unknown) => (e instanceof Error ? e.message : 'Request failed')

type Tagged<T> = { key: string; error: string; data: T | null }
const EMPTY = { key: '', error: '', data: null }

function Kpi({ label, value }: { label: string; value: string }) {
  return (
    <div className="dash-card as-kpi">
      <span className="as-kpi__label">{label}</span>
      <span className="as-kpi__value">{value}</span>
    </div>
  )
}

function ByVersion({ rows }: { rows: AppStartSummary['by_version'] }) {
  const max = Math.max(...rows.map((r) => r.p90), 1)
  if (rows.length === 0) return <p className="ppl-muted">No versions yet.</p>
  return (
    <ul className="as-versions">
      {rows.map((r) => (
        <li key={r.version} className="as-versions__row">
          <span className="as-versions__name" title={r.version}>
            {r.version}
          </span>
          <span className="as-versions__track" title={`p50 ${formatMs(r.p50)} · p90 ${formatMs(r.p90)} · ${r.count} starts`}>
            <span className="as-versions__p90" style={{ width: `${(r.p90 / max) * 100}%` }} />
            <span className="as-versions__p50" style={{ width: `${(r.p50 / max) * 100}%` }} />
          </span>
          <span className="as-versions__ms">{formatMs(r.p50)}</span>
        </li>
      ))}
    </ul>
  )
}

function FunctionsTable({
  res,
  appOnly,
  onAppOnly,
  platform,
}: {
  res: AppStartFunctionsResponse | null
  appOnly: boolean
  onAppOnly: (v: boolean) => void
  platform: Platform
}) {
  return (
    <section className="dash-card">
      <div className="dash-card__head">
        <div>
          <h2 className="dash-card__title">Slowest functions during startup</h2>
          <p className="dash-card__desc">From profiled cold starts. Click a function to open an example start.</p>
        </div>
        <label className="hm-toggle">
          <input type="checkbox" checked={!appOnly} onChange={(e) => onAppOnly(!e.target.checked)} />
          Include libraries
        </label>
      </div>
      {res && res.profiles === 0 ? (
        <ProfilingSetup platform={platform} />
      ) : !res || res.functions.length === 0 ? (
        <p className="ppl-muted">{res ? 'No profiled starts match these filters.' : 'Loading…'}</p>
      ) : (
        <div className="dash-table-wrap">
          <table className="dash-table as-table">
            <thead>
              <tr>
                <th>Function</th>
                <th className="as-num">% of starts</th>
                <th className="as-num">{appOnly ? 'Median attributed' : 'Median self'}</th>
                <th className="as-num">Median total</th>
                <th className="as-num">p90 total</th>
              </tr>
            </thead>
            <tbody>
              {res.functions.map((f) => (
                <tr key={f.function} className="as-row" onClick={() => navigate(startHref(f.example_start_id))}>
                  <td className="as-fn">
                    <span>
                      <Link to={startHref(f.example_start_id)} onClick={(e) => e.stopPropagation()} title={f.function}>
                        {f.function}
                      </Link>
                      {!f.is_app && <span className="as-tag">library</span>}
                    </span>
                  </td>
                  <td className="as-num" data-label="% of starts">{Math.round(f.starts_pct)}%</td>
                  <td className="as-num" data-label={appOnly ? 'Median attributed' : 'Median self'}>
                    {formatMs(appOnly ? f.median_attributed_ms : f.median_self_ms)}
                  </td>
                  <td className="as-num" data-label="Median total">{formatMs(f.median_total_ms)}</td>
                  <td className="as-num" data-label="p90 total">{formatMs(f.p90_total_ms)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function StartsTable({
  rows,
  loading,
  hasMore,
  onMore,
  pageType,
  scope,
  onScope,
  sort,
  onSort,
}: {
  rows: AppStartRow[] | null
  loading: boolean
  hasMore: boolean
  onMore: () => void
  pageType: AppStartType
  scope: ListScope
  onScope: (v: ListScope) => void
  sort: AppStartSort
  onSort: (v: AppStartSort) => void
}) {
  const allTypes = scope === 'all'
  const scopeOptions: { value: ListScope; label: string }[] = [
    { value: 'page', label: `${TYPE_LABEL[pageType]} starts only` },
    { value: 'all', label: 'All start types' },
  ]
  return (
    <section className="dash-card">
      <div className="dash-card__head">
        <div>
          <h2 className="dash-card__title">{sort === 'slowest' ? 'Slowest starts' : 'Recent starts'}</h2>
          <p className="dash-card__desc">
            Individual {allTypes ? 'cold, warm and hot' : pageType} starts, {sort === 'slowest' ? 'slowest' : 'newest'} first.
          </p>
        </div>
        <div className="as-list-controls">
          <CustomSelect size="sm" ariaLabel="Start types in list" options={scopeOptions} value={scope} onChange={onScope} />
          <CustomSelect size="sm" ariaLabel="Sort starts" options={SORT_OPTIONS} value={sort} onChange={onSort} />
        </div>
      </div>
      {!rows || rows.length === 0 ? (
        <p className="ppl-muted">{rows ? 'No starts match these filters.' : 'Loading…'}</p>
      ) : (
        <div className="dash-table-wrap">
          <table className="dash-table as-table">
            <thead>
              <tr>
                <th className="as-num">Duration</th>
                {allTypes && <th>Type</th>}
                <th>Version</th>
                <th>Device</th>
                <th>First screen</th>
                <th>Profile</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.start_id} className="as-row" onClick={() => navigate(startHref(r.start_id))}>
                  <td className="as-num" data-label="Duration">
                    <Link to={startHref(r.start_id)} onClick={(e) => e.stopPropagation()}>
                      {formatMs(r.duration_ms)}
                    </Link>
                  </td>
                  {allTypes && (
                    <td data-label="Type">
                      <span className={`as-type as-type--${r.type}`}>{TYPE_LABEL[r.type] ?? r.type}</span>
                    </td>
                  )}
                  <td data-label="Version">{r.app_version}</td>
                  <td data-label="Device">
                    <span>
                      {r.device_model} <span className="ppl-muted">· {r.os_version}</span>
                    </span>
                  </td>
                  <td data-label="First screen">{r.first_screen || '—'}</td>
                  <td data-label="Profile">{r.profiled ? 'Profiled' : <span className="ppl-muted">No</span>}</td>
                  <td className="ppl-muted" data-label="When">
                    {new Date(r.started_at).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {hasMore && (
        <div className="as-more">
          <button type="button" className="dash-btn" disabled={loading} onClick={onMore}>
            {loading ? 'Loading…' : 'Load more'}
          </button>
        </div>
      )}
    </section>
  )
}

function AppStartView({ projectId, platform }: { projectId: number; platform: Platform }) {
  const [days, setDays] = useState(30)
  // '' = the backend's effective environment (production when it has data, else all).
  const [environment, setEnvironment] = useState('')
  const [type, setType] = useState<AppStartType>('cold')
  const [appVersion, setAppVersion] = useState('')
  const [ramTier, setRamTier] = useState<RamTier | ''>('')
  const [appOnly, setAppOnly] = useState(true)

  // The window is anchored when the range changes, so requests stay stable between renders.
  const range = useMemo(() => {
    const now = Date.now()
    return { from: new Date(now - days * DAY_MS).toISOString(), to: new Date(now).toISOString() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days, projectId])

  const filterKey = `${projectId}|${days}|${environment}|${type}|${appVersion}|${ramTier}`
  const params = useMemo(
    () => ({ ...range, environment, type, app_version: appVersion, ram_tier: ramTier }),
    [range, environment, type, appVersion, ramTier],
  )

  const [summary, setSummary] = useState<Tagged<AppStartSummary>>(EMPTY)
  useEffect(() => {
    let cancelled = false
    appStartApi
      .summary(projectId, params)
      .then((data) => !cancelled && setSummary({ key: filterKey, error: '', data }))
      .catch((e: unknown) => !cancelled && setSummary({ key: filterKey, error: errMsg(e), data: null }))
    return () => {
      cancelled = true
    }
  }, [projectId, params, filterKey])

  const fnKey = `${filterKey}|${appOnly}`
  const [fns, setFns] = useState<Tagged<AppStartFunctionsResponse>>(EMPTY)
  useEffect(() => {
    let cancelled = false
    appStartApi
      .functions(projectId, { ...params, app_only: appOnly })
      .then((data) => !cancelled && setFns({ key: fnKey, error: '', data }))
      .catch((e: unknown) => !cancelled && setFns({ key: fnKey, error: errMsg(e), data: null }))
    return () => {
      cancelled = true
    }
  }, [projectId, params, appOnly, fnKey])

  const [listScope, setListScope] = useState<ListScope>('page')
  const [listSort, setListSort] = useState<AppStartSort>('slowest')
  const listKey = `${filterKey}|${listScope}|${listSort}`
  const listParams = useMemo(
    () => ({ ...params, type: listScope === 'all' ? ('all' as const) : params.type, sort: listSort, limit: PAGE_SIZE }),
    [params, listScope, listSort],
  )
  const [list, setList] = useState<Tagged<{ rows: AppStartRow[]; next: string }>>(EMPTY)
  const [loadingMore, setLoadingMore] = useState(false)
  useEffect(() => {
    let cancelled = false
    appStartApi
      .list(projectId, listParams)
      .then((r) => !cancelled && setList({ key: listKey, error: '', data: { rows: r.starts, next: r.next_cursor ?? '' } }))
      .catch((e: unknown) => !cancelled && setList({ key: listKey, error: errMsg(e), data: null }))
    return () => {
      cancelled = true
    }
  }, [projectId, listParams, listKey])

  const loadMore = () => {
    const cur = list.data
    if (!cur?.next || loadingMore) return
    setLoadingMore(true)
    const key = listKey
    appStartApi
      .list(projectId, { ...listParams, cursor: cur.next })
      .then((r) =>
        setList((prev) =>
          prev.key === key && prev.data
            ? { key, error: '', data: { rows: [...prev.data.rows, ...r.starts], next: r.next_cursor ?? '' } }
            : prev,
        ),
      )
      .catch((e: unknown) => setList((prev) => (prev.key === key ? { ...prev, error: errMsg(e) } : prev)))
      .finally(() => setLoadingMore(false))
  }

  // Keep the previous result on screen while a new filter loads.
  const s = summary.data
  const fresh = summary.key === filterKey
  const error = (fresh && summary.error) || (fns.key === fnKey && fns.error) || (list.key === listKey && list.error) || ''

  const envOptions = useMemo(() => {
    const names = new Set(s?.environments ?? [])
    if (environment && environment !== ALL_ENVIRONMENTS) names.add(environment)
    if (s?.environment) names.add(s.environment)
    if (names.size === 0) return []
    return [
      { value: ALL_ENVIRONMENTS, label: 'All environments' },
      ...[...names].map((n) => ({ value: n, label: n })),
    ]
  }, [s, environment])
  const versionOptions = useMemo(() => {
    const vs = new Set(s?.versions ?? [])
    if (appVersion) vs.add(appVersion)
    return [{ value: '', label: 'All versions' }, ...[...vs].map((v) => ({ value: v, label: v }))]
  }, [s, appVersion])

  const filters = (
    <div className="as-toolbar">
      {envOptions.length > 0 && (
        <CustomSelect
          size="sm"
          ariaLabel="Environment"
          options={envOptions}
          value={environment || s?.environment || ALL_ENVIRONMENTS}
          onChange={setEnvironment}
        />
      )}
      <CustomSelect size="sm" ariaLabel="App version" options={versionOptions} value={appVersion} onChange={setAppVersion} />
      <CustomSelect size="sm" ariaLabel="RAM tier" options={RAM_OPTIONS} value={ramTier} onChange={setRamTier} />
      <CustomSelect size="sm" ariaLabel="Date range" options={RANGE_OPTIONS} value={days} onChange={setDays} />
    </div>
  )

  if (!s) {
    return (
      <>
        {filters}
        <p className="ppl-muted ppl-pad">{error ? `Couldn't load app starts: ${error}` : 'Loading…'}</p>
      </>
    )
  }

  const overview = <TypeOverview stats={s.by_type ?? []} selected={type} onSelect={setType} />
  const anyStarts = (s.by_type ?? []).some((t) => t.count > 0)

  if (s.count === 0 && fresh && anyStarts) {
    return (
      <>
        {filters}
        {overview}
        <section className="dash-card ppl-pad">
          <h2 className="dash-card__title">No {TYPE_LABEL[type].toLowerCase()} starts</h2>
          <p className="ppl-muted">
            No {TYPE_LABEL[type].toLowerCase()} starts match these filters. Pick another start type above to see its details.
          </p>
        </section>
      </>
    )
  }

  if (s.count === 0 && fresh) {
    const filtered = type !== 'cold' || appVersion !== '' || ramTier !== '' || (environment !== '' && environment !== ALL_ENVIRONMENTS)
    return (
      <>
        {filters}
        <section className="dash-card fb-card ppl-pad">
          <h2 className="dash-card__title">{filtered ? 'No matching app starts' : 'No app starts yet'}</h2>
          <p className="ppl-muted">
            {filtered
              ? 'No app starts match these filters.'
              : 'No app starts yet. Update to the latest Artracker SDK; enable startup profiling to see slow functions and flamegraphs.'}
          </p>
        </section>
      </>
    )
  }

  // Only cold starts have a phase before SDK init worth acting on.
  const preInit = type === 'cold' ? slowPreInitPhase(s.phases) : null

  return (
    <div className={`as-page${fresh ? '' : ' is-loading'}`}>
      {filters}
      {error && <p className="hm-note hm-note--warn">Couldn't load all sections: {error}</p>}
      {overview}
      <h2 className="as-type-heading">{TYPE_LABEL[type]} starts</h2>
      <div className="as-kpis">
        <Kpi label="Median (p50)" value={s.count ? formatMs(s.p50) : '—'} />
        <Kpi label="p90" value={s.count ? formatMs(s.p90) : '—'} />
        <Kpi label="Starts" value={s.count.toLocaleString()} />
        <Kpi label="Profiled" value={s.count ? `${Math.round(s.profiled_pct)}%` : '—'} />
      </div>
      <div className="as-split">
        <section className="dash-card">
          <h2 className="dash-card__title">Start time trend</h2>
          <TrendChart daily={s.daily} />
        </section>
        <section className="dash-card">
          <h2 className="dash-card__title">By version</h2>
          <p className="dash-card__desc">Median start time, p90 shaded behind.</p>
          <ByVersion rows={s.by_version} />
        </section>
      </div>
      <section className="dash-card">
        <h2 className="dash-card__title">Where the time goes</h2>
        <p className="dash-card__desc">Median per phase.</p>
        <PhaseBar phases={s.phases.map((p) => ({ name: p.name, ms: p.median_ms }))} total={s.p50} />
        {preInit && <PreInitHint phase={preInit} platform={platform} />}
      </section>
      <FunctionsTable res={fns.data} appOnly={appOnly} onAppOnly={setAppOnly} platform={platform} />
      <StartsTable
        rows={list.data?.rows ?? null}
        loading={loadingMore}
        hasMore={!!list.data?.next && list.key === listKey}
        onMore={loadMore}
        pageType={type}
        scope={listScope}
        onScope={setListScope}
        sort={listSort}
        onSort={setListSort}
      />
    </div>
  )
}

export default function AppStartPage() {
  return (
    <DashboardShell sectionTitle="App Start">
      {({ project }) => (
        <>
          <div className="fb-page-header">
            <div className="fb-page-header__left">
              <div className="fb-title-wrap">
                <h1 className="fb-page-title">App Start</h1>
                <p className="fb-page-desc">
                  How long <strong>{project?.name || 'your app'}</strong> takes to start, and which functions slow it down.
                </p>
              </div>
            </div>
          </div>
          {!project ? (
            <div className="dash-empty dash-card fb-card ppl-pad">Select a project from the sidebar to see its app starts.</div>
          ) : project.platform === 'web' ? (
            <section className="dash-card fb-card ppl-pad">
              <h2 className="dash-card__title">App Start</h2>
              <p className="ppl-muted">App Start is available for Android and iOS projects.</p>
            </section>
          ) : (
            <AppStartView key={project.id} projectId={project.id} platform={project.platform} />
          )}
        </>
      )}
    </DashboardShell>
  )
}
