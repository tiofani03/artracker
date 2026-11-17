import type { ReplaySessionItem, TelemetryEvent } from '../../api'
import { formatDurationMs, formatSessionId } from '../../replay/format'
import { buildTimelineRows } from '../../replay/sessionTimeline'

/** Stands in for the player on a session whose SDK sent no replay: the same header, then its events as a list. */
export function SessionTimelinePanel({
  session,
  events,
  loading,
  queueOpen,
  onToggleQueue,
}: {
  session: ReplaySessionItem
  events: TelemetryEvent[]
  loading: boolean
  queueOpen: boolean
  onToggleQueue: () => void
}) {
  const rows = buildTimelineRows(events)
  const user = session.user_id || 'anonymous_user'

  return (
    <div className="replay-player-shell">
      <div className="or-theater or-timeline-panel">
        <div className="or-unified-header">
          <div className="or-unified-header__left">
            <button
              type="button"
              className={`or-queue-toggle${queueOpen ? ' is-active' : ''}`}
              aria-pressed={queueOpen}
              aria-label="Toggle session list"
              onClick={onToggleQueue}
            >
              ≡ Sessions
            </button>
            <strong className="or-unified-header__user" title={user}>
              {user}
            </strong>
            <span className="or-unified-header__id" title={session.id}>
              {formatSessionId(session.id, 14)}
            </span>
          </div>
          <div className="or-unified-header__right">
            <span className="or-unified-header__duration">{formatDurationMs(session.duration_ms)}</span>
          </div>
        </div>

        <div className="or-timeline-panel__notice">
          <h2 className="or-timeline-panel__title">Replay not recorded for this session</h2>
          <p className="or-timeline-panel__subtitle">
            Replay is disabled in the SDK or this session was sampled out. Showing its events instead.
          </p>
        </div>

        {loading ? (
          <p className="or-timeline-panel__empty" role="status">
            Loading events…
          </p>
        ) : rows.length === 0 ? (
          <p className="or-timeline-panel__empty">No events recorded for this session.</p>
        ) : (
          <ul className="or-timeline-panel__list" aria-label="Session events">
            {rows.map((r) => (
              <li key={r.key} className={`or-timeline-panel__row or-timeline-panel__row--${r.tone}`}>
                <span className="or-timeline-panel__offset">{r.offsetLabel}</span>
                <span className="or-timeline-panel__label">{r.label}</span>
                <span className="or-timeline-panel__text">{r.text}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
