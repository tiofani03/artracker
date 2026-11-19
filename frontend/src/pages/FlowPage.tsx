import { useEffect, useMemo, useState } from 'react'
import { flowApi, type FlowDirection, type FlowPathsResponse, type FlowScreensResponse } from '../api'
import CustomSelect from '../components/CustomSelect'
import DashboardShell from '../components/DashboardShell'
import { formatDuration } from '../flow/format'
import { layoutSankey, NODE_WIDTH, type LaidLink } from '../flow/sankey'
import {
  exitRateLabel,
  isSelectableNode,
  isTerminal,
  linkLabel,
  linkPath,
  mirrorX,
  percentLabel,
  resolveSelection,
  screenKey,
  truncate,
  type ScreenRef,
} from '../flow/view'

const DAY_MS = 24 * 60 * 60 * 1000

const RANGE_OPTIONS = [
  { value: 7, label: 'Last 7 days' },
  { value: 30, label: 'Last 30 days' },
  { value: 90, label: 'Last 90 days' },
]
const STEP_OPTIONS = [1, 2, 3, 4, 5].map((n) => ({ value: n, label: `${n} ${n === 1 ? 'step' : 'steps'}` }))

// Sankey drawing area (viewBox units). Labels sit to the right of each node, so the
// layout leaves LABEL_PAD free on the right for the last column.
const CHART_W = 900
const CHART_H = 360
const LABEL_PAD = 170
const LAYOUT_W = CHART_W - LABEL_PAD
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
    const out = layoutSankey(res.nodes, res.links, LAYOUT_W, CHART_H)
    if (res.direction !== 'prev') return out
    return { ...out, nodes: out.nodes.map((n) => ({ ...n, x: mirrorX(n.x, LAYOUT_W, NODE_WIDTH) })) }
  }, [res])
  const byId = useMemo(() => new Map(laid.nodes.map((n) => [n.id, n])), [laid])

  return (
    <div className="fl-chart" onMouseLeave={() => setHover(null)}>
      <svg
        className="fl-svg"
        viewBox={`-2 -6 ${CHART_W + 4} ${CHART_H + 12}`}
        role="img"
        aria-label={`${res.direction === 'next' ? 'Screens after' : 'Screens before'} ${res.screen}`}
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
            const label = `${truncate(n.screen, LABEL_CHARS)} · ${percentLabel(n.sessions, res.sessions)}`
            return (
              <g
                key={n.id}
                className={`fl-node${tone}${pickable ? ' is-pickable' : ''}`}
                onClick={pickable ? () => onPick(n.screen) : undefined}
                role={pickable ? 'button' : undefined}
                tabIndex={pickable ? 0 : undefined}
                aria-label={pickable ? `Show paths for ${n.screen}` : undefined}
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
                <title>{`${n.screen}: ${n.sessions} ${n.sessions === 1 ? 'session' : 'sessions'}`}</title>
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

function FlowView({ projectId }: { projectId: number }) {
  const [days, setDays] = useState(30)
  const [appVersion, setAppVersion] = useState('')
  const [search, setSearch] = useState('')
  const [picked, setPicked] = useState<ScreenRef | null>(null)
  const [direction, setDirection] = useState<FlowDirection>('next')
  const [steps, setSteps] = useState(3)

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
  const screens = useMemo(() => {
    const q = search.trim().toLowerCase()
    return q ? allScreens.filter((s) => s.screen.toLowerCase().includes(q)) : allScreens
  }, [allScreens, search])
  const unrecognized = list.data?.unrecognized_sessions ?? 0

  const selected = useMemo(() => resolveSelection(allScreens, picked), [allScreens, picked])

  const pathKey = selected && !listLoading ? `${listKey}|${screenKey(selected)}|${direction}|${steps}` : ''
  const [paths, setPaths] = useState<Tagged<FlowPathsResponse>>({ key: '', error: '', data: null })
  useEffect(() => {
    if (!selected || !pathKey) return
    let cancelled = false
    flowApi
      .paths(projectId, {
        ...range,
        app_version: appVersion,
        screen: selected.screen,
        platform: selected.platform,
        direction,
        steps,
      })
      .then((data) => !cancelled && setPaths({ key: pathKey, error: '', data }))
      .catch((e: unknown) => !cancelled && setPaths({ key: pathKey, error: e instanceof Error ? e.message : 'Request failed', data: null }))
    return () => {
      cancelled = true
    }
    // pathKey encodes every input
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathKey, projectId, range])
  const pathsLoading = !!pathKey && paths.key !== pathKey
  // Keep showing the previous chart while a new one loads.
  const res = paths.data

  const versionOptions = useMemo(() => {
    const vs = new Set(list.data?.app_versions ?? [])
    if (appVersion) vs.add(appVersion)
    return [{ value: '', label: 'All versions' }, ...[...vs].map((v) => ({ value: v, label: v }))]
  }, [list.data, appVersion])

  const pick = (screen: string) => {
    if (!selected) return
    setPicked({ screen, platform: selected.platform })
  }

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
                  <th scope="col">Screen</th>
                  <th scope="col" className="fl-num">Sessions</th>
                  <th scope="col" className="fl-num">Median</th>
                  <th scope="col" className="fl-num">Exit</th>
                </tr>
              </thead>
              <tbody>
                {screens.map((s) => {
                  const active = !!selected && screenKey(selected) === screenKey(s)
                  return (
                    <tr
                      key={screenKey(s)}
                      className={active ? 'is-active' : undefined}
                      onClick={() => setPicked({ screen: s.screen, platform: s.platform })}
                    >
                      <td className="fl-name">
                        <button
                          type="button"
                          className="fl-row-btn"
                          aria-pressed={active}
                          title={s.screen}
                        >
                          {s.screen}
                        </button>
                      </td>
                      <td className="fl-num">{s.sessions}</td>
                      <td className="fl-num">{formatDuration(s.median_ms)}</td>
                      <td className="fl-num">{exitRateLabel(s)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {unrecognized > 0 && allScreens.length > 0 && !listLoading && (
          <p className="hm-note fl-side-note">
            {unrecognized} {unrecognized === 1 ? 'session' : 'sessions'} without screen names are not shown.
          </p>
        )}
      </aside>

      <section className="dash-card fb-card hm-main">
        {noRows ? (
          <div className="hm-empty ppl-pad">
            {unrecognized > 0 ? (
              <p className="ppl-muted">
                {unrecognized} sessions have no recognizable screen names — your SDK sends <code>App</code> as the screen
                name. Send real screen names to see flows.
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
          ) : (
            <div className="hm-empty ppl-pad">
              <h2 className="dash-card__title">Pick a screen</h2>
              <p className="ppl-muted">Choose a screen on the left to see where users go next.</p>
            </div>
          )
        ) : (
          <>
            <div className="hm-toolbar">
              <h2 className="dash-card__title hm-toolbar__title" title={selected.screen}>
                {selected.screen}
              </h2>
              {pathsLoading && res && <span className="ppl-muted">Loading…</span>}
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
            </div>
            {paths.key === pathKey && paths.error ? (
              <p className="ppl-muted ppl-pad">Couldn't load paths: {paths.error}</p>
            ) : !res ? (
              <p className="ppl-muted ppl-pad">Loading…</p>
            ) : res.sessions === 0 || res.links.length === 0 ? (
              <p className="ppl-muted ppl-pad">No sessions reached this screen for these filters.</p>
            ) : (
              <div className={`fl-stage${pathsLoading ? ' is-loading' : ''}`}>
                <SankeyChart res={res} onPick={pick} />
                <p className="hm-note">
                  {res.sessions} {res.sessions === 1 ? 'session' : 'sessions'} reached {res.screen}. Hover a band for its
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
