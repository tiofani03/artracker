import { useEffect, useRef, useState, type ReactNode } from 'react'
import { nextScrollTop } from '../../replay/followScroll'
import { countByFilter, filterMarkers, selectableFilters, type MarkerFilter } from '../../replay/markerFilter'
import type { TimelineMarker } from '../ReplayPlayer'

export type { MarkerFilter }
export type SidePanelTab = 'events' | 'network' | 'performance'

export type ReplaySidePanelProps = {
  markers: TimelineMarker[]
  activeMarkerId: string | null
  activeFilter: MarkerFilter
  onFilterChange(f: MarkerFilter): void
  followPlayhead: boolean
  onFollowChange(v: boolean): void
  onSeek(offsetMs: number): void
  tab: SidePanelTab
  onTabChange(t: SidePanelTab): void
  /** Content of the Performance tab; an empty state is shown when absent. */
  performance?: ReactNode
}

type Snap = 'peek' | 'half' | 'full'
const NEXT_SNAP: Record<Snap, Snap> = { peek: 'half', half: 'full', full: 'peek' }

const TABS: { id: SidePanelTab; label: string }[] = [
  { id: 'events', label: 'Events' },
  { id: 'network', label: 'Network' },
  { id: 'performance', label: 'Performance' },
]

const FILTERS: { id: MarkerFilter; label: string; modifier?: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'error', label: 'Errors', modifier: 'error' },
  { id: 'network', label: 'Network', modifier: 'network' },
  { id: 'gesture', label: 'Gestures', modifier: 'gesture' },
  { id: 'perf', label: 'Performance', modifier: 'perf' },
]

const toneBadge = (tone: TimelineMarker['tone']) =>
  tone === 'error' ? 'badge--exception' : tone === 'warn' ? 'badge--console' : 'badge--network'

function networkParts(m: TimelineMarker) {
  const a = (m.rawEvent?.attributes || {}) as Record<string, unknown>
  const method = typeof a.method === 'string' ? a.method.toUpperCase() : 'HTTP'
  const url = typeof a.url === 'string' ? a.url : typeof a.endpoint === 'string' ? a.endpoint : m.text
  const status = typeof a.status_code === 'number' ? a.status_code : typeof a.status === 'number' ? a.status : null
  const ms = typeof a.latency_ms === 'number' ? a.latency_ms : typeof a.duration_ms === 'number' ? a.duration_ms : null
  return { method, url, status, ms }
}

function Inspector({ marker }: { marker: TimelineMarker }) {
  const attrs = (marker.rawEvent?.attributes || {}) as Record<string, unknown>
  const stack = typeof attrs.stack === 'string' && attrs.stack ? attrs.stack : null
  const entries = Object.entries(attrs)
    .filter(([k, v]) => v !== undefined && v !== null && k !== 'stack')
    .slice(0, 8)
  const env = marker.rawEvent?.environment
  const release = marker.rawEvent?.release_version

  return (
    <div className="or-inspector" aria-label="Event details">
      <div className="or-inspector__head">
        <span className="dash-code or-activity-time">{marker.formattedTime}</span>
        <span className={`badge ${toneBadge(marker.tone)}`}>{marker.badgeLabel}</span>
      </div>
      <div className="or-inspector__text">{marker.text}</div>
      <div className="or-activity-meta-grid">
        {env && (
          <div className="or-activity-meta-item">
            <span className="or-activity-meta-k">Env:</span>
            <span className="or-activity-meta-v">{env}</span>
          </div>
        )}
        {release && (
          <div className="or-activity-meta-item">
            <span className="or-activity-meta-k">Release:</span>
            <span className="or-activity-meta-v">{release}</span>
          </div>
        )}
        {entries.map(([k, v]) => (
          <div key={k} className="or-activity-meta-item">
            <span className="or-activity-meta-k">{k}:</span>
            <span className="or-activity-meta-v" title={String(v)}>
              {typeof v === 'object' ? JSON.stringify(v) : String(v)}
            </span>
          </div>
        ))}
      </div>
      {stack && (
        <div className="or-activity-stack-wrap">
          <div className="or-activity-stack-head">
            <span>Stack Trace</span>
            <button
              type="button"
              className="dash-btn dash-btn--sm"
              onClick={() => {
                void navigator.clipboard.writeText(stack)
              }}
            >
              Copy
            </button>
          </div>
          <pre className="dash-code or-activity-stack-pre">{stack}</pre>
        </div>
      )}
    </div>
  )
}

