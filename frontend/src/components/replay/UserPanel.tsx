import { useEffect, useState } from 'react'
import { replayApi, type SessionContextResponse } from '../../api'
import { formatDateTime, peopleLink } from '../../people/format'
import { Link } from '../../router'
import { ContextList, IdentifySnippet, SessionRow, TraitList } from '../people/PeopleParts'

type State =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: SessionContextResponse }

/** Replay side panel "User" tab: who the user is, this session's device, and their other sessions. Render it with
 * `key={sessionId}` so switching sessions starts from the loading state. */
export function UserPanel({
  projectId,
  sessionId,
  fallbackUserId,
}: {
  projectId: number
  sessionId: string
  fallbackUserId: string
}) {
  const [state, setState] = useState<State>({ status: 'loading' })
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    replayApi
      .getSessionContext(projectId, sessionId)
      .then((data) => {
        if (!cancelled) setState({ status: 'ready', data })
      })
      .catch((err: unknown) => {
        if (!cancelled) setState({ status: 'error', message: err instanceof Error ? err.message : 'Request failed' })
      })
    return () => {
      cancelled = true
    }
  }, [projectId, sessionId, attempt])

  if (state.status === 'loading') {
    return (
      <div className="ppl-panel" aria-busy="true">
        <p className="ppl-muted">Loading user…</p>
      </div>
    )
  }
  if (state.status === 'error') {
    return (
      <div className="ppl-panel">
        <p className="ppl-muted">Couldn't load user details: {state.message}</p>
        <button
          type="button"
          className="dash-btn dash-btn--sm"
          onClick={() => {
            setState({ status: 'loading' })
            setAttempt((n) => n + 1)
          }}
        >
          Retry
        </button>
      </div>
    )
  }

  const { data } = state
  const userId = data.user_id || fallbackUserId
  const person = data.person

  return (
    <div className="ppl-panel">
      <section className="ppl-section" aria-label="User">
        {userId ? (
          <div className="ppl-identity">
            <div className="ppl-identity__id" title={userId}>
              {userId}
            </div>
            <div className="ppl-identity__actions">
              <button
                type="button"
                className="dash-btn dash-btn--sm"
                onClick={() => void navigator.clipboard?.writeText(userId)}
              >
                Copy
              </button>
              <Link to={peopleLink(userId)} className="ppl-link">
                View profile →
              </Link>
            </div>
            {person && (
              <div className="ppl-identity__meta">
                First seen {formatDateTime(person.first_seen)} · {person.session_count}{' '}
                {person.session_count === 1 ? 'session' : 'sessions'}
                {person.error_session_count > 0 && ` · ${person.error_session_count} with errors`}
              </div>
            )}
          </div>
        ) : (
          <div className="ppl-identity">
            <div className="ppl-identity__id">This session is anonymous</div>
          </div>
        )}
      </section>

      {userId && (
        <section className="ppl-section">
          <h3 className="ppl-section__title">Traits</h3>
          <TraitList traits={person?.traits ?? {}} />
        </section>
      )}

      <section className="ppl-section">
        <h3 className="ppl-section__title">This session</h3>
        <ContextList context={data.context} />
      </section>

      {userId ? (
        <section className="ppl-section">
          <h3 className="ppl-section__title">Other sessions</h3>
          {data.other_sessions.length === 0 ? (
            <p className="ppl-muted">No other sessions from this user.</p>
          ) : (
            <div className="ppl-sessions">
              {data.other_sessions.map((s) => (
                <SessionRow key={s.session_id} session={s} compact />
              ))}
            </div>
          )}
        </section>
      ) : (
        <section className="ppl-section">
          <IdentifySnippet />
        </section>
      )}
    </div>
  )
}
