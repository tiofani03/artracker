import type { PersonSession } from '../../api'
import { formatDurationMs } from '../../replay/format'
import { Link } from '../../router'
import { contextRows, formatDateTime, sessionReplayLink, spanMs, traitEntries } from '../../people/format'

/** Key/value list used for traits and device context. */
export function KeyValueList({ rows, empty }: { rows: [string, string][]; empty: string }) {
  if (rows.length === 0) return <p className="ppl-muted">{empty}</p>
  return (
    <dl className="ppl-kv">
      {rows.map(([k, v]) => (
        <div key={k} className="ppl-kv__row">
          <dt className="ppl-kv__k">{k}</dt>
          <dd className="ppl-kv__v" title={v}>
            {v}
          </dd>
        </div>
      ))}
    </dl>
  )
}

export function TraitList({ traits }: { traits: Record<string, unknown> }) {
  return <KeyValueList rows={traitEntries(traits)} empty="No traits sent for this user." />
}

export function ContextList({ context }: { context: Record<string, unknown> }) {
  return (
    <KeyValueList
      rows={contextRows(context).map((r) => [r.label, r.value])}
      empty="No device context recorded for this session."
    />
  )
}

/** A session row: date, duration, badges; opens the session in Session Replay. */
export function SessionRow({ session, compact = false }: { session: PersonSession; compact?: boolean }) {
  const duration = spanMs(session.first_seen, session.last_seen)
  return (
    <Link to={sessionReplayLink(session.session_id)} className={`ppl-session${compact ? ' ppl-session--compact' : ''}`}>
      <span className="ppl-session__when">{formatDateTime(session.last_seen)}</span>
      <span className="ppl-session__dur">{duration > 0 ? formatDurationMs(duration) : '—'}</span>
      <span className="ppl-session__badges">
        {session.has_error && <span className="badge badge--exception">Error</span>}
        {session.has_replay ? (
          <span className="badge badge--network">Replay</span>
        ) : (
          <span className="ppl-muted">No replay</span>
        )}
      </span>
      <span className="ppl-session__go" aria-hidden="true">
        →
      </span>
    </Link>
  )
}

/** How to identify users, shown wherever a session or project has no identified user. */
export function IdentifySnippet() {
  return (
    <div className="ppl-snippet">
      <p className="ppl-muted">
        Call <code>identify</code> after login so sessions show who the user is, and <code>reset</code> on logout.
      </p>
      <div className="ppl-snippet__label">Android</div>
      <pre className="dash-code ppl-snippet__code">{`ARTracker.identify("u_123", mapOf("plan" to "pro"))
ARTracker.reset()`}</pre>
      <div className="ppl-snippet__label">iOS</div>
      <pre className="dash-code ppl-snippet__code">{`ARTracker.identify(userId: "u_123", traits: ["plan": "pro"])
ARTracker.reset()`}</pre>
      <Link to="/docs#identify-users" className="ppl-link">
        Identify users docs →
      </Link>
    </div>
  )
}
