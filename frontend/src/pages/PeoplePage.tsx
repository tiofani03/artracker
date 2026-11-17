import { useEffect, useState } from 'react'
import { ApiError, peopleApi, type PeopleListResponse, type PersonDetailResponse } from '../api'
import DashboardShell from '../components/DashboardShell'
import { ContextList, IdentifySnippet, SessionRow, TraitList } from '../components/people/PeopleParts'
import { deviceSummary, formatDateTime, peopleLink, traitEntries } from '../people/format'
import { Link, navigate, useSearch } from '../router'

const PAGE_SIZE = 25
const SEARCH_DEBOUNCE_MS = 300

function setQuery(updates: Record<string, string | null>) {
  const qs = new URLSearchParams(window.location.search)
  for (const [k, v] of Object.entries(updates)) {
    if (v) qs.set(k, v)
    else qs.delete(k)
  }
  const s = qs.toString()
  navigate(`/dashboard/people${s ? `?${s}` : ''}`, true)
}

function PeopleList({ projectId }: { projectId: number }) {
  const search = new URLSearchParams(useSearch())
  const query = search.get('q') ?? ''
  const offset = Math.max(0, Number(search.get('offset') ?? 0) || 0)
  const [draft, setDraft] = useState(query)
  // The URL changed from outside the box (sidebar link, back button): show that query, don't write the old one back.
  const [seenQuery, setSeenQuery] = useState(query)
  if (query !== seenQuery) {
    setSeenQuery(query)
    if (draft.trim() !== query) setDraft(query)
  }
  // Results are tagged with the request they answer; loading = the latest result is for an older request.
  const requestKey = `${projectId}|${query}|${offset}`
  const [result, setResult] = useState<{ key: string; error: string; data: PeopleListResponse | null }>({
    key: '',
    error: '',
    data: null,
  })
  const state = { loading: result.key !== requestKey, error: result.key === requestKey ? result.error : '', data: result.data }

  // Debounce the search box into the URL (?q=), resetting paging.
  useEffect(() => {
    if (draft.trim() === query) return
    const t = window.setTimeout(() => setQuery({ q: draft.trim() || null, offset: null }), SEARCH_DEBOUNCE_MS)
    return () => window.clearTimeout(t)
  }, [draft, query])

  useEffect(() => {
    let cancelled = false
    const key = `${projectId}|${query}|${offset}`
    peopleApi
      .list(projectId, { search: query || undefined, limit: PAGE_SIZE, offset })
      .then((data) => {
        if (!cancelled) setResult({ key, error: '', data })
      })
      .catch((err: unknown) => {
        if (!cancelled) setResult({ key, error: err instanceof Error ? err.message : 'Request failed', data: null })
      })
    return () => {
      cancelled = true
    }
  }, [projectId, query, offset])

  const data = state.data
  const items = data?.items ?? []
  const total = data?.total ?? 0

  return (
    <section className="dash-card fb-card ppl-card">
      <div className="ppl-toolbar">
        <input
          type="search"
          className="dash-input ppl-search"
          placeholder="Search user id or trait value"
          aria-label="Search people"
          value={draft}
          maxLength={128}
          onChange={(e) => setDraft(e.target.value)}
        />
        <span className="ppl-muted">{state.loading ? 'Loading…' : `${total} ${total === 1 ? 'person' : 'people'}`}</span>
      </div>

      {state.error ? (
        <p className="ppl-muted ppl-pad">Couldn't load people: {state.error}</p>
      ) : !state.loading && items.length === 0 ? (
        query ? (
          <p className="ppl-muted ppl-pad">No people match “{query}”.</p>
        ) : (
          <div className="ppl-pad">
            <h2 className="dash-card__title">No identified users yet</h2>
            <IdentifySnippet />
          </div>
        )
      ) : (
        <div className="ppl-table" role="table" aria-label="People">
          <div className="ppl-table__head" role="row">
            <span role="columnheader">User</span>
            <span role="columnheader">Traits</span>
            <span role="columnheader">Last seen</span>
            <span role="columnheader">Sessions</span>
            <span role="columnheader">Errors</span>
            <span role="columnheader">Last device</span>
          </div>
          {items.map((p) => (
            <Link key={p.user_id} to={peopleLink(p.user_id)} className="ppl-table__row" role="row">
              <span role="cell" className="ppl-table__user" title={p.user_id}>
                {p.user_id}
              </span>
              <span role="cell" className="ppl-table__traits">
                {traitEntries(p.traits, 3).map(([k, v]) => (
                  <span key={k} className="ppl-chip" title={`${k}: ${v}`}>
                    {k}: {v}
                  </span>
                ))}
              </span>
              <span role="cell" data-label="Last seen">
                {formatDateTime(p.last_seen)}
              </span>
              <span role="cell" data-label="Sessions">
                {p.session_count}
              </span>
              <span role="cell" data-label="Errors" className={p.error_session_count > 0 ? 'ppl-error-count' : undefined}>
                {p.error_session_count}
              </span>
              <span role="cell" data-label="Device" className="ppl-muted">
                {deviceSummary(p.last_context)}
              </span>
            </Link>
          ))}
        </div>
      )}

      {total > PAGE_SIZE && (
        <div className="ppl-pager">
          <button
            type="button"
            className="dash-btn dash-btn--sm"
            disabled={offset === 0 || state.loading}
            onClick={() => setQuery({ offset: offset - PAGE_SIZE > 0 ? String(offset - PAGE_SIZE) : null })}
          >
            ← Prev
          </button>
          <span className="ppl-muted">
            {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
          </span>
          <button
            type="button"
            className="dash-btn dash-btn--sm"
            disabled={offset + PAGE_SIZE >= total || state.loading}
            onClick={() => setQuery({ offset: String(offset + PAGE_SIZE) })}
          >
            Next →
          </button>
        </div>
      )}
    </section>
  )
}

function PersonDetail({ projectId, userId }: { projectId: number; userId: string }) {
  const [state, setState] = useState<{ loading: boolean; notFound: boolean; error: string; data: PersonDetailResponse | null }>({
    loading: true,
    notFound: false,
    error: '',
    data: null,
  })

  useEffect(() => {
    // The parent keys this component by project and user, so it always mounts in the loading state.
    let cancelled = false
    peopleApi
      .get(projectId, userId)
      .then((data) => {
        if (!cancelled) setState({ loading: false, notFound: false, error: '', data })
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (err instanceof ApiError && err.status === 404) setState({ loading: false, notFound: true, error: '', data: null })
        else setState({ loading: false, notFound: false, error: err instanceof Error ? err.message : 'Request failed', data: null })
      })
    return () => {
      cancelled = true
    }
  }, [projectId, userId])

  const back = (
    <Link to="/dashboard/people" className="ppl-link">
      ← Back to People
    </Link>
  )

  if (state.loading) return <section className="dash-card fb-card ppl-card ppl-pad">{back}<p className="ppl-muted">Loading…</p></section>
  if (state.notFound || !state.data) {
    return (
      <section className="dash-card fb-card ppl-card ppl-pad">
        {back}
        <h2 className="dash-card__title">{state.notFound ? 'No profile for this user yet' : "Couldn't load this user"}</h2>
        <p className="ppl-muted">
          {state.notFound
            ? `Nothing has been recorded for “${userId}” in this project.`
            : state.error}
        </p>
      </section>
    )
  }

  const { person, sessions } = state.data
  return (
    <div className="ppl-detail">
      <section className="dash-card fb-card ppl-card ppl-pad">
        {back}
        <h2 className="ppl-detail__id" title={person.user_id}>
          {person.user_id}
        </h2>
        <p className="ppl-muted">
          First seen {formatDateTime(person.first_seen)} · Last seen {formatDateTime(person.last_seen)} ·{' '}
          {person.session_count} {person.session_count === 1 ? 'session' : 'sessions'}
          {person.error_session_count > 0 && ` · ${person.error_session_count} with errors`}
        </p>
      </section>

      <div className="ppl-detail__grid">
        <section className="dash-card fb-card ppl-card ppl-pad">
          <h3 className="ppl-section__title">Traits</h3>
          <TraitList traits={person.traits} />
        </section>
        <section className="dash-card fb-card ppl-card ppl-pad">
          <h3 className="ppl-section__title">Last device</h3>
          <ContextList context={person.last_context} />
        </section>
      </div>

      <section className="dash-card fb-card ppl-card ppl-pad">
        <h3 className="ppl-section__title">Sessions</h3>
        {sessions.length === 0 ? (
          <p className="ppl-muted">No sessions recorded.</p>
        ) : (
          <div className="ppl-sessions" aria-label="Sessions">
            {sessions.map((s) => (
              <SessionRow key={s.session_id} session={s} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

export default function PeoplePage() {
  const userId = new URLSearchParams(useSearch()).get('user')
  return (
    <DashboardShell sectionTitle="People" allowDemo>
      {({ project, isDemo }) => (
        <>
          <div className="fb-page-header">
            <div className="fb-page-header__left">
              <div className="fb-title-wrap">
                <h1 className="fb-page-title">People</h1>
                <p className="fb-page-desc">
                  End users of <strong>{project?.name || 'your app'}</strong>: their traits, devices and sessions.
                </p>
              </div>
            </div>
          </div>
          {isDemo ? (
            <section className="dash-card fb-card ppl-card ppl-pad" style={{ textAlign: 'center' }}>
              <h2 className="dash-card__title">Sign in to view people</h2>
              <p className="ppl-muted">People are not available in preview mode.</p>
              <Link to="/login" className="dash-btn dash-btn--primary">
                Sign in to ARTracker
              </Link>
            </section>
          ) : !project ? (
            <div className="dash-empty dash-card fb-card ppl-pad">Select a project from the sidebar to see its people.</div>
          ) : userId ? (
            <PersonDetail key={`${project.id}:${userId}`} projectId={project.id} userId={userId} />
          ) : (
            <PeopleList key={project.id} projectId={project.id} />
          )}
        </>
      )}
    </DashboardShell>
  )
}
