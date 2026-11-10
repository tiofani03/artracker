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
  const [copied, setCopied] = useState(false)
  const trace = event.stack_trace_resolved || event.stack_trace
  if (!trace) {
    return <p className="dash-empty">No stack trace captured for this occurrence.</p>
  }
  const lines = trace.split('\n')
  const [head, ...frames] = lines

  const copyTrace = () => {
    navigator.clipboard?.writeText(trace).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <div className="stack-viewer fb-stack-viewer">
      <div className="stack-viewer__head fb-stack-head">
        <span className="fb-stack-head__title">{head}</span>
        <button
          type="button"
          className="dash-btn dash-btn--sm fb-stack-copy-btn"
          onClick={copyTrace}
          title="Copy raw stack trace to clipboard"
        >
          {copied ? '✓ Copied' : 'Copy Stack'}
        </button>
      </div>
      <ol className="stack-viewer__frames fb-stack-frames">
        {frames.map((line, i) => (
          <li key={i} className="stack-viewer__frame fb-stack-frame">
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
  const isFatal =
    detail.type.toLowerCase().includes('fatal') ||
    detail.type.toLowerCase().includes('crash') ||
    detail.type.toLowerCase().includes('typeerror') ||
    detail.type.toLowerCase().includes('referenceerror')

  return (
    <div className="fb-detail-view">
      {/* Top Breadcrumb & Action Banner */}
      <div className="fb-detail-top">
        <Link to="/dashboard/issues" className="dash-btn dash-btn--sm fb-back-btn">
          ← Back to Issues
        </Link>
        <span className="fb-crumb-sep">/</span>
        <span className="fb-crumb-cluster">Cluster #{detail.id}</span>
      </div>

      {/* Main Issue Header Card */}
      <section className="dash-card dash-card--compact fb-card fb-detail-header-card">
        <div className="fb-detail-header__main">
          <div className="fb-detail-header__badges">
            <span className={`fb-badge-severity fb-badge-severity--${isFatal ? 'fatal' : 'nonfatal'}`}>
              {isFatal ? 'Fatal Crash' : 'Non-fatal Exception'}
            </span>
            <span className={`fb-status-tag fb-status-tag--${detail.status}`}>
              {detail.status === 'resolved' ? 'Closed' : detail.status === 'ignored' ? 'Muted' : 'Open'}
            </span>
            <span className="fb-tag-type">{detail.type}</span>
            {latest?.release_version && (
              <span className="fb-tag-release">v{latest.release_version}</span>
            )}
            {detail.assigned_to && (
              <span className="fb-assignee-chip">
                Assigned: @{detail.assigned_to.name || detail.assigned_to.email.split('@')[0]}
              </span>
            )}
          </div>

          <h2 className="fb-detail-title">{detail.title}</h2>
          {detail.latest_message && detail.latest_message !== detail.title && (
            <p className="fb-detail-msg">{detail.latest_message}</p>
          )}

          <div className="fb-detail-stats-strip">
            <div className="fb-detail-stat">
              <span className="fb-detail-stat__label">Total Events</span>
              <strong className="fb-detail-stat__val">{detail.count.toLocaleString()}</strong>
            </div>
            <div className="fb-detail-stat">
              <span className="fb-detail-stat__label">First Seen</span>
              <strong className="fb-detail-stat__val" title={detail.first_seen}>{formatAgo(detail.first_seen)}</strong>
            </div>
            <div className="fb-detail-stat">
              <span className="fb-detail-stat__label">Last Seen</span>
              <strong className="fb-detail-stat__val" title={detail.last_seen}>{formatAgo(detail.last_seen)}</strong>
            </div>
            <div className="fb-detail-stat">
              <span className="fb-detail-stat__label">Fingerprint</span>
              <span className="fb-detail-stat__code">{detail.fingerprint.slice(0, 12)}…</span>
            </div>
          </div>
        </div>

        <div className="fb-detail-actions">
          {detail.status === 'open' && (
            <>
              <button
                type="button"
                className="dash-btn dash-btn--sm dash-btn--primary"
                disabled={busy}
                onClick={() => void setStatus('resolved')}
              >
                ✓ Mark as Closed
              </button>
              <button
                type="button"
                className="dash-btn dash-btn--sm"
                disabled={busy}
                onClick={() => void setStatus('ignored')}
              >
                Mute Notifications
              </button>
            </>
          )}
          {detail.status !== 'open' && (
            <button
              type="button"
              className="dash-btn dash-btn--sm"
              disabled={busy}
              onClick={() => void setStatus('open')}
            >
              Reopen Issue
            </button>
          )}
          <button
            type="button"
            className="dash-btn dash-btn--sm"
            disabled={busy}
            onClick={() => void assign()}
          >
            {detail.assigned_to ? 'Reassign' : 'Assign to Me'}
          </button>
        </div>
      </section>

      {/* Correlated Replay Quick Banner if session_id is available */}
      {latest?.session_id && (
        <div className="fb-replay-banner">
          <div className="fb-replay-banner__info">
            <span className="fb-replay-banner__badge">SESSION REPLAY</span>
            <span>Recorded session <strong>{latest.session_id}</strong> captured right before this crash occurred.</span>
          </div>
          <Link
            to={`/dashboard/replays?session=${encodeURIComponent(latest.session_id)}`}
            className="dash-btn dash-btn--sm dash-btn--primary fb-replay-banner__btn"
          >
            Watch Session Replay →
          </Link>
        </div>
      )}

      {latest && (
        <div className="dash-split fb-split">
          <section className="dash-card dash-card--compact fb-card">
            <div className="dash-card__head">
              <div>
                <h3 className="dash-card__title">Stack Trace</h3>
                <p className="dash-card__desc">
                  {latest.stack_trace_resolved !== latest.stack_trace ? (
                    <span className="fb-pill fb-pill--green" style={{ fontSize: '0.65rem' }}>Source-mapped to original lines</span>
                  ) : (
                    <span>Minified stack trace (upload source map in Settings to demangle).</span>
                  )}
                </p>
              </div>
            </div>
            <StackTraceViewer event={latest} />
          </section>

          <section className="dash-card dash-card--compact fb-card">
            <div className="dash-card__head">
              <div>
                <h3 className="dash-card__title">User Activity &amp; Breadcrumbs</h3>
                <p className="dash-card__desc">Last {breadcrumbs.length} activities before this crash occurred.</p>
              </div>
            </div>
            {breadcrumbs.length === 0 ? (
              <p className="dash-empty">No breadcrumbs recorded for this occurrence.</p>
            ) : (
              <ol className="crumb-timeline fb-timeline">
                {breadcrumbs.map((b, i) => (
                  <li key={i} className="crumb-timeline__item fb-timeline__item">
                    <span className="crumb-timeline__dot fb-timeline__dot" aria-hidden="true" />
                    <div className="crumb-timeline__body fb-timeline__body">
                      <strong className="crumb-timeline__label fb-timeline__label">{breadcrumbLabel(b)}</strong>
                      {b.timestamp && (
                        <span className="crumb-timeline__time fb-timeline__time">{formatAgo(b.timestamp)}</span>
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
        <section className="dash-card dash-card--compact fb-card">
          <div className="dash-card__head">
            <div>
              <h3 className="dash-card__title">Device &amp; Browser Context</h3>
              <p className="dash-card__desc">Runtime telemetry captured on occurrence #{latest.id}.</p>
            </div>
          </div>
          {device.length === 0 ? (
            <p className="dash-empty">No device context recorded.</p>
          ) : (
            <div className="device-grid fb-device-grid">
              {device.map(([k, v]) => (
                <div key={k} className="device-grid__cell fb-device-cell">
                  <span className="device-grid__key fb-device-key">{k}</span>
                  <span className="device-grid__value fb-device-val">{v}</span>
                </div>
              ))}
            </div>
          )}
        </section>
      )}

      <section className="dash-card dash-card--compact fb-card">
        <div className="dash-card__head">
          <div>
            <h3 className="dash-card__title">Recent Crash Occurrences</h3>
            <p className="dash-card__desc">Chronological log of events clustered under this fingerprint.</p>
          </div>
          <span className="badge badge--role">{detail.events.length} occurrences recorded</span>
        </div>
        <div className="fb-occurrences-list">
          {detail.events.map((ev) => (
            <div key={ev.id} className="fb-occurrence-row">
              <div className="fb-occurrence-row__left">
                <span className="fb-occurrence-row__dot" aria-hidden="true" />
                <div>
                  <strong className="fb-occurrence-row__msg">{ev.message || detail.title}</strong>
                  <div className="fb-occurrence-row__meta">
                    <span>{formatAgo(ev.created_at)}</span>
                    {ev.session_id && <span>· session: <code>{ev.session_id}</code></span>}
                    {ev.release_version && <span>· release: v{ev.release_version}</span>}
                    {ev.user_id && <span>· user: {ev.user_id}</span>}
                  </div>
                </div>
              </div>
              {ev.session_id && (
                <Link
                  to={`/dashboard/replays?session=${encodeURIComponent(ev.session_id)}`}
                  className="dash-btn dash-btn--sm"
                >
                  Replay →
                </Link>
              )}
            </div>
          ))}
        </div>
      </section>
    </div>
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



