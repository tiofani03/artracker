import { useEffect, useMemo, useState } from 'react'
import { flowApi, type FlowDirection, type FlowPathsResponse, type FlowScreen, type FlowScreensResponse } from '../api'
import CustomSelect from '../components/CustomSelect'
import DashboardShell from '../components/DashboardShell'
import {
  exitTone,
  heatmapHref,
  neighbourLabel,
  neighbours,
  rateOf,
  sortScreens,
  type Neighbour,
  type SortDir,
  type SortKey,
} from '../flow/focus'
import { formatDuration } from '../flow/format'
import { layoutSankey, NODE_WIDTH, type LaidLink } from '../flow/sankey'
import {
  exitRateLabel,
  isSelectableNode,
  isTerminal,
  labelPad,
  linkLabel,
  linkPath,
  mirrorX,
  percentLabel,
  resolveSelection,
  screenKey,
  sessionsLabel,
  truncate,
  unrecognizedLead,
  type ScreenRef,
} from '../flow/view'
import { Link } from '../router'
import { displayScreen, screenMatches } from '../screens/name'

const DAY_MS = 24 * 60 * 60 * 1000

const RANGE_OPTIONS = [
  { value: 7, label: 'Last 7 days' },
  { value: 30, label: 'Last 30 days' },
  { value: 90, label: 'Last 90 days' },
]
const STEP_OPTIONS = [1, 2, 3, 4, 5].map((n) => ({ value: n, label: `${n} ${n === 1 ? 'step' : 'steps'}` }))

// Sankey drawing area (viewBox units). Labels sit to the right of each node, so the
// layout leaves labelPad(direction) free on the right for the last column.
const CHART_W = 900
const CHART_H = 360
const LABEL_CHARS = 24

type Tagged<T> = { key: string; error: string; data: T | null }

function SankeyChart({
  res,
  onPick,
}: {
  res: FlowPathsResponse
  onPick: (screen: string) => void
}) {
  const [hover, setHover] = useState<{ link: LaidLink; x: number; y: number } | null>(null)
  const laid = useMemo(() => {
    const layoutW = CHART_W - labelPad(res.direction)
    const out = layoutSankey(res.nodes, res.links, layoutW, CHART_H)
    if (res.direction !== 'prev') return out
    return { ...out, nodes: out.nodes.map((n) => ({ ...n, x: mirrorX(n.x, layoutW, NODE_WIDTH) })) }
  }, [res])
  const byId = useMemo(() => new Map(laid.nodes.map((n) => [n.id, n])), [laid])

  return (
    <div className="fl-chart" onMouseLeave={() => setHover(null)}>
      <svg
        className="fl-svg"
        viewBox={`-2 -6 ${CHART_W + 4} ${CHART_H + 12}`}
        role="group"
        aria-label={`${res.direction === 'next' ? 'Screens after' : 'Screens before'} ${displayScreen(res.screen)}`}
      >
        <g>
          {laid.links.map((l) => {
            const s = byId.get(l.source)!
            const t = byId.get(l.target)!
            const dropOff = isTerminal(s.screen) || isTerminal(t.screen)
            const active = hover?.link === l
            return (
              <path
                key={`${l.source}>${l.target}`}
                className={`fl-link${dropOff ? ' fl-link--drop' : ''}${active ? ' is-active' : ''}`}
                d={linkPath(s.x, l.sy, t.x, l.ty, NODE_WIDTH)}
                strokeWidth={Math.max(1, l.w)}
                onMouseMove={(e) => {
                  const box = e.currentTarget.ownerSVGElement?.parentElement?.getBoundingClientRect()
                  if (box) setHover({ link: l, x: e.clientX - box.left, y: e.clientY - box.top })
                }}
                onMouseLeave={() => setHover(null)}
              />
            )
          })}
        </g>
        <g>
          {laid.nodes.map((n) => {
            const pickable = isSelectableNode(n.screen) && n.step !== 0
            const tone = isTerminal(n.screen) ? ' fl-node--drop' : n.screen === 'Other' ? ' fl-node--other' : ''
            const label = `${truncate(displayScreen(n.screen), LABEL_CHARS)} · ${percentLabel(n.sessions, res.sessions)}`
            return (
              <g
                key={n.id}
                className={`fl-node${tone}${pickable ? ' is-pickable' : ''}`}
                onClick={pickable ? () => onPick(n.screen) : undefined}
                role={pickable ? 'button' : undefined}
                tabIndex={pickable ? 0 : undefined}
                aria-label={pickable ? `Show paths for ${displayScreen(n.screen)}` : undefined}
                onKeyDown={
                  pickable
                    ? (e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          onPick(n.screen)
                        }
                      }
                    : undefined
                }
              >
                <title>{`${displayScreen(n.screen)}: ${n.sessions} ${n.sessions === 1 ? 'session' : 'sessions'}`}</title>
                <rect x={n.x} y={n.y} width={NODE_WIDTH} height={Math.max(1, n.h)} rx={2} />
                <text className="fl-label" x={n.x + NODE_WIDTH + 6} y={n.y + Math.max(1, n.h) / 2} dominantBaseline="middle">
                  {label}
                </text>
              </g>
            )
          })}
        </g>
      </svg>
      {hover && (
        <div className="hm-tip" style={{ left: hover.x, top: hover.y }}>
          {linkLabel(hover.link.sessions, res.sessions)}
        </div>
      )}
    </div>
  )
}