export function ReplaySidePanel({
  markers,
  activeMarkerId,
  activeFilter,
  onFilterChange,
  followPlayhead,
  onFollowChange,
  onSeek,
  tab,
  onTabChange,
  performance,
}: ReplaySidePanelProps) {
  const [snap, setSnap] = useState<Snap>(() =>
    typeof window !== 'undefined' && window.matchMedia?.('(max-width: 1100px)').matches ? 'peek' : 'half',
  )
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const listRef = useRef<HTMLDivElement | null>(null)
  const activeItemRef = useRef<HTMLDivElement | null>(null)

  const counts = countByFilter(markers)
  const chips = selectableFilters(counts)
  const rows = tab === 'network' ? filterMarkers(markers, 'network') : filterMarkers(markers, activeFilter)
  const selected = rows.find((m) => m.id === selectedId) ?? null

  // Follow Playhead: scroll ONLY this list (never the page) so the active event stays visible.
  useEffect(() => {
    if (!followPlayhead || tab === 'performance') return
    const list = listRef.current
    const item = activeItemRef.current
    if (!list || !item) return
    const top = nextScrollTop(list, item)
    if (top !== null) list.scrollTop = top
  }, [activeMarkerId, followPlayhead, tab, activeFilter])

  const showList = tab !== 'performance'

  return (
    <aside className="or-side-panel" data-snap={snap} aria-label="Session activity">
      <div className="or-side-panel__head">
        <div className="or-side-tabs" role="tablist" aria-label="Activity views">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              className={`or-side-tab${tab === t.id ? ' is-active' : ''}`}
              onClick={() => {
                onTabChange(t.id)
                if (snap === 'peek') setSnap('half')
              }}
            >
              {t.label}
              {t.id === 'events' && <span className="or-side-tab__count">{counts.all}</span>}
              {t.id === 'network' && counts.network > 0 && <span className="or-side-tab__count">{counts.network}</span>}
            </button>
          ))}
        </div>
        <button
          type="button"
          className="or-side-panel__sheet-toggle"
          aria-label={snap === 'full' ? 'Collapse activity panel' : 'Expand activity panel'}
          onClick={() => setSnap(NEXT_SNAP[snap])}
        >
          {snap === 'full' ? '▾' : '▴'}
        </button>
      </div>

      {showList && (
        <div className="or-side-panel__toolbar">
          {tab === 'events' && chips.length === 0 ? (
            <span className="or-side-panel__hint">{counts.all} {counts.all === 1 ? 'event' : 'events'}</span>
          ) : tab === 'events' ? (
            <div className="or-transport-filters" role="group" aria-label="Filter events">
              {FILTERS.filter((f) => chips.includes(f.id)).map((f) => (
                <button
                  key={f.id}
                  type="button"
                  className={`or-transport-filter-btn${f.modifier ? ` or-transport-filter-btn--${f.modifier}` : ''}${activeFilter === f.id ? ' is-active' : ''}`}
                  onClick={() => onFilterChange(f.id)}
                >
                  {f.label} ({counts[f.id]})
                </button>
              ))}
            </div>
          ) : (
            <span className="or-side-panel__hint">HTTP requests in this session</span>
          )}
          <label className="or-activity-follow-label" title="Auto-scroll to follow the playhead">
            <input type="checkbox" checked={followPlayhead} onChange={(e) => onFollowChange(e.target.checked)} />
            <span>Follow</span>
          </label>
        </div>
      )}

      {tab === 'performance' ? (
        <div className="or-side-panel__body">
          {performance ?? (
            <p className="dash-empty" style={{ padding: '1.5rem 1rem', margin: 0 }}>
              No profiling data for this session. Enable <code>performance</code> in the SDK config.
            </p>
          )}
        </div>
      ) : rows.length === 0 ? (
        <p className="dash-empty" style={{ padding: '1.5rem 1rem', margin: 0 }}>
          {tab === 'network' ? 'No network requests recorded.' : 'No events match the selected filter.'}
        </p>
      ) : (
        <div
          className="or-activity-list or-side-panel__list"
          ref={listRef}
          onWheel={() => onFollowChange(false)}
          onTouchMove={() => onFollowChange(false)}
        >
          {rows.map((m) => {
            const isCurrent = activeMarkerId === m.id
            const net = tab === 'network' ? networkParts(m) : null
            return (
              <div
                key={m.id}
                ref={isCurrent ? activeItemRef : null}
                className={`or-activity-row${isCurrent ? ' is-active' : ''}${selectedId === m.id ? ' is-expanded' : ''}`}
              >
                <button
                  type="button"
                  className="or-activity-row__bar or-activity-row__btn"
                  onClick={() => {
                    setSelectedId(m.id)
                    onSeek(m.offsetMs)
                  }}
                >
                  <span className="dash-code or-activity-time">{m.formattedTime}</span>
                  {net ? (
                    <>
                      <span className={`badge ${net.status !== null && net.status >= 400 ? 'badge--exception' : 'badge--network'}`}>
                        {net.method}
                        {net.status !== null ? ` ${net.status}` : ''}
                      </span>
                      <span className="or-activity-text" title={net.url}>
                        {net.url}
                      </span>
                      {net.ms !== null && <span className="or-activity-dur">{Math.round(net.ms)} ms</span>}
                    </>
                  ) : (
                    <>
                      <span className={`badge ${toneBadge(m.tone)}`}>{m.badgeLabel}</span>
                      <span className="or-activity-text" title={m.text}>
                        {m.text}
                      </span>
                    </>
                  )}
                </button>
              </div>
            )
          })}
        </div>
      )}

      {showList && selected && <Inspector marker={selected} />}
    </aside>
  )
}
