import { useEffect, useMemo, useState } from 'react'
import {
  issueApi,
  type Breadcrumb,
  type IssueDetail,
  type IssueEvent,
  type IssueStatus,
  type IssueSummary,
} from '../api'
import DashboardShell from '../components/DashboardShell'
import { Link, useLocation } from '../router'
import { toShareRows, versionRange } from '../issues/breakdown'
import { eventContext } from '../issues/device'
import { isFatalCrash, STATUS_LABEL } from '../issues/severity'
import { groupFrames, parseStackTrace, type StackFrame } from '../issues/stackTrace'
import '../dashboard.css'

type Tab = 'stack' | 'breadcrumbs' | 'device' | 'events'

function formatAgo(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '—'
  const secs = Math.max(0, (Date.now() - then) / 1000)
  if (secs < 60) return 'just now'
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`
  return `${Math.floor(secs / 86400)}d ago`
}

function formatDateTime(iso: string): string {
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

function formatTime(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' })
}

function replayHref(sessionId: string): string {
  return `/dashboard/replays?session=${encodeURIComponent(sessionId)}`
}

/* ---------------- Header ---------------- */

function IssueHeader({
  detail,
  busy,
  onStatus,
  onAssign,
}: {
  detail: IssueDetail
  busy: boolean
  onStatus: (s: IssueStatus) => void
  onAssign: () => void
}) {
  const fatal = isFatalCrash(detail)
  return (
    <div className="dash-page-header issue-header">
      <Link to="/dashboard/issues" className="issue-header__back">
        ← Issues
      </Link>
      <div className="issue-header__row">
        <div className="dash-page-header__title-wrap issue-header__title-wrap">
          <div className="issue-header__badges">
            <span className={`fb-badge-severity fb-badge-severity--${fatal ? 'fatal' : 'nonfatal'}`}>
              {fatal ? 'Fatal' : 'Non-fatal'}
            </span>
            <span className={`fb-status-tag fb-status-tag--${detail.status}`}>{STATUS_LABEL[detail.status]}</span>
            <span className="issue-header__meta">
              Issue #{detail.id} · {detail.type}
            </span>
          </div>
          <h1 className="dash-page-header__title issue-header__title">{detail.title}</h1>
          {detail.latest_message && detail.latest_message !== detail.title && (
            <p className="dash-page-header__desc issue-header__desc">{detail.latest_message}</p>
          )}
        </div>
        <div className="issue-header__actions">
          {detail.assigned_to && (
            <span className="issue-header__assignee">
              Assigned to {detail.assigned_to.name || detail.assigned_to.email}
            </span>
          )}
          <button type="button" className="dash-btn dash-btn--sm" disabled={busy} onClick={onAssign}>
            {detail.assigned_to ? 'Reassign to Me' : 'Assign to Me'}
          </button>
          {detail.status === 'open' ? (
            <>
              <button
                type="button"
                className="dash-btn dash-btn--sm"
                disabled={busy}
                onClick={() => onStatus('ignored')}
              >
                Mute
              </button>
              <button
                type="button"
                className="dash-btn dash-btn--sm dash-btn--primary"
                disabled={busy}
                onClick={() => onStatus('resolved')}
              >
                Mark as Closed
              </button>
            </>
          ) : (
            <button type="button" className="dash-btn dash-btn--sm" disabled={busy} onClick={() => onStatus('open')}>
              Reopen
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

/* ---------------- Summary strip ---------------- */

function Sparkline({ daily }: { daily: IssueSummary['daily'] }) {
  const max = Math.max(1, ...daily.map((d) => d.count))
  return (
    <div className="issue-trend" role="img" aria-label="Crashes per day, last 14 days">
      {daily.map((d) => (
        <span
          key={d.day}
          className={`issue-trend__bar${d.count === 0 ? ' is-empty' : ''}`}
          style={{ height: `${Math.max(6, (d.count / max) * 100)}%` }}
          title={`${d.day}: ${d.count.toLocaleString()} crashes`}
        />
      ))}
    </div>
  )
}

function SummaryStrip({ detail }: { detail: IssueDetail }) {
  const s = detail.summary
  return (
    <section className="dash-card dash-card--compact issue-summary" aria-label="Issue summary">
      <div className="issue-summary__stats">
        <div className="issue-stat">
          <span className="issue-stat__label">Crashes</span>
          <strong className="issue-stat__value">{detail.count.toLocaleString()}</strong>
        </div>
        <div className="issue-stat">
          <span className="issue-stat__label">Affected users</span>
          <strong className="issue-stat__value">{s.affected_users.toLocaleString()}</strong>
        </div>
        <div className="issue-stat">
          <span className="issue-stat__label">App versions</span>
          <strong className="issue-stat__value issue-stat__value--sm">{versionRange(s.app_versions)}</strong>
        </div>
        <div className="issue-stat">
          <span className="issue-stat__label">First seen</span>
          <strong className="issue-stat__value issue-stat__value--sm" title={formatDateTime(detail.first_seen)}>
            {formatAgo(detail.first_seen)}
          </strong>
        </div>
        <div className="issue-stat">
          <span className="issue-stat__label">Last seen</span>
          <strong className="issue-stat__value issue-stat__value--sm" title={formatDateTime(detail.last_seen)}>
            {formatAgo(detail.last_seen)}
          </strong>
        </div>
      </div>
      {s.daily.length > 0 && (
        <div className="issue-summary__trend">
          <span className="issue-stat__label">Last 14 days</span>
          <Sparkline daily={s.daily} />
        </div>
      )}
    </section>
  )
}

/* ---------------- Stack trace ---------------- */

function FrameLine({ frame }: { frame: StackFrame }) {
  return (
    <li className={`issue-frame${frame.app ? ' issue-frame--app' : ''}`}>
      <span className="issue-frame__symbol">{frame.symbol}</span>
      {frame.location && <span className="issue-frame__loc">{frame.location}</span>}
    </li>
  )
}

function LibraryFrames({ frames, expanded }: { frames: StackFrame[]; expanded: boolean }) {
  const [open, setOpen] = useState(false)
  if (expanded || open) return <>{frames.map((f, i) => <FrameLine key={i} frame={f} />)}</>
  return (
    <li className="issue-frame issue-frame--collapsed">
      <button type="button" className="issue-frame__expand" onClick={() => setOpen(true)}>
        {frames.length} framework {frames.length === 1 ? 'frame' : 'frames'} hidden · Show
      </button>
    </li>
  )
}

function StackTracePanel({ event }: { event: IssueEvent }) {
  const [allFrames, setAllFrames] = useState(false)
  const [copied, setCopied] = useState(false)
  const trace = event.stack_trace_resolved || event.stack_trace
  const sections = useMemo(() => parseStackTrace(trace), [trace])
  const symbolicated = Boolean(event.stack_trace) && event.stack_trace_resolved !== event.stack_trace

  if (!trace) return <p className="dash-empty">No stack trace captured for this event.</p>

  const copy = () => {
    void navigator.clipboard?.writeText(trace).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <div className="issue-stack">
      <div className="issue-toolbar">
        {symbolicated ? (
          <span className="fb-pill fb-pill--green">Deobfuscated</span>
        ) : (
          <span className="issue-toolbar__note">
            Raw trace. Upload a mapping file or source map in Settings to deobfuscate.
          </span>
        )}
        <div className="issue-toolbar__actions">
          <label className="issue-toggle">
            <input type="checkbox" checked={allFrames} onChange={(e) => setAllFrames(e.target.checked)} />
            Show all frames
          </label>
          <button type="button" className="dash-btn dash-btn--sm" onClick={copy}>
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
      </div>
      {sections.map((section, si) => (
        <div key={si} className="issue-stack__section">
          <p className="issue-stack__title">
            {section.causedBy && <span className="issue-stack__caused">Caused by</span>}
            {section.title}
          </p>
          {section.frames.length > 0 && (
            <ol className="issue-stack__frames">
              {groupFrames(section.frames).map((g, gi) =>
                g.kind === 'app' ? (
                  <FrameLine key={gi} frame={g.frame} />
                ) : (
                  <LibraryFrames key={gi} frames={g.frames} expanded={allFrames} />
                ),
              )}
            </ol>
          )}
        </div>
      ))}
    </div>
  )
}

/* ---------------- Breadcrumbs / device / events ---------------- */

function breadcrumbLabel(b: Breadcrumb): string {
  return b.message || b.category || b.type || 'Breadcrumb'
}

function BreadcrumbsPanel({ event }: { event: IssueEvent }) {
  const crumbs = event.breadcrumbs.slice(-50).reverse()
  if (crumbs.length === 0) return <p className="dash-empty">No breadcrumbs recorded for this event.</p>
  return (
    <ol className="issue-crumbs">
      {crumbs.map((b, i) => (
        <li key={i} className="issue-crumbs__item">
          <span className="issue-crumbs__time">{formatTime(b.timestamp)}</span>
          <span className="issue-crumbs__kind">{b.category || b.type || 'event'}</span>
          <span className="issue-crumbs__msg">{breadcrumbLabel(b)}</span>
        </li>
      ))}
    </ol>
  )
}

function DevicePanel({ event }: { event: IssueEvent }) {
  const rows = Object.entries(event.device_info).filter(([, v]) => v !== null && v !== undefined && v !== '')
  if (rows.length === 0) return <p className="dash-empty">No device context recorded for this event.</p>
  return (
    <dl className="issue-kv">
      {rows.map(([k, v]) => (
        <div key={k} className="issue-kv__row">
          <dt>{k}</dt>
          <dd>{typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean' ? String(v) : JSON.stringify(v)}</dd>
        </div>
      ))}
    </dl>
  )
}

function EventsPanel({
  events,
  selected,
  onSelect,
}: {
  events: IssueEvent[]
  selected: number
  onSelect: (index: number) => void
}) {
  return (
    <div className="issue-table-wrap">
      <table className="issue-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>User</th>
            <th>Device</th>
            <th>OS</th>
            <th>App version</th>
            <th aria-label="Actions" />
          </tr>
        </thead>
        <tbody>
          {events.map((ev, i) => {
            const ctx = eventContext(ev.device_info, ev.release_version)
            return (
              <tr key={ev.id} className={i === selected ? 'is-selected' : undefined}>
                <td title={formatDateTime(ev.created_at)}>{formatAgo(ev.created_at)}</td>
                <td className="issue-table__mono">{ev.user_id || '—'}</td>
                <td>{ctx.device}</td>
                <td>{ctx.os}</td>
                <td>{ctx.app}</td>
                <td>
                  <div className="issue-table__actions">
                    <button type="button" className="dash-btn dash-btn--sm" onClick={() => onSelect(i)}>
                      View
                    </button>
                    {ev.session_id && (
                      <Link to={replayHref(ev.session_id)} className="dash-btn dash-btn--sm">
                        Replay
                      </Link>
                    )}
                  </div>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function EventPanel({ events }: { events: IssueEvent[] }) {
  const [tab, setTab] = useState<Tab>('stack')
  const [index, setIndex] = useState(0)
  const event = events[index]
  const ctx = eventContext(event.device_info, event.release_version)

  const tabs: { key: Tab; label: string; count?: number }[] = [
    { key: 'stack', label: 'Stack Trace' },
    { key: 'breadcrumbs', label: 'Breadcrumbs', count: event.breadcrumbs.length },
    { key: 'device', label: 'Device' },
    { key: 'events', label: 'Events', count: events.length },
  ]

  return (
    <section className="dash-card dash-card--compact issue-event">
      <div className="issue-event__head">
        <div className="dash-page-tabs" role="tablist" aria-label="Event details">
          {tabs.map((t) => (
            <button
              key={t.key}
              type="button"
              role="tab"
              aria-selected={tab === t.key}
              className={`dash-page-tab${tab === t.key ? ' is-active' : ''}`}
              onClick={() => setTab(t.key)}
            >
              <span>{t.label}</span>
              {t.count ? <span className="dash-page-tab__badge">{t.count}</span> : null}
            </button>
          ))}
        </div>
        <div className="issue-pager" aria-label="Event navigation">
          <button
            type="button"
            className="dash-btn dash-btn--sm"
            disabled={index === 0}
            onClick={() => setIndex(index - 1)}
            aria-label="Newer event"
          >
            ‹
          </button>
          <span className="issue-pager__pos">
            Event {index + 1} of {events.length}
          </span>
          <button
            type="button"
            className="dash-btn dash-btn--sm"
            disabled={index === events.length - 1}
            onClick={() => setIndex(index + 1)}
            aria-label="Older event"
          >
            ›
          </button>
        </div>
      </div>

      {tab !== 'events' && (
        <div className="issue-event__ctx">
          <span title={formatDateTime(event.created_at)}>{formatAgo(event.created_at)}</span>
          <span>{ctx.device}</span>
          <span>{ctx.os}</span>
          <span>v{ctx.app}</span>
          {event.user_id && <span className="issue-table__mono">{event.user_id}</span>}
          {event.session_id && (
            <Link to={replayHref(event.session_id)} className="issue-event__replay">
              Watch replay →
            </Link>
          )}
        </div>
      )}

      {tab === 'stack' && <StackTracePanel key={event.id} event={event} />}
      {tab === 'breadcrumbs' && <BreadcrumbsPanel event={event} />}
      {tab === 'device' && <DevicePanel event={event} />}
      {tab === 'events' && (
        <EventsPanel
          events={events}
          selected={index}
          onSelect={(i) => {
            setIndex(i)
            setTab('stack')
          }}
        />
      )}
    </section>
  )
}

/* ---------------- Sidebar breakdowns & users ---------------- */

function BreakdownCard({ title, buckets, total }: { title: string; buckets: IssueSummary['devices']; total: number }) {
  const rows = toShareRows(buckets, total)
  return (
    <section className="dash-card dash-card--compact issue-breakdown">
      <h3 className="dash-card__title issue-breakdown__title">{title}</h3>
      {rows.length === 0 ? (
        <p className="dash-empty">No data yet.</p>
      ) : (
        <ul className="issue-breakdown__list">
          {rows.map((r) => (
            <li key={r.label} className="issue-breakdown__row" title={`${r.count.toLocaleString()} events`}>
              <div className="issue-breakdown__line">
                <span className="issue-breakdown__label">{r.label}</span>
                <span className="issue-breakdown__pct">{r.pct}%</span>
              </div>
              <span className="issue-breakdown__bar">
                <span style={{ width: `${r.pct}%` }} />
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function AffectedUsers({ summary }: { summary: IssueSummary }) {
  return (
    <section className="dash-card dash-card--compact">
      <div className="dash-card__head">
        <div>
          <h3 className="dash-card__title">Affected Users</h3>
          <p className="dash-card__desc">
            {summary.affected_users.toLocaleString()} identified users. Device, OS and app version come from each
            user's latest crash.
          </p>
        </div>
      </div>
      {summary.users.length === 0 ? (
        <p className="dash-empty">No identified users. Events were sent without a user ID.</p>
      ) : (
        <div className="issue-table-wrap">
          <table className="issue-table">
            <thead>
              <tr>
                <th>User</th>
                <th className="issue-table__num">Crashes</th>
                <th>Device</th>
                <th>OS</th>
                <th>App version</th>
                <th>Last seen</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {summary.users.map((u) => (
                <tr key={u.user_id}>
                  <td className="issue-table__mono">{u.user_id}</td>
                  <td className="issue-table__num">{u.count.toLocaleString()}</td>
                  <td>{u.device}</td>
                  <td>{u.os}</td>
                  <td>{u.app_version}</td>
                  <td title={formatDateTime(u.last_seen)}>{formatAgo(u.last_seen)}</td>
                  <td>
                    <div className="issue-table__actions">
                      {u.session_id && (
                        <Link to={replayHref(u.session_id)} className="dash-btn dash-btn--sm">
                          Replay
                        </Link>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

/* ---------------- Page ---------------- */

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

  const run = async (fn: () => Promise<Partial<IssueDetail>>, failMsg: string) => {
    setBusy(true)
    try {
      const updated = await fn()
      setDetail((cur) => (cur ? { ...cur, ...updated } : cur))
    } catch (err) {
      setError(err instanceof Error ? err.message : failMsg)
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
          Back to Issues
        </Link>
      </section>
    )
  }

  if (!detail) return <p className="dash-empty dash-card">Loading issue…</p>

  const s = detail.summary
  return (
    <div className="dash-panel-stack issue-detail">
      <IssueHeader
        detail={detail}
        busy={busy}
        onStatus={(st) => void run(() => issueApi.updateStatus(projectId, issueId, st), 'Update failed')}
        onAssign={() => void run(() => issueApi.assign(projectId, issueId), 'Assign failed')}
      />
      <SummaryStrip detail={detail} />

      <div className="issue-layout">
        <div className="issue-layout__main">
          {detail.events.length > 0 ? (
            <EventPanel events={detail.events} />
          ) : (
            <section className="dash-card">
              <p className="dash-empty">No events stored for this issue.</p>
            </section>
          )}
        </div>
        <aside className="issue-layout__side" aria-label="Breakdowns">
          <BreakdownCard title="OS Version" buckets={s.os_versions} total={s.total_events} />
          <BreakdownCard title="Device" buckets={s.devices} total={s.total_events} />
          <BreakdownCard title="App Version" buckets={s.app_versions} total={s.total_events} />
        </aside>
      </div>

      <AffectedUsers summary={s} />
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
                Sign in with Google to inspect grouped crashes, breadcrumbs and deobfuscated stack traces.
              </p>
            </section>
          )
        }
        if (!project) return <p className="dash-empty dash-card">Select a project to view this issue.</p>
        if (!Number.isFinite(issueId) || issueId <= 0) {
          return (
            <section className="dash-card">
              <p className="dash-empty">Invalid issue ID.</p>
              <Link to="/dashboard/issues" className="dash-btn">
                Back to Issues
              </Link>
            </section>
          )
        }
        return <IssueDetailView key={`${project.id}:${issueId}`} projectId={project.id} issueId={issueId} />
      }}
    </DashboardShell>
  )
}
