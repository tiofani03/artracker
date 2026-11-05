import { useEffect, useState } from 'react'
import { issueApi, type Issue, type IssueStatus } from '../api'
import DashboardShell from '../components/DashboardShell'
import { Link } from '../router'
import '../dashboard.css'

type StatusFilter = 'all' | IssueStatus
type SortKey = 'frequency' | 'newest' | 'oldest'

const STATUS_LABEL: Record<IssueStatus, string> = {
  open: 'Open',
  resolved: 'Resolved',
  ignored: 'Ignored',
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

function IssuesList({ projectId }: { projectId: number }) {
  const [items, setItems] = useState<Issue[]>([])
  const [status, setStatus] = useState<StatusFilter>('open')
  const [sort, setSort] = useState<SortKey>('frequency')
  const [q, setQ] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busyId, setBusyId] = useState<number | null>(null)

  useEffect(() => {
    let cancelled = false
    const timer = window.setTimeout(
      () => {
        setLoading(true)
        issueApi
          .list(projectId, { status, q: q.trim(), sort })
          .then((res) => {
            if (cancelled) return
            setItems(res.items)
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

  const statusTone = (s: IssueStatus) => `badge badge--${s}`

  return (
    <section className="dash-card dash-card--compact">
      <div className="dash-card__head">
        <div>
          <h2 className="dash-card__title">Crash Diagnostics</h2>
          <p className="dash-card__desc">
            Identical crashes are grouped automatically by normalized stack trace.
          </p>
        </div>
        <div className="dash-issues-toolbar">
          <div className="dash-seg" role="group" aria-label="Filter status">
            {(['all', 'open', 'resolved', 'ignored'] as StatusFilter[]).map((f) => (
              <button
                key={f}
                type="button"
                className={`dash-seg__btn${status === f ? ' is-active' : ''}`}
                aria-pressed={status === f}
                onClick={() => setStatus(f)}
              >
                {f === 'all' ? 'All' : STATUS_LABEL[f]}
              </button>
            ))}
          </div>
          <select
            className="dash-input"
            aria-label="Sort issues"
            value={sort}
            onChange={(e) => setSort(e.target.value as SortKey)}
          >
            <option value="frequency">Most frequent</option>
            <option value="newest">Newest</option>
            <option value="oldest">Oldest</option>
          </select>
          <input
            className="dash-input"
            type="search"
            placeholder="Search error message…"
            aria-label="Search error message"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
      </div>

      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}

      {loading ? (
        <p className="dash-empty">Loading issues…</p>
      ) : items.length === 0 ? (
        <div className="dash-empty">
          <p style={{ margin: '0 0 0.5rem 0' }}>No issues match the current filter.</p>
          <p style={{ fontSize: '0.8125rem', margin: 0 }}>
            Send an error event through <code>/api/v1/ingest</code> to auto-create issues here.
          </p>
        </div>
      ) : (
        <div className="dash-list">
          {items.map((it) => (
            <div key={it.id} className="dash-list__row">
              <div className="dash-list__main">
                <div className="dash-list__top">
                  <span className="badge badge--exception">{it.type}</span>
                  <span className={statusTone(it.status)}>{STATUS_LABEL[it.status]}</span>
                  {it.assigned_to && (
                    <span className="badge badge--role">
                      {it.assigned_to.name || it.assigned_to.email}
                    </span>
                  )}
                </div>
                <Link to={`/dashboard/issues/${it.id}`} className="dash-list__title">
                  {it.title}
                </Link>
                <p className="dash-list__sub">
                  {it.count.toLocaleString()} events · last seen {formatAgo(it.last_seen)}
                </p>
              </div>
              <div className="dash-issue-actions">
                {it.status !== 'resolved' && (
                  <button
                    type="button"
                    className="dash-btn"
                    disabled={busyId === it.id}
                    onClick={() => void updateStatus(it.id, 'resolved')}
                  >
                    Resolve
                  </button>
                )}
                {it.status === 'open' && (
                  <button
                    type="button"
                    className="dash-btn"
                    disabled={busyId === it.id}
                    onClick={() => void updateStatus(it.id, 'ignored')}
                  >
                    Ignore
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

export default function IssuesPage() {
  return (
    <DashboardShell sectionTitle="Issues" allowDemo>
      {({ project, isDemo, activeOrg }) => (
        <>
          <div className="dash-title">
            <h1>Issues</h1>
            {activeOrg && project && (
              <p>
                {activeOrg.name} · {project.name}
              </p>
            )}
          </div>

          {isDemo ? (
            <section className="dash-card">
              <h2 className="dash-card__title">Sign in to view crash diagnostics</h2>
              <p className="dash-card__desc">
                You are viewing the console in preview mode. Authenticate with Google SSO to see
                grouped crashes, breadcrumbs, and demangled stack traces for your projects.
              </p>
            </section>
          ) : !project ? (
            <p className="dash-empty dash-card">
              Select a project to inspect its issues. Create one from the Project switcher if needed.
            </p>
          ) : (
            <IssuesList key={project.id} projectId={project.id} />
          )}
        </>
      )}
    </DashboardShell>
  )
}