type PathsState = { data: FlowPathsResponse | null; error: string; loading: boolean }

/** Loads one /flow/paths result; key encodes every input, '' disables the request. */
function usePaths(
  key: string,
  load: () => Promise<FlowPathsResponse>,
): PathsState {
  const [state, setState] = useState<Tagged<FlowPathsResponse>>({ key: '', error: '', data: null })
  useEffect(() => {
    if (!key) return
    let cancelled = false
    load()
      .then((data) => !cancelled && setState({ key, error: '', data }))
      .catch((e: unknown) => !cancelled && setState({ key, error: e instanceof Error ? e.message : 'Request failed', data: null }))
    return () => {
      cancelled = true
    }
    // key encodes every input of load
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])
  return {
    // Keep the previous result while a new one loads.
    data: state.data,
    error: state.key === key ? state.error : '',
    loading: !!key && state.key !== key,
  }
}

function NeighbourList({
  title,
  items,
  state,
  onPick,
}: {
  title: string
  items: Neighbour[]
  state: PathsState
  onPick: (screen: string) => void
}) {
  return (
    <div className="fl-nb">
      <h3 className="fl-nb__title">{title}</h3>
      {state.error ? (
        <p className="ppl-muted">Couldn't load: {state.error}</p>
      ) : !state.data ? (
        <p className="ppl-muted">Loading…</p>
      ) : items.length === 0 ? (
        <p className="ppl-muted">No data for these filters.</p>
      ) : (
        <ul className="fl-nb__list">
          {items.map((n) => {
            const pickable = isSelectableNode(n.screen)
            const pct = percentLabel(n.sessions, state.data!.sessions)
            const body = (
              <>
                <span className="fl-nb__row">
                  <span className="fl-nb__name" title={neighbourLabel(n.screen)}>
                    {neighbourLabel(n.screen)}
                  </span>
                  <span className="fl-nb__pct">{pct}</span>
                </span>
                <span className="fl-bar" aria-hidden="true">
                  <span style={{ width: `${Math.max(2, n.share * 100)}%` }} />
                </span>
                <span className="fl-nb__sub">{sessionsLabel(n.sessions)}</span>
              </>
            )
            const tone = isTerminal(n.screen) ? ' fl-nb__item--drop' : n.screen === 'Other' ? ' fl-nb__item--other' : ''
            return (
              <li key={n.screen} className={`fl-nb__item${tone}`}>
                {pickable ? (
                  <button type="button" className="fl-nb__btn" onClick={() => onPick(n.screen)} aria-label={`Focus ${neighbourLabel(n.screen)}, ${pct}`}>
                    {body}
                  </button>
                ) : (
                  <div className="fl-nb__btn is-static">{body}</div>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

function FocusView({
  stats,
  prev,
  next,
  onPick,
}: {
  stats: FlowScreen | undefined
  prev: PathsState
  next: PathsState
  onPick: (screen: string) => void
}) {
  const exitRate = stats ? rateOf(stats.exit_sessions, stats.sessions) : 0
  const loading = prev.loading || next.loading
  return (
    <div className={`fl-focus${loading ? ' is-loading' : ''}`}>
      <NeighbourList title="Came from" items={neighbours(prev.data)} state={prev} onPick={onPick} />
      <div className="fl-focus__card" aria-live="polite">
        {stats ? (
          <>
            <p className="fl-focus__name" title={displayScreen(stats.screen)}>
              {displayScreen(stats.screen)}
            </p>
            <dl className="fl-stats">
              <div>
                <dt>Sessions</dt>
                <dd>{stats.sessions}</dd>
              </div>
              <div>
                <dt>Median time</dt>
                <dd>{formatDuration(stats.median_ms)}</dd>
              </div>
              <div>
                <dt>p75 time</dt>
                <dd>{formatDuration(stats.p75_ms)}</dd>
              </div>
              <div>
                <dt>Sessions ended here</dt>
                <dd className={`fl-tone--${exitTone(exitRate)}`}>{exitRateLabel(stats)}</dd>
              </div>
              <div>
                <dt>Sessions started here</dt>
                <dd>{percentLabel(stats.entry_sessions, stats.sessions)}</dd>
              </div>
            </dl>
            <Link className="fl-focus__link" to={heatmapHref(stats.screen)}>
              Open heatmap →
            </Link>
          </>
        ) : (
          <p className="ppl-muted">Loading…</p>
        )}
      </div>
      <NeighbourList title="Went to" items={neighbours(next.data)} state={next} onPick={onPick} />
    </div>
  )
}

const SORT_LABELS: Record<SortKey, string> = { screen: 'Screen', sessions: 'Sessions', median: 'Median', exit: 'Exit' }

function SortHeader({
  k,
  sort,
  onSort,
  numeric,
}: {
  k: SortKey
  sort: { key: SortKey; dir: SortDir }
  onSort: (k: SortKey) => void
  numeric?: boolean
}) {
  const active = sort.key === k
  return (
    <th
      scope="col"
      className={numeric ? 'fl-num' : undefined}
      aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
    >
      <button type="button" className={`fl-sort${active ? ' is-active' : ''}`} onClick={() => onSort(k)}>
        {SORT_LABELS[k]}
        <span aria-hidden="true">{active ? (sort.dir === 'asc' ? ' ▲' : ' ▼') : ''}</span>
      </button>
    </th>
  )
}

type ViewMode = 'focus' | 'paths'

function FlowView({ projectId }: { projectId: number }) {
  const [days, setDays] = useState(30)
  const [appVersion, setAppVersion] = useState('')
  const [search, setSearch] = useState('')
  const [picked, setPicked] = useState<ScreenRef | null>(null)
  const [mode, setMode] = useState<ViewMode>('focus')
  const [direction, setDirection] = useState<FlowDirection>('next')
  const [steps, setSteps] = useState(3)
  const [sort, setSort] = useState<{ key: SortKey; dir: SortDir }>({ key: 'sessions', dir: 'desc' })

  // The window is anchored when the range changes, so requests stay stable between renders.
  const range = useMemo(() => {
    const now = Date.now()
    return { from: new Date(now - days * DAY_MS).toISOString(), to: new Date(now).toISOString() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days, projectId])

  const listKey = `${projectId}|${days}|${appVersion}`
  const [list, setList] = useState<Tagged<FlowScreensResponse>>({ key: '', error: '', data: null })
  useEffect(() => {
    let cancelled = false
    flowApi
      .screens(projectId, { ...range, app_version: appVersion })
      .then((data) => !cancelled && setList({ key: listKey, error: '', data }))
      .catch((e: unknown) => !cancelled && setList({ key: listKey, error: e instanceof Error ? e.message : 'Request failed', data: null }))
    return () => {
      cancelled = true
    }
  }, [projectId, range, appVersion, listKey])
  const listLoading = list.key !== listKey

  const allScreens = useMemo(() => list.data?.screens ?? [], [list.data])
  const screens = useMemo(
    () => sortScreens(allScreens.filter((s) => screenMatches(s.screen, search)), sort.key, sort.dir),
    [allScreens, search, sort],
  )
  const maxMedian = useMemo(() => Math.max(1, ...allScreens.map((s) => s.median_ms)), [allScreens])
  const unrecognized = list.data?.unrecognized_sessions ?? 0

  const selected = useMemo(() => resolveSelection(allScreens, picked), [allScreens, picked])
  const stats = useMemo(
    () => (selected ? allScreens.find((s) => screenKey(s) === screenKey(selected)) : undefined),
    [allScreens, selected],
  )

  const baseKey = selected && !listLoading ? `${listKey}|${screenKey(selected)}` : ''
  const load = (dir: FlowDirection, n: number) => () =>
    flowApi.paths(projectId, { ...range, app_version: appVersion, screen: selected!.screen, platform: selected!.platform, direction: dir, steps: n })
  const prev = usePaths(baseKey && mode === 'focus' ? `${baseKey}|prev|1` : '', load('prev', 1))
  const next = usePaths(baseKey && mode === 'focus' ? `${baseKey}|next|1` : '', load('next', 1))
  const explore = usePaths(baseKey && mode === 'paths' ? `${baseKey}|${direction}|${steps}` : '', load(direction, steps))
  const res = explore.data

  const versionOptions = useMemo(() => {
    const vs = new Set(list.data?.app_versions ?? [])
    if (appVersion) vs.add(appVersion)
    return [{ value: '', label: 'All versions' }, ...[...vs].map((v) => ({ value: v, label: v }))]
  }, [list.data, appVersion])

  const pick = (screen: string) => {
    if (!selected) return
    setPicked({ screen, platform: selected.platform })
  }
  const onSort = (key: SortKey) =>
    setSort((cur) => (cur.key === key ? { key, dir: cur.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: key === 'screen' ? 'asc' : 'desc' }))

  const noRows = !listLoading && !list.error && allScreens.length === 0

  return (
    <div className="hm-layout fl-layout">
      <aside className="dash-card fb-card hm-side">
        <input
          type="search"
          className="dash-input hm-search"
          placeholder="Search screens"
          aria-label="Search screens"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <div className="hm-filters">
          <CustomSelect size="sm" ariaLabel="Date range" options={RANGE_OPTIONS} value={days} onChange={setDays} />
          <CustomSelect size="sm" ariaLabel="App version" options={versionOptions} value={appVersion} onChange={setAppVersion} />
        </div>
        {list.error && !listLoading ? (
          <p className="ppl-muted ppl-pad">Couldn't load screens: {list.error}</p>
        ) : listLoading ? (
          <p className="ppl-muted ppl-pad">Loading…</p>
        ) : noRows ? (
          <p className="ppl-muted ppl-pad">No screens yet.</p>
        ) : screens.length === 0 ? (
          <p className="ppl-muted ppl-pad">No screens match.</p>
        ) : (
          <div className="fl-table-wrap">
            <table className="fl-table" aria-label="Screens">
              <thead>
                <tr>
                  <SortHeader k="screen" sort={sort} onSort={onSort} />
                  <SortHeader k="sessions" sort={sort} onSort={onSort} numeric />
                  <SortHeader k="median" sort={sort} onSort={onSort} numeric />
                  <SortHeader k="exit" sort={sort} onSort={onSort} numeric />
                  <th scope="col" className="fl-hm-col">
                    <span className="sr-only">Heatmap</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {screens.map((s) => {
                  const active = !!selected && screenKey(selected) === screenKey(s)
                  const exitRate = rateOf(s.exit_sessions, s.sessions)
                  return (
                    <tr
                      key={screenKey(s)}
                      className={active ? 'is-active' : undefined}
                      onClick={() => setPicked({ screen: s.screen, platform: s.platform })}
                    >
                      <td className="fl-name">
                        <button type="button" className="fl-row-btn" aria-pressed={active} title={displayScreen(s.screen)}>
                          {displayScreen(s.screen)}
                        </button>
                      </td>
                      <td className="fl-num">{s.sessions}</td>
                      <td className="fl-num">
                        {formatDuration(s.median_ms)}
                        <span className="fl-bar fl-bar--cell" aria-hidden="true">
                          <span style={{ width: `${Math.max(2, (s.median_ms / maxMedian) * 100)}%` }} />
                        </span>
                      </td>
                      <td className={`fl-num fl-tone--${exitTone(exitRate)}`}>{exitRateLabel(s)}</td>
                      <td className="fl-hm-col">
                        <Link
                          className="fl-hm-link"
                          to={heatmapHref(s.screen)}
                          onClick={(e) => e.stopPropagation()}
                          aria-label={`Open heatmap for ${displayScreen(s.screen)}`}
                          title="Open heatmap"
                        >
                          <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                            <path d="M4 4h6v6H4V4Zm10 0h6v6h-6V4ZM4 14h6v6H4v-6Zm10 0h6v6h-6v-6Z" fill="none" stroke="currentColor" strokeWidth="1.8" />
                          </svg>
                        </Link>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {unrecognized > 0 && allScreens.length > 0 && !listLoading && (
          <p className="hm-note fl-side-note">
            {sessionsLabel(unrecognized)} without screen names {unrecognized === 1 ? 'is' : 'are'} not shown.
          </p>
        )}
      </aside>

      <section className="dash-card fb-card hm-main">
        {noRows ? (
          <div className="hm-empty ppl-pad">
            {unrecognized > 0 ? (
              <p className="ppl-muted">
                {unrecognizedLead(unrecognized)} no recognizable screen names or replay data. If your SDK sends{' '}
                <code>App</code> as the screen name, send real screen names to see flows.
              </p>
            ) : (
              <p className="ppl-muted">
                No screen flow yet. User Flow is built from screenshot session replay and appears about 10 minutes after a
                session ends.
              </p>
            )}
          </div>
        ) : !selected ? (
          listLoading ? (
            <p className="ppl-muted ppl-pad">Loading…</p>
          ) : list.error ? null : (
            <div className="hm-empty ppl-pad">
              <h2 className="dash-card__title">Pick a screen</h2>
              <p className="ppl-muted">Choose a screen on the left to see where users go next.</p>
            </div>
          )
        ) : (
          <>
            <div className="hm-toolbar">
              <h2 className="dash-card__title hm-toolbar__title" title={displayScreen(selected.screen)}>
                {displayScreen(selected.screen)}
              </h2>
              <div className="dash-seg" role="group" aria-label="View">
                <button
                  type="button"
                  className={`dash-seg__btn${mode === 'focus' ? ' is-active' : ''}`}
                  aria-pressed={mode === 'focus'}
                  onClick={() => setMode('focus')}
                >
                  Focus
                </button>
                <button
                  type="button"
                  className={`dash-seg__btn${mode === 'paths' ? ' is-active' : ''}`}
                  aria-pressed={mode === 'paths'}
                  onClick={() => setMode('paths')}
                >
                  Explore paths
                </button>
              </div>
              {mode === 'paths' && (
                <>
                  {explore.loading && res && <span className="ppl-muted">Loading…</span>}
                  <div className="dash-seg" role="group" aria-label="Direction">
                    <button
                      type="button"
                      className={`dash-seg__btn${direction === 'next' ? ' is-active' : ''}`}
                      aria-pressed={direction === 'next'}
                      onClick={() => setDirection('next')}
                    >
                      Next ▸
                    </button>
                    <button
                      type="button"
                      className={`dash-seg__btn${direction === 'prev' ? ' is-active' : ''}`}
                      aria-pressed={direction === 'prev'}
                      onClick={() => setDirection('prev')}
                    >
                      ◂ Previous
                    </button>
                  </div>
                  <CustomSelect size="sm" ariaLabel="Steps" options={STEP_OPTIONS} value={steps} onChange={setSteps} />
                </>
              )}
            </div>
            {mode === 'focus' ? (
              <>
                <FocusView stats={stats} prev={prev} next={next} onPick={pick} />
                <p className="hm-note">
                  Shares are of the sessions that opened this screen (first visit). Click a screen to focus it. Updated about 10
                  minutes after a session ends.
                </p>
              </>
            ) : explore.error ? (
              <p className="ppl-muted ppl-pad">Couldn't load paths: {explore.error}</p>
            ) : !res ? (
              <p className="ppl-muted ppl-pad">Loading…</p>
            ) : res.sessions === 0 || res.links.length === 0 ? (
              <p className="ppl-muted ppl-pad">No sessions reached this screen for these filters.</p>
            ) : (
              <div className={`fl-stage${explore.loading ? ' is-loading' : ''}`}>
                <SankeyChart res={res} onPick={pick} />
                <p className="hm-note">
                  {res.sessions} {res.sessions === 1 ? 'session' : 'sessions'} reached {displayScreen(res.screen)}. Hover a band for its
                  share; click a screen to follow its paths. Updated about 10 minutes after a session ends.
                </p>
              </div>
            )}
          </>
        )}
      </section>
    </div>
  )
}

export default function FlowPage() {
  return (
    <DashboardShell sectionTitle="User Flow">
      {({ project }) => (
        <>
          <div className="fb-page-header">
            <div className="fb-page-header__left">
              <div className="fb-title-wrap">
                <h1 className="fb-page-title">User Flow</h1>
                <p className="fb-page-desc">
                  How users move between screens of <strong>{project?.name || 'your app'}</strong>, and how long they stay.
                </p>
              </div>
            </div>
          </div>
          {!project ? (
            <div className="dash-empty dash-card fb-card ppl-pad">Select a project from the sidebar to see its user flow.</div>
          ) : (
            <FlowView key={project.id} projectId={project.id} />
          )}
        </>
      )}
    </DashboardShell>
  )
}
