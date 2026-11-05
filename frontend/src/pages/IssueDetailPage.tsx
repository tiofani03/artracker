import { useEffect, useState } from 'react'
import {
  issueApi,
  type Breadcrumb,
  type DeviceInfo,
  type IssueDetail,
  type IssueEvent,
  type IssueStatus,
} from '../api'
import DashboardShell from '../components/DashboardShell'
import { Link, useLocation } from '../router'
import '../dashboard.css'

function formatAgo(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const secs = Math.max(0, (Date.now() - then) / 1000)
  if (secs < 60) return 'just now'
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`
  return `${Math.floor(secs / 86400)}d ago`
}

function breadcrumbLabel(b: Breadcrumb): string {
  if (b.message) return b.message
  if (b.category) return b.category
  return b.type ?? 'breadcrumb'
}

function deviceRows(d: DeviceInfo): [string, string][] {
  const rows: [string, string][] = []
  if (d.os) rows.push(['OS', d.os])
  if (d.browser) rows.push(['Browser', d.browser])
  if (d.screen) rows.push(['Screen', d.screen])
  if (d.ip) rows.push(['IP', d.ip])
  if (d.location) rows.push(['Location', d.location])
  if (d.user_agent) rows.push(['User agent', d.user_agent])
  return rows
}

function StackTraceViewer({ event }: { event: IssueEvent }) {
  const trace = event.stack_trace_resolved || event.stack_trace
  if (!trace) {
    return <p className="dash-empty">No stack trace captured for this occurrence.</p>
  }
  const lines = trace.split('\n')
  const [head, ...frames] = lines
  return (
    <div className="stack-viewer">
      <div className="stack-viewer__head">{head}</div>
      <ol className="stack-viewer__frames">
        {frames.map((line, i) => (
          <li key={i} className="stack-viewer__frame">
            {line || '\u00a0'}
          </li>
        ))}
      </ol>
    </div>
  )
}

function IssueDetailView({ projectId, issueId }: { projectId: number; issueId: number }) {
  const [detail, setDetail] = useState<IssueDetail | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    issueApi
      .get(projectId, issueId)
      .then((res) => {
        if (!cancelled) setDetail(res)
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load issue')
      })
    return () => {
      cancelled = true
    }
  }, [projectId, issueId])

  const setStatus = async (status: IssueStatus) => {
    setBusy(true)
    try {
      const updated = await issueApi.updateStatus(projectId, issueId, status)
      setDetail((cur) => (cur ? { ...cur, ...updated } : cur))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Update failed')
    } finally {
      setBusy(false)
    }
  }

  const assign = async () => {
    setBusy(true)
    try {
      const updated = await issueApi.assign(projectId, issueId)
      setDetail((cur) => (cur ? { ...cur, ...updated } : cur))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Assign failed')
    } finally {
      setBusy(false)
    }
  }

  if (error) {
    return (
      <section className="dash-card">
        <p className="dash-error" role="alert">
          {error}
        </p>
        <Link to="/dashboard/issues" className="dash-btn">
          Back to issues
        </Link>
      </section>
    )
  }

  if (!detail) {
    return <p className="dash-empty dash-card">Loading issue…</p>
  }

  const latest = detail.events[0]
  const breadcrumbs = latest ? latest.breadcrumbs.slice(-10) : []
  const device = latest ? deviceRows(latest.device_info) : []

  return (
    <>
      <section className="dash-card dash-card--compact">
        <div className="dash-card__head">
          <div>
            <div className="dash-list__top">
              <span className="badge badge--exception">{detail.type}</span>
              <span className={`badge badge--${detail.status}`}>{detail.status}</span>
              <span className="badge badge--network">{detail.count.toLocaleString()} events</span>
              {detail.assigned_to && (
                <span className="badge badge--role">
                  {detail.assigned_to.name || detail.assigned_to.email}
                </span>
              )}
            </div>
            <h2 className="dash-card__title">{detail.title}</h2>
            <p className="dash-card__desc">
              First seen {formatAgo(detail.first_seen)} · Last seen {formatAgo(detail.last_seen)}
            </p>
          </div>
          <div className="dash-issue-actions">
            {detail.status === 'open' && (
              <>
                <button type="button" className="dash-btn" disabled={busy} onClick={() => void setStatus('resolved')}>
                  Mark as Resolved
                </button>
                <button type="button" className="dash-btn" disabled={busy} onClick={() => void setStatus('ignored')}>
                  Ignore
                </button>
              </>
            )}
            {detail.status !== 'open' && (
              <button type="button" className="dash-btn" disabled={busy} onClick={() => void setStatus('open')}>
                Reopen
              </button>
            )}
            <button type="button" className="dash-btn" disabled={busy} onClick={() => void assign()}>
              {detail.assigned_to ? 'Reassign' : 'Assign'}
            </button>
          </div>
        </div>
      </section>

      {latest && (
        <div className="dash-split">
          <section className="dash-card dash-card--compact">
            <div className="dash-card__head">
              <div>
                <h2 className="dash-card__title">Stack Trace</h2>
                <p className="dash-card__desc">
                  {latest.stack_trace_resolved !== latest.stack_trace
                    ? 'Source-mapped to original source lines.'
                    : 'Minified stack trace (upload a source map to demangle).'}
                </p>
              </div>
            </div>
            <StackTraceViewer event={latest} />
          </section>

          <section className="dash-card dash-card--compact">
            <div className="dash-card__head">
              <div>
                <h2 className="dash-card__title">Breadcrumbs</h2>
                <p className="dash-card__desc">Last {breadcrumbs.length} activities before the crash.</p>
              </div>
            </div>
            {breadcrumbs.length === 0 ? (
              <p className="dash-empty">No breadcrumbs recorded.</p>
            ) : (
              <ol className="crumb-timeline">
                {breadcrumbs.map((b, i) => (
                  <li key={i} className="crumb-timeline__item">
                    <span className="crumb-timeline__dot" aria-hidden="true" />
                    <div className="crumb-timeline__body">
                      <strong className="crumb-timeline__label">{breadcrumbLabel(b)}</strong>
                      {b.timestamp && (
                        <span className="crumb-timeline__time">{formatAgo(b.timestamp)}</span>
                      )}
                    </div>
                  </li>
                ))}
              </ol>
            )}
          </section>
        </div>
      )}

      {latest && (
        <section className="dash-card dash-card--compact">
          <div className="dash-card__head">
            <div>
              <h2 className="dash-card__title">Device &amp; Browser Context</h2>
              <p className="dash-card__desc">Environment of the most recent occurrence.</p>
            </div>
          </div>
          {device.length === 0 ? (
            <p className="dash-empty">No device context recorded.</p>
          ) : (
            <div className="device-grid">
              {device.map(([k, v]) => (
                <div key={k} className="device-grid__cell">
                  <span className="device-grid__key">{k}</span>
                  <span className="device-grid__value">{v}</span>
                </div>
              ))}
            </div>
          )}
        </section>
      )}

      <section className="dash-card dash-card--compact">
        <div className="dash-card__head">
          <div>
            <h2 className="dash-card__title">Occurrences</h2>
            <p className="dash-card__desc">Every recorded crash for this issue.</p>
          </div>
        </div>
        <div className="dash-list">
          {detail.events.map((ev) => (
            <div key={ev.id} className="dash-list__row">
              <div className="dash-list__main">
                <p className="dash-list__title">{ev.message}</p>
                <p className="dash-list__sub">
                  {formatAgo(ev.created_at)} · session {ev.session_id || 'n/a'}
                  {ev.release_version ? ` · release ${ev.release_version}` : ''}
                </p>
              </div>
            </div>
          ))}
        </div>
      </section>
    </>
  )
}

export default function IssueDetailPage() {
  const path = useLocation().replace(/\/+$/, '') || '/'
  const issueId = Number(path.split('/').filter(Boolean).pop() ?? 0)

  return (
    <DashboardShell sectionTitle="Issue Details">
      {({ project, isDemo }) => {
        if (isDemo) {
          return (
            <section className="dash-card">
              <h2 className="dash-card__title">Sign in to view crash diagnostics</h2>
              <p className="dash-card__desc">
                Authenticate with Google SSO to inspect grouped crashes, breadcrumbs, and demangled
                stack traces.
              </p>
            </section>
          )
        }
        if (!project) {
          return <p className="dash-empty dash-card">Select a project to inspect this issue.</p>
        }
        if (!Number.isFinite(issueId) || issueId <= 0) {
          return (
            <p className="dash-empty dash-card">
              Invalid issue id.{' '}
              <Link to="/dashboard/issues" className="dash-btn">
                Back to issues
              </Link>
            </p>
          )
        }
        return (
          <IssueDetailView key={`${project.id}:${issueId}`} projectId={project.id} issueId={issueId} />
        )
      }}
    </DashboardShell>
  )
}



