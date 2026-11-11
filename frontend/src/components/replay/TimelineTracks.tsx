import { useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { offsetFromPointer } from '../../replay/playback'
import { browserStorage, readStringSet, writeStringSet } from '../../replay/prefs'
import { overflowCount, seriesPath, visibleTracks, type TrackDef } from '../../replay/tracks'

const HIDDEN_KEY = 'artracker.replay.hiddenTracks'
/** The network lane draws at most this many spans, then shows "+N". */
const MAX_LANE_SPANS = 15
const SERIES_H = 24

export type TimelineTracksProps = {
  defs: TrackDef[]
  durationMs: number
  currentMs: number
  onSeek(ms: number): void
  expanded: boolean
  onExpandedChange(v: boolean): void
}

const clampPct = (n: number, lo = 0, hi = 100) => Math.min(hi, Math.max(lo, n))

function Lane({
  def,
  durationMs,
  progressPct,
  onSeek,
}: {
  def: TrackDef
  durationMs: number
  progressPct: number
  onSeek(ms: number): void
}) {
  if (def.kind === 'spans') {
    const all = def.spans ?? []
    const cap = def.id === 'network' ? MAX_LANE_SPANS : all.length
    const shown = all.slice(0, cap)
    const hidden = overflowCount(all.length, shown.length)
    return (
      <div className={`or-gantt-lane or-gantt-lane--${def.id}`}>
        {shown.map((s, i) => {
          const left = clampPct((s.startMs / durationMs) * 100)
          const width = Math.max(def.id === 'screens' ? 2 : 0.8, ((s.endMs - s.startMs) / durationMs) * 100)
          const isScreen = def.id === 'screens'
          return (
            <div
              key={i}
              className={isScreen ? 'or-gantt-screen-tag' : `or-gantt-net-span${s.tone === 'error' ? ' is-error' : ''}`}
              style={{ left: `${left}%`, width: `${Math.min(width, 100 - left)}%` }}
              title={s.label}
              onClick={(e) => {
                e.stopPropagation()
                onSeek(s.startMs)
              }}
            >
              {isScreen && <span>{s.label}</span>}
            </div>
          )
        })}
        {hidden > 0 && <span className="or-gantt-overflow">+{hidden}</span>}
      </div>
    )
  }

  if (def.kind === 'series') {
    const maxV = def.max ?? 100
    const d = seriesPath(def.points ?? [], durationMs, 100, SERIES_H, maxV)
    const thresholdY =
      def.threshold === undefined ? null : SERIES_H - Math.min(1, Math.max(0, def.threshold / maxV)) * SERIES_H
    const scale = `${Math.round(maxV)}${def.unit === '%' ? '%' : def.unit ? ` ${def.unit}` : ''}`
    return (
      <div className={`or-gantt-lane or-gantt-lane--series or-gantt-lane--${def.id}`}>
        <svg className="or-track-svg" viewBox={`0 0 100 ${SERIES_H}`} preserveAspectRatio="none" aria-hidden="true">
          {(def.bands ?? []).map((b, i) => {
            const x = clampPct((b.startMs / durationMs) * 100)
            const w = Math.min(100 - x, Math.max(0.4, ((b.endMs - b.startMs) / durationMs) * 100))
            return <rect key={i} className="or-track-band" x={x} y={0} width={w} height={SERIES_H} />
          })}
          {thresholdY !== null && (
            <line className="or-track-threshold" x1={0} x2={100} y1={thresholdY} y2={thresholdY} />
          )}
          <path d={d} className={`or-track-line or-track-line--${def.id}`} />
        </svg>
        <span className="or-track-scale" aria-hidden="true">
          {scale}
        </span>
      </div>
    )
  }

  return (
    <div className="or-gantt-rail">
      <div className="replay-marker-track__rail" />
      <div className="replay-marker-track__fill" style={{ width: `${progressPct}%` }} />
      {(def.markers ?? []).map((m) => (
        <button
          key={m.id}
          type="button"
          className={`replay-marker-dot replay-marker-dot--${m.tone}`}
          style={{ left: `${clampPct((m.offsetMs / durationMs) * 100, 1, 99)}%` }}
          aria-label={m.label}
          onClick={(e) => {
            e.stopPropagation()
            onSeek(m.offsetMs)
          }}
        >
          <span className="replay-marker-tooltip" aria-hidden="true">
            {m.label}
          </span>
        </button>
      ))}
    </div>
  )
}

/**
 * Multi-track timeline under the stage: a label column (name + value at the playhead) and one shared seek
 * surface. Which tracks exist is up to the caller; which are visible is a per-user preference.
 */
export function TimelineTracks({ defs, durationMs, currentMs, onSeek, expanded, onExpandedChange }: TimelineTracksProps) {
  const [hidden, setHidden] = useState<Set<string>>(() => readStringSet(browserStorage(), HIDDEN_KEY))
  const [pickerOpen, setPickerOpen] = useState(false)
  const scrubbingRef = useRef(false)

  const tracks = visibleTracks(defs, hidden)
  const shown = expanded ? tracks : tracks.filter((t) => t.id === 'events')
  const progressPct = durationMs > 0 ? clampPct((currentMs / durationMs) * 100) : 0

  const toggleTrack = (id: string) => {
    const next = new Set(hidden)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setHidden(next)
    writeStringSet(browserStorage(), HIDDEN_KEY, next)
  }

  const seekToPointer = (e: ReactPointerEvent<HTMLDivElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    onSeek(offsetFromPointer(e.clientX, { left: r.left, width: r.width }, durationMs))
  }

  return (
    <div className="or-tracks">
      <div className="or-tracks__head">
        <div className="or-tracks__picker">
          <button
            type="button"
            className="or-tracks__btn"
            aria-haspopup="true"
            aria-expanded={pickerOpen}
            onClick={() => setPickerOpen((o) => !o)}
          >
            Tracks ▾
          </button>
          {pickerOpen && (
            <div className="or-tracks__menu" role="group" aria-label="Visible tracks">
              {visibleTracksAll(defs).map((d) => (
                <label key={d.id} className="or-tracks__menu-item">
                  <input
                    type="checkbox"
                    checked={d.id === 'events' || !hidden.has(d.id)}
                    disabled={d.id === 'events'}
                    onChange={() => toggleTrack(d.id)}
                  />
                  <span>{d.label}</span>
                </label>
              ))}
            </div>
          )}
        </div>
        <button
          type="button"
          className="or-tracks__btn"
          aria-label={expanded ? 'Collapse timeline' : 'Expand timeline'}
          aria-pressed={expanded}
          onClick={() => onExpandedChange(!expanded)}
        >
          {expanded ? '▴' : '▾'}
        </button>
      </div>

      <div className="or-tracks__grid">
        <div className="or-tracks__labels">
          {shown.map((t) => {
            const value = t.valueAt?.(currentMs) ?? null
            return (
              <div key={t.id} className={`or-track-label or-track-row--${t.id}`}>
                <span className="or-track-label__name">{t.label}</span>
                {t.kind === 'series' && <span className="or-track-label__value">{value ?? '–'}</span>}
              </div>
            )
          })}
        </div>

        <div
          className="or-tracks__area or-gantt-scrubber"
          role="slider"
          aria-label="Seek replay"
          aria-valuemin={0}
          aria-valuemax={Math.round(durationMs / 1000)}
          aria-valuenow={Math.round(currentMs / 1000)}
          onPointerDown={(e) => {
            if ((e.target as HTMLElement).closest('.replay-marker-dot')) return
            scrubbingRef.current = true
            e.currentTarget.setPointerCapture(e.pointerId)
            seekToPointer(e)
          }}
          onPointerMove={(e) => {
            if (scrubbingRef.current) seekToPointer(e)
          }}
          onPointerUp={(e) => {
            scrubbingRef.current = false
            try {
              e.currentTarget.releasePointerCapture(e.pointerId)
            } catch {
              // pointer already released
            }
          }}
        >
          {shown.map((t) => (
            <div key={t.id} className={`or-track or-track-row or-track-row--${t.id}`} data-track={t.id}>
              <Lane def={t} durationMs={durationMs} progressPct={progressPct} onSeek={onSeek} />
            </div>
          ))}
          <div className="or-gantt-needle" style={{ left: `${progressPct}%` }} aria-hidden="true" />
        </div>
      </div>
    </div>
  )
}

/** Every track the session has, for the picker (hidden or not), in display order. */
function visibleTracksAll(defs: TrackDef[]): TrackDef[] {
  return visibleTracks(defs, new Set())
}
