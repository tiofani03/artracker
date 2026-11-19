import { useEffect, useMemo, useState } from 'react'
import { heatmapApi, type HeatmapCell, type HeatmapOrientation, type HeatmapResponse, type HeatmapScreen, type HeatmapScreensResponse } from '../api'
import CustomSelect from '../components/CustomSelect'
import DashboardShell from '../components/DashboardShell'
import HeatCanvas from '../components/heatmap/HeatCanvas'
import { cellShare, MIN_RELIABLE_SESSIONS, shareLabel } from '../heatmap/grid'
import { fetchChunkPayloads } from '../replay/parseChunk'
import { screenFrameData } from '../replay/screen'
import { displayScreen, screenMatches } from '../screens/name'

const SEARCH_ALL = ''
const DAY_MS = 24 * 60 * 60 * 1000

const RANGE_OPTIONS = [
  { value: 7, label: 'Last 7 days' },
  { value: 30, label: 'Last 30 days' },
  { value: 90, label: 'Last 90 days' },
]
// Only portrait heatmaps are shown; landscape taps stay stored but are not selectable.
const orientation: HeatmapOrientation = 'portrait'

function screenKey(s: HeatmapScreen) {
  return `${s.platform}|${s.screen}`
}

type Tagged<T> = { key: string; error: string; data: T | null }

