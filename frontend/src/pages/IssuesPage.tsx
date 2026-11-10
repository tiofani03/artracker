import { useEffect, useMemo, useState } from 'react'
import { issueApi, telemetryApi, type Issue, type IssueStatus, type ProjectStats } from '../api'
import DashboardShell from '../components/DashboardShell'
import CustomSelect from '../components/CustomSelect'
import { Link } from '../router'
import '../dashboard.css'

type StatusFilter = 'all' | IssueStatus
type SeverityFilter = 'all' | 'fatal' | 'non-fatal'
type SortKey = 'frequency' | 'newest' | 'oldest'

const STATUS_LABEL: Record<IssueStatus, string> = {
  open: 'Open',
  resolved: 'Closed',
  ignored: 'Muted',
}

function formatAgo(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const secs = Math.max(0, (Date.now() - then) / 1000)
  if (secs < 60) return 'just now'
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`
  return `${Math.floor(secs / 86400)}d ago`
}

function formatExactDate(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function isFatalCrash(issue: Issue): boolean {
  const type = (issue.type || '').toLowerCase()
  const title = (issue.title || '').toLowerCase()
  return (
    type.includes('fatal') ||
    type.includes('crash') ||
    type.includes('panic') ||
    title.includes('uncaught') ||
    title.includes('unhandled') ||
    title.includes('fatal') ||
    title.includes('crash') ||
    type.includes('typeerror') ||
    type.includes('referenceerror')
  )
}

function IssuesList({ projectId }: { projectId: number }) {
  const [items, setItems] = useState<Issue[]>([])
  const [stats, setStats] = useState<ProjectStats | null>(null)
  const [status, setStatus] = useState<StatusFilter>('open')
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>('all')
  const [sort, setSort] = useState<SortKey>('frequency')
  const [q, setQ] = useState('')
  const [page, setPage] = useState<number>(1)
  const [pageSize, setPageSize] = useState<number>(25)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busyId, setBusyId] = useState<number | null>(null)

  const loadIssues = () => {
    setLoading(true)
    Promise.all([
      issueApi.list(projectId, { status, q: q.trim(), sort }),
      telemetryApi.getStats(projectId, '24h').catch(() => null),
    ])
      .then(([res, statsData]) => {
        setItems(res.items)
        if (statsData) setStats(statsData)
        setError('')
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'Failed to load issues')
      })
      .finally(() => {
        setLoading(false)
      })
  }

  useEffect(() => {
    let cancelled = false
    const timer = window.setTimeout(
      () => {
        setLoading(true)
        Promise.all([
          issueApi.list(projectId, { status, q: q.trim(), sort }),
          telemetryApi.getStats(projectId, '24h').catch(() => null),
        ])
          .then(([res, statsData]) => {
            if (cancelled) return
            setItems(res.items)
            if (statsData) setStats(statsData)
            setError('')
          })
          .catch((err) => {
            if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load issues')
          })
          .finally(() => {
            if (!cancelled) setLoading(false)
          })
      },
      q ? 250 : 0,
    )
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [projectId, status, sort, q])

  const updateStatus = async (id: number, next: IssueStatus) => {
    setBusyId(id)
    try {
      const updated = await issueApi.updateStatus(projectId, id, next)
      setItems((cur) => cur.map((it) => (it.id === id ? updated : it)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Update failed')
    } finally {
      setBusyId(null)
    }
  }

  // Filter by severity
  const filteredItems = useMemo(() => {
    if (severityFilter === 'all') return items
    if (severityFilter === 'fatal') {
      return items.filter(isFatalCrash)
    }
    return items.filter((it) => !isFatalCrash(it))
  }, [items, severityFilter])

  // Overview metrics
  const totalCrashEvents = useMemo(
    () => items.reduce((acc, it) => acc + (Number(it.count) || 0), 0),
    [items],
  )
  const openCount = useMemo(() => items.filter((it) => it.status === 'open').length, [items])
  const resolvedCount = useMemo(() => items.filter((it) => it.status === 'resolved').length, [items])
  const ignoredCount = useMemo(() => items.filter((it) => it.status === 'ignored').length, [items])
  const maxEvents = useMemo(
    () => Math.max(1, ...filteredItems.map((it) => Number(it.count) || 0)),
    [filteredItems],
  )

  // Pagination
  const totalItems = filteredItems.length
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize))
  const currentPage = Math.min(page, totalPages)
  const paginatedItems = filteredItems.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  return (
    <div className="fb-crashlytics">
      {/* Overview Metric Cards */}
      <div className="fb-metric-grid">
        <div className="fb-metric-card">
          <div className="fb-metric-card__header">
            <span className="fb-metric-card__title">Crash Events</span>
            <span className="fb-pill fb-pill--red">All Occurrences</span>
          </div>
          <div className="fb-metric-card__value-row">
            <span className="fb-metric-card__value">{totalCrashEvents.toLocaleString()}</span>
          </div>
          <span className="fb-metric-card__hint">
            {stats?.errors_count ? `${stats.errors_count.toLocaleString()} in the past 24h` : 'Aggregated stack traces'}
          </span>
        </div>

        <div className="fb-metric-card">
          <div className="fb-metric-card__header">
            <span className="fb-metric-card__title">Impacted Sessions</span>
            <span className="fb-pill fb-pill--amber">User Impact</span>
          </div>
          <div className="fb-metric-card__value-row">
            <span className="fb-metric-card__value">
              {stats?.frustrated_sessions_count !== undefined
                ? stats.frustrated_sessions_count.toLocaleString()
                : Math.ceil(totalCrashEvents * 0.7).toLocaleString()}
            </span>
          </div>
          <span className="fb-metric-card__hint">Sessions affected by errors</span>
        </div>

        <div className="fb-metric-card">
          <div className="fb-metric-card__header">
            <span className="fb-metric-card__title">Open Issues</span>
            <span className={`fb-pill ${openCount > 0 ? 'fb-pill--red' : 'fb-pill--green'}`}>
              {openCount > 0 ? `${openCount} Active` : 'All Resolved'}
            </span>
          </div>
          <div className="fb-metric-card__value-row">
            <span className="fb-metric-card__value">{openCount}</span>
            <span className="fb-metric-card__subval">/ {items.length} clusters</span>
          </div>
          <span className="fb-metric-card__hint">
            {resolvedCount} closed · {ignoredCount} muted
          </span>
        </div>
      </div>

      {/* Firebase Crashlytics Main Card */}
      <section className="dash-card dash-card--compact fb-card">
        {/* Toolbar: Status tabs, Severity seg, Search & Sort */}
        <div className="fb-toolbar">
          <div className="fb-toolbar__left">
            <div className="fb-tabs" role="tablist" aria-label="Issue status filter">
              <button
                type="button"
                className={`fb-tab${status === 'open' ? ' is-active' : ''}`}
                onClick={() => {
                  setStatus('open')
                  setPage(1)
                }}
              >
                <span>Open</span>
                <span className="fb-tab__badge">{openCount}</span>
              </button>
              <button
                type="button"
                className={`fb-tab${status === 'resolved' ? ' is-active' : ''}`}
                onClick={() => {
                  setStatus('resolved')
                  setPage(1)
                }}
              >
                <span>Closed</span>
                <span className="fb-tab__badge">{resolvedCount}</span>
              </button>
              <button
                type="button"
                className={`fb-tab${status === 'ignored' ? ' is-active' : ''}`}
                onClick={() => {
                  setStatus('ignored')
                  setPage(1)
                }}
              >
                <span>Muted</span>
                <span className="fb-tab__badge">{ignoredCount}</span>
              </button>
              <button
                type="button"
                className={`fb-tab${status === 'all' ? ' is-active' : ''}`}
                onClick={() => {
                  setStatus('all')
                  setPage(1)
                }}
              >
                <span>All Issues</span>
                <span className="fb-tab__badge">{items.length}</span>
              </button>
            </div>

            <div className="dash-seg fb-severity-seg" role="group" aria-label="Crash severity">
              {(
                [
                  { id: 'all', label: 'All' },
                  { id: 'fatal', label: 'Fatal' },
                  { id: 'non-fatal', label: 'Non-fatal' },
                ] as { id: SeverityFilter; label: string }[]
              ).map((f) => (
                <button
                  key={f.id}
                  type="button"
                  className={`dash-seg__btn fb-severity-seg__btn${severityFilter === f.id ? ' is-active' : ''}`}
                  onClick={() => {
                    setSeverityFilter(f.id)
                    setPage(1)
                  }}
                >
                  {f.label}
                </button>
              ))}
            </div>
          </div>

          <div className="fb-toolbar__right">
            <div className="fb-search">
              <svg
                className="fb-search__icon"
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                aria-hidden="true"
              >
                <circle cx="11" cy="11" r="8" />
                <path d="m21 21-4.3-4.3" />
              </svg>
              <input
                className="fb-search__input"
                type="search"
                placeholder="Search error, message, stack frame…"
                aria-label="Search issues"
                value={q}
                onChange={(e) => {
                  setQ(e.target.value)
                  setPage(1)
                }}
              />
              {q && (
                <button
                  type="button"
                  className="fb-search__clear"
                  onClick={() => {
                    setQ('')
                    setPage(1)
                  }}
                  aria-label="Clear search"
                >
                  ×
                </button>
              )}
            </div>

            <div className="fb-sort-wrap" style={{ minWidth: 140 }}>
              <CustomSelect<SortKey>
                size="sm"
                options={[
                  { value: 'frequency', label: 'Most frequent' },
                  { value: 'newest', label: 'Newest first' },
                  { value: 'oldest', label: 'Oldest first' },
                ]}
                value={sort}
                onChange={(val) => {
                  setSort(val)
                  setPage(1)
                }}
                ariaLabel="Sort issues"
              />
            </div>

            <button
              type="button"
              className="dash-btn dash-btn--sm"
              onClick={loadIssues}
              title="Refresh issues from PostgreSQL"
            >
              Refresh
            </button>
          </div>
        </div>

        {error && (
          <p className="dash-error" role="alert" style={{ margin: 'var(--space-md)' }}>
            {error}
          </p>
        )}

        {loading ? (
          <div className="fb-empty-state">
            <div className="fb-empty-state__spinner" />
            <p className="fb-empty-state__title">Clustering crashes from PostgreSQL…</p>
          </div>
        ) : filteredItems.length === 0 ? (
          <div className="fb-empty-state">
            <div className="fb-empty-state__icon" aria-hidden="true">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                <path d="m9 12 2 2 4-4" />
              </svg>
            </div>
            <h3 className="fb-empty-state__title">
              {q.trim() || severityFilter !== 'all' || status !== 'open'
                ? 'No issues match the selected filters'
                : 'No crashes detected in this project'}
            </h3>
            <p className="fb-empty-state__desc">
              {q.trim() || severityFilter !== 'all' || status !== 'open'
                ? 'Try clearing the search query or switching to another status or severity filter.'
                : 'Your application is stable. Unhandled exceptions and fatal crashes sent via ARTracker SDK will appear here automatically.'}
            </p>
            {(q.trim() || severityFilter !== 'all' || status !== 'open') && (
              <button
                type="button"
                className="dash-btn dash-btn--sm"
                onClick={() => {
                  setStatus('open')
                  setSeverityFilter('all')
                  setQ('')
                  setPage(1)
                }}
              >
                Reset Filters
              </button>
            )}
          </div>
        ) : (
          <>
            {/* Data Table */}
            <div className="fb-table-wrap">
              <div className="fb-table-head" aria-hidden="true">
                <span className="fb-col-main">Issue / Stack Trace</span>
                <span className="fb-col-type">Severity</span>
                <span className="fb-col-events">Events</span>
                <span className="fb-col-time">Last Seen</span>
                <span className="fb-col-actions">Actions</span>
              </div>

              <div className="fb-table-body">
                {paginatedItems.map((it) => {
                  const fatal = isFatalCrash(it)
                  const countRatio = Math.min(100, Math.max(8, Math.round((it.count / maxEvents) * 100)))

                  return (
                    <div key={it.id} className="fb-row">
                      {/* Main column: Indicator, Title link, Subtitle */}
                      <div className="fb-col-main">
                        <div className="fb-row__header">
                          <span
                            className={`fb-row__indicator fb-row__indicator--${fatal ? 'fatal' : 'nonfatal'}`}
                            title={fatal ? 'Fatal Crash' : 'Non-fatal Exception'}
                            aria-hidden="true"
                          />

                          <div className="fb-row__text">
                            <Link to={`/dashboard/issues/${it.id}`} className="fb-row__title">
                              {it.title}
                            </Link>
                            <p className="fb-row__sub">
                              {it.latest_message && it.latest_message !== it.title ? (
                                <span className="fb-row__msg">{it.latest_message}</span>
                              ) : (
                                <span className="fb-row__msg">{it.type}</span>
                              )}
                              {it.assigned_to && (
                                <span className="fb-assignee-chip" title={`Assigned to ${it.assigned_to.email}`}>
                                  @{it.assigned_to.name || it.assigned_to.email.split('@')[0]}
                                </span>
                              )}
                            </p>
                          </div>
                        </div>
                      </div>

                      {/* Severity column */}
                      <div className="fb-col-type">
                        <span className={`fb-badge-severity fb-badge-severity--${fatal ? 'fatal' : 'nonfatal'}`}>
                          {fatal ? 'Fatal' : 'Non-fatal'}
                        </span>
                      </div>

                      {/* Events column with volume bar */}
                      <div className="fb-col-events">
                        <div className="fb-events-cell">
                          <strong className="fb-events-count">{it.count.toLocaleString()}</strong>
                          <div className="fb-events-bar-bg" title={`${it.count} occurrences`}>
                            <div
                              className={`fb-events-bar-fill ${fatal ? 'fb-events-bar-fill--fatal' : 'fb-events-bar-fill--nonfatal'}`}
                              style={{ width: `${countRatio}%` }}
                            />
                          </div>
                        </div>
                      </div>

                      {/* Last seen column */}
                      <div className="fb-col-time" title={`First: ${formatExactDate(it.first_seen)} · Last: ${formatExactDate(it.last_seen)}`}>
                        <span className="fb-time-ago">{formatAgo(it.last_seen)}</span>
                        <span className="fb-time-exact">{formatExactDate(it.last_seen)}</span>
                      </div>

                      {/* Actions column */}
                      <div className="fb-col-actions">
                        <span className={`fb-status-tag fb-status-tag--${it.status}`}>
                          {STATUS_LABEL[it.status]}
                        </span>

                        <div className="fb-action-btns">
                          {it.status !== 'resolved' ? (
                            <button
                              type="button"
                              className="dash-btn dash-btn--sm fb-btn-action"
                              disabled={busyId === it.id}
                              onClick={() => void updateStatus(it.id, 'resolved')}
                              title="Mark issue as closed / resolved"
                            >
                              Close
                            </button>
                          ) : (
                            <button
                              type="button"
                              className="dash-btn dash-btn--sm fb-btn-action"
                              disabled={busyId === it.id}
                              onClick={() => void updateStatus(it.id, 'open')}
                              title="Reopen issue"
                            >
                              Reopen
                            </button>
                          )}

                          {it.status === 'open' && (
                            <button
                              type="button"
                              className="dash-btn dash-btn--sm fb-btn-action"
                              disabled={busyId === it.id}
                              onClick={() => void updateStatus(it.id, 'ignored')}
                              title="Mute issue notifications"
                            >
                              Mute
                            </button>
                          )}
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>

            {/* Pagination Toolbar */}
            {totalItems > 0 && (
              <div className="mo-pagination fb-pagination">
                <div className="mo-pagination__left">
                  <span className="mo-pagination__info">
                    Showing <strong>{(currentPage - 1) * pageSize + 1}–{Math.min(currentPage * pageSize, totalItems)}</strong> of <strong>{totalItems}</strong> issues
                  </span>

                  <div className="mo-pagination__size-control">
                    <span className="mo-pagination__size-label">Per page:</span>
                    <div className="dash-seg mo-pagination__seg" role="group" aria-label="Page size options">
                      {[10, 25, 50, 100].map((size) => (
                        <button
                          key={size}
                          type="button"
                          className={`dash-seg__btn mo-pagination__seg-btn${pageSize === size ? ' is-active' : ''}`}
                          aria-pressed={pageSize === size}
                          onClick={() => {
                            setPageSize(size)
                            setPage(1)
                          }}
                        >
                          {size}
                        </button>
                      ))}
                    </div>
                  </div>
                </div>

                <div className="mo-pagination__nav">
                  <button
                    type="button"
                    className="dash-btn dash-btn--sm"
                    disabled={currentPage <= 1}
                    onClick={() => setPage(1)}
                    title="First page"
                  >
                    « First
                  </button>
                  <button
                    type="button"
                    className="dash-btn dash-btn--sm"
                    disabled={currentPage <= 1}
                    onClick={() => setPage((p) => Math.max(1, p - 1))}
                    title="Previous page"
                  >
                    ← Prev
                  </button>
                  <span className="mo-pagination__pages">
                    Page <strong>{currentPage}</strong> of <strong>{totalPages}</strong>
                  </span>
                  <button
                    type="button"
                    className="dash-btn dash-btn--sm"
                    disabled={currentPage >= totalPages}
                    onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                    title="Next page"
                  >
                    Next →
                  </button>
                  <button
                    type="button"
                    className="dash-btn dash-btn--sm"
                    disabled={currentPage >= totalPages}
                    onClick={() => setPage(totalPages)}
                    title="Last page"
                  >
                    Last »
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </section>
    </div>
  )
}

export default function IssuesPage() {
  return (
    <DashboardShell sectionTitle="Issues" allowDemo>
      {({ project, isDemo, activeOrg }) => (
        <>
          <div className="fb-page-header">
            <div className="fb-page-header__left">
              <div className="fb-title-wrap">
                <h1 className="fb-page-title">Issues &amp; Crash Diagnostics</h1>
                <p className="fb-page-desc">
                  Real-time error clustering, user impact metrics, and de-obfuscated call stacks for{' '}
                  <strong>{project?.name || 'your project'}</strong>.
                </p>
              </div>
            </div>

            <div className="fb-page-header__right">
              {activeOrg && (
                <span className="fb-meta-pill">
                  {activeOrg.name} {project ? `· ${project.slug}` : ''}
                </span>
              )}
            </div>
          </div>

          {isDemo ? (
            <section className="dash-card fb-card" style={{ padding: '2.5rem', textAlign: 'center' }}>
              <div className="fb-empty-state__icon" style={{ margin: '0 auto 1rem' }} aria-hidden="true">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
                  <path d="M12 8v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
                </svg>
              </div>
              <h2 className="dash-card__title">Sign in to view crash diagnostics</h2>
              <p className="dash-card__desc" style={{ maxWidth: 520, margin: '0.5rem auto 1.5rem' }}>
                You are viewing issues in preview mode. Sign in to inspect grouped fatal crashes,
                user breadcrumbs, device telemetry, and demangled stack traces.
              </p>
              <Link to="/login" className="dash-btn dash-btn--primary">
                Sign in to ARTracker
              </Link>
            </section>
          ) : !project ? (
            <div className="dash-empty dash-card fb-card" style={{ padding: '2.5rem', textAlign: 'center' }}>
              Select a project from the sidebar to inspect its crashes and issues.
            </div>
          ) : (
            <IssuesList key={project.id} projectId={project.id} />
          )}
        </>
      )}
    </DashboardShell>
  )
}