function HeatmapView({ projectId }: { projectId: number }) {
  const [days, setDays] = useState(30)
  const [appVersion, setAppVersion] = useState('')
  const [aspect, setAspect] = useState<number | ''>('')
  const [search, setSearch] = useState(SEARCH_ALL)
  const [picked, setSelected] = useState<HeatmapScreen | null>(null)
  // ?screen=<raw name> (e.g. from User Flow) preselects that screen until the user picks another.
  const [wanted] = useState(() => new URLSearchParams(window.location.search).get('screen') ?? '')
  const [showRage, setShowRage] = useState(false)
  const [hover, setHover] = useState<{ cell: HeatmapCell; x: number; y: number } | null>(null)

  // The window is anchored when the range changes, so requests stay stable between renders.
  const range = useMemo(() => {
    const now = Date.now()
    return { from: new Date(now - days * DAY_MS).toISOString(), to: new Date(now).toISOString() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days, projectId])

  const changeDays = (v: number) => {
    setDays(v)
    setAspect('')
  }
  const changeVersion = (v: string) => {
    setAppVersion(v)
    setAspect('')
  }
  const listKey = `${projectId}|${days}|${appVersion}`
  const [list, setList] = useState<Tagged<HeatmapScreensResponse>>({ key: '', error: '', data: null })
  useEffect(() => {
    let cancelled = false
    heatmapApi
      .screens(projectId, { ...range, app_version: appVersion })
      .then((data) => !cancelled && setList({ key: listKey, error: '', data }))
      .catch((e: unknown) => !cancelled && setList({ key: listKey, error: e instanceof Error ? e.message : 'Request failed', data: null }))
    return () => {
      cancelled = true
    }
  }, [projectId, range, appVersion, listKey])
  const listLoading = list.key !== listKey

  const screens = useMemo(() => {
    return (list.data?.screens ?? []).filter((s) => screenMatches(s.screen, search))
  }, [list.data, search])
  const selected = useMemo(
    () => picked ?? (wanted ? (list.data?.screens.find((s) => s.screen === wanted) ?? null) : null),
    [picked, wanted, list.data],
  )

  const heatKey = selected ? `${listKey}|${screenKey(selected)}|${orientation}|${aspect}` : ''
  const [heat, setHeat] = useState<Tagged<HeatmapResponse>>({ key: '', error: '', data: null })
  useEffect(() => {
    if (!selected) return
    let cancelled = false
    heatmapApi
      .get(projectId, {
        ...range,
        screen: selected.screen,
        platform: selected.platform,
        orientation,
        app_version: appVersion,
        aspect: aspect === '' ? undefined : aspect,
      })
      .then((data) => {
        if (cancelled) return
        setHeat({ key: heatKey, error: '', data })
        // A remembered size that no longer exists would hide the dropdown: fall back to the default.
        if (aspect !== '' && !data.aspects.some((a) => a.aspect === aspect)) setAspect('')
      })
      .catch((e: unknown) => !cancelled && setHeat({ key: heatKey, error: e instanceof Error ? e.message : 'Request failed', data: null }))
    return () => {
      cancelled = true
    }
    // heatKey encodes every input
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [heatKey, projectId, range])
  const heatLoading = !!selected && heat.key !== heatKey
  // Keep showing the previous result while a new one loads.
  const res = heat.data

  // Background image: chunk -> frame -> data URL. Failure leaves null (neutral frame).
  const bgUrl = res?.background?.url ?? null
  const bgFrame = res?.background?.frame_index ?? 0
  const [bg, setBg] = useState<{ url: string; src: string | null } | null>(null)
  useEffect(() => {
    if (!bgUrl) return
    const ctrl = new AbortController()
    fetchChunkPayloads([bgUrl], ctrl.signal)
      .then(({ payloads }) => {
        const data = screenFrameData(payloads[0], bgFrame)
        setBg({ url: bgUrl, src: data ? `data:image/jpeg;base64,${data}` : null })
      })
      .catch(() => {
        if (!ctrl.signal.aborted) setBg({ url: bgUrl, src: null })
      })
    return () => ctrl.abort()
  }, [bgUrl, bgFrame])
  const bgSrc = bgUrl && bg?.url === bgUrl ? bg.src : null

  const versionOptions = useMemo(() => {
    const vs = new Set(list.data?.app_versions ?? [])
    if (appVersion) vs.add(appVersion)
    return [{ value: '', label: 'All versions' }, ...[...vs].map((v) => ({ value: v, label: v }))]
  }, [list.data, appVersion])

  const aspectOptions = useMemo(() => {
    const opts = (res?.aspects ?? []).map((a) => ({
      value: a.aspect,
      label: `${a.aspect.toFixed(2)} (${a.sessions} sessions)`,
    }))
    return opts
  }, [res])

  // res.aspect is long side / short side (>= 1); without a screenshot, build a portrait frame.
  const longShort = res && res.aspect >= 1 ? res.aspect : 16 / 9
  const vw = res?.background?.vw || 1
  const vh = res?.background?.vh || longShort
  const bgFailed = !!bgUrl && bg?.url === bgUrl && bg.src === null
  const hasTaps = !!res && res.cells.length > 0
  const hoverText = hover && res
    ? `${shareLabel(cellShare(hover.cell, res.screen_sessions))} of sessions · ${hover.cell.taps} ${hover.cell.taps === 1 ? 'tap' : 'taps'}`
    : ''

  return (
    <div className="hm-layout">
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
          <CustomSelect size="sm" ariaLabel="Date range" options={RANGE_OPTIONS} value={days} onChange={changeDays} />
          <CustomSelect
            size="sm"
            ariaLabel="App version"
            options={versionOptions}
            value={appVersion}
            onChange={changeVersion}
          />
        </div>
        {list.error && !listLoading ? (
          <p className="ppl-muted ppl-pad">Couldn't load screens: {list.error}</p>
        ) : listLoading ? (
          <p className="ppl-muted ppl-pad">Loading…</p>
        ) : screens.length === 0 ? (
          <div className="ppl-pad hm-empty">
            <h2 className="dash-card__title">{search ? 'No screens match' : 'No screens yet'}</h2>
            {!search && (
              <p className="ppl-muted">
                Heatmaps are built from screenshot session replays of iOS and Android apps. If replays are recorded,
                processing may still be pending: data appears about 10 minutes after a session ends.
              </p>
            )}
          </div>
        ) : (
          <ul className="hm-screens" aria-label="Screens">
            {screens.map((s) => {
              const active = selected ? screenKey(selected) === screenKey(s) : false
              return (
                <li key={screenKey(s)}>
                  <button
                    type="button"
                    className={`hm-screen${active ? ' is-active' : ''}`}
                    aria-pressed={active}
                    onClick={() => {
                      setSelected(s)
                      setAspect('')
                      if (!selected || screenKey(selected) !== screenKey(s)) setHeat({ key: '', error: '', data: null })
                      setHover(null)
                    }}
                  >
                    <span className="hm-screen__name" title={displayScreen(s.screen)}>
                      {displayScreen(s.screen)}
                    </span>
                    <span className="hm-screen__meta">
                      {s.platform} · {s.sessions} {s.sessions === 1 ? 'session' : 'sessions'} · {s.taps} {s.taps === 1 ? 'tap' : 'taps'}
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
        )}
        {(list.data?.unrecognized_taps ?? 0) > 0 && (
          <p className="ppl-muted ppl-pad">
            {list.data!.unrecognized_taps} {list.data!.unrecognized_taps === 1 ? 'tap' : 'taps'} on unrecognized screens {list.data!.unrecognized_taps === 1 ? 'is' : 'are'} hidden.
          </p>
        )}
      </aside>

      <section className="dash-card fb-card hm-main">
        {!selected ? (
          <div className="hm-empty ppl-pad">
            <h2 className="dash-card__title">Pick a screen</h2>
            <p className="ppl-muted">Choose a screen on the left to see where users tap.</p>
          </div>
        ) : heat.key === heatKey && heat.error ? (
          <p className="ppl-muted ppl-pad">Couldn't load the heatmap: {heat.error}</p>
        ) : !res ? (
          <p className="ppl-muted ppl-pad">Loading…</p>
        ) : (
          <>
            <div className="hm-toolbar">
              <h2 className="dash-card__title hm-toolbar__title" title={displayScreen(selected.screen)}>
                {displayScreen(selected.screen)}
              </h2>
              {heatLoading && <span className="ppl-muted">Loading…</span>}
              {aspectOptions.length > 0 && (
                <CustomSelect
                  size="sm"
                  ariaLabel="Screen size"
                  options={aspectOptions}
                  value={aspect === '' ? res.aspect : aspect}
                  onChange={setAspect}
                />
              )}
              <label className="hm-toggle">
                <input type="checkbox" checked={showRage} onChange={(e) => setShowRage(e.target.checked)} />
                Rage taps
              </label>
            </div>
            {res.screen_sessions < MIN_RELIABLE_SESSIONS && (
              <p className="hm-note hm-note--warn">
                Too few sessions for a reliable pattern ({res.screen_sessions} of {MIN_RELIABLE_SESSIONS} needed).
              </p>
            )}
            {!hasTaps && <p className="ppl-muted ppl-pad">No taps recorded on this screen for these filters.</p>}
            {(!res.background || bgFailed) && (
              <p className="hm-note">Screenshot unavailable: the overlay is drawn on a neutral frame.</p>
            )}
            <div className={`hm-stage${heatLoading ? ' is-loading' : ''}`}>
              <div className="hm-canvas-wrap" style={{ ['--hm-aspect' as string]: String(vw / vh) }}>
                <HeatCanvas
                  background={bgSrc}
                  vw={vw}
                  vh={vh}
                  res={res}
                  showRage={showRage}
                  onHover={(cell, pos) => setHover(cell && pos ? { cell, ...pos } : null)}
                />
                {hover && (
                  <div className="hm-tip" style={{ left: hover.x, top: hover.y }}>
                    {hoverText}
                  </div>
                )}
              </div>
              <div className="hm-legend">
                <div className="hm-legend__bar" aria-hidden="true" />
                <div className="hm-legend__labels">
                  <span>Fewer</span>
                  <span>More</span>
                </div>
                <p className="ppl-muted">
                  Colour is relative to the busiest area. Hover for the % of sessions that opened this screen and tapped
                  there.
                </p>
                <p className="ppl-muted">Positions follow the screen, not scrolled content.</p>
                <p className="ppl-muted">Updated about 10 minutes after a session ends.</p>
              </div>
            </div>
          </>
        )}
      </section>
    </div>
  )
}

export default function HeatmapPage() {
  return (
    <DashboardShell sectionTitle="Heatmap">
      {({ project }) => (
        <>
          <div className="fb-page-header">
            <div className="fb-page-header__left">
              <div className="fb-title-wrap">
                <h1 className="fb-page-title">Heatmap</h1>
                <p className="fb-page-desc">
                  Where users tap on each screen of <strong>{project?.name || 'your app'}</strong>.
                </p>
              </div>
            </div>
          </div>
          {!project ? (
            <div className="dash-empty dash-card fb-card ppl-pad">Select a project from the sidebar to see its heatmap.</div>
          ) : (
            <HeatmapView key={project.id} projectId={project.id} />
          )}
        </>
      )}
    </DashboardShell>
  )
}
