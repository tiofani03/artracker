import { useEffect, useState } from 'react'
import {
  keyApi,
  telemetryApi,
  type ApiKey,
  type OrgMembership,
  type Project,
  type ProjectStats,
  type TelemetryEvent,
} from '../api'
import DashboardShell from '../components/DashboardShell'
import { Link, navigate } from '../router'
import '../dashboard.css'

export type ConsoleSection = 'overview' | 'replays' | 'logs' | 'campaigns' | 'alerts'
type RangeKey = '24h' | '7d' | '30d'
type LogKind = 'all' | 'exception' | 'network' | 'console'

const SECTION_TITLES: Record<ConsoleSection, string> = {
  overview: 'Overview',
  replays: 'Session Replay',
  logs: 'Event Log',
  campaigns: 'Campaigns',
  alerts: 'Custom Alerts',
}

type BucketPoint = { label: string; sessions: number; errors: number }

type RangeStats = {
  sessions: string
  sessionsDelta: string
  errorFree: string
  errorNote: string
  conversions: string
  convRate: string
  buckets: BucketPoint[]
}

const RANGE_DATA: Record<RangeKey, RangeStats> = {
  '24h': {
    sessions: '1,482',
    sessionsDelta: '+12.4%',
    errorFree: '99.18%',
    errorNote: '12 unhandled',
    conversions: '314',
    convRate: '4.8% conv.',
    buckets: [
      { label: '00:00', sessions: 64, errors: 1 },
      { label: '02:00', sessions: 48, errors: 0 },
      { label: '04:00', sessions: 41, errors: 0 },
      { label: '06:00', sessions: 78, errors: 1 },
      { label: '08:00', sessions: 124, errors: 1 },
      { label: '10:00', sessions: 168, errors: 2 },
      { label: '12:00', sessions: 192, errors: 1 },
      { label: '14:00', sessions: 228, errors: 5 },
      { label: '16:00', sessions: 196, errors: 1 },
      { label: '18:00', sessions: 162, errors: 0 },
      { label: '20:00', sessions: 134, errors: 0 },
      { label: '22:00', sessions: 108, errors: 0 },
    ],
  },
  '7d': {
    sessions: '9,840',
    sessionsDelta: '+8.7%',
    errorFree: '99.34%',
    errorNote: '65 unhandled',
    conversions: '2,108',
    convRate: '5.1% conv.',
    buckets: [
      { label: 'Mon', sessions: 1320, errors: 9 },
      { label: 'Tue', sessions: 1480, errors: 8 },
      { label: 'Wed', sessions: 1510, errors: 11 },
      { label: 'Thu', sessions: 1690, errors: 16 },
      { label: 'Fri', sessions: 1590, errors: 10 },
      { label: 'Sat', sessions: 1180, errors: 6 },
      { label: 'Sun', sessions: 1070, errors: 5 },
    ],
  },
  '30d': {
    sessions: '41,920',
    sessionsDelta: '+19.2%',
    errorFree: '99.41%',
    errorNote: '247 unhandled',
    conversions: '8,940',
    convRate: '5.3% conv.',
    buckets: [
      { label: 'Week 1', sessions: 9420, errors: 62 },
      { label: 'Week 2', sessions: 10180, errors: 58 },
      { label: 'Week 3', sessions: 10840, errors: 69 },
      { label: 'Week 4', sessions: 11480, errors: 58 },
    ],
  },
}

type ReplaySession = {
  id: string
  user: string
  meta: string
  duration: string
  status: 'error' | 'warn' | 'clean'
  statusLabel: string
  campaign: string
  route: string
  cursorX: number
  cursorY: number
  steps: { t: string; pct: number; kind: 'dom' | 'warn' | 'error' | 'net'; text: string }[]
}

const REPLAY_SESSIONS: ReplaySession[] = [
  {
    id: 'ses_94k2p8',
    user: 'nadia.putri@fintech.id',
    meta: 'Jakarta · Chrome 131 · macOS',
    duration: '01:42',
    status: 'error',
    statusLabel: '502 + Rage click',
    campaign: 'utm_launch_q4',
    route: '/checkout/payment',
    cursorX: 66,
    cursorY: 72,
    steps: [
      { t: '00:12', pct: 12, kind: 'dom', text: 'Navigated /pricing → /checkout/payment (card input masked)' },
      { t: '00:48', pct: 46, kind: 'warn', text: 'Rage click ×4 on button#pay-submit within 380 ms' },
      { t: '00:49', pct: 54, kind: 'net', text: 'POST /api/v1/charge → 502 Bad Gateway (1,840 ms)' },
      { t: '00:50', pct: 62, kind: 'error', text: 'TypeError: Cannot read properties of undefined (PaymentForm.tsx:142)' },
    ],
  },
  {
    id: 'ses_71m9x3',
    user: 'rian.wijaya@commerce.co',
    meta: 'Bandung · Safari 18 · iOS',
    duration: '02:18',
    status: 'warn',
    statusLabel: 'Slow API 2.4s',
    campaign: 'utm_ig_reels',
    route: '/onboarding/workspace',
    cursorX: 44,
    cursorY: 56,
    steps: [
      { t: '00:08', pct: 10, kind: 'dom', text: 'Landed via ?utm_source=instagram&utm_campaign=utm_ig_reels' },
      { t: '01:04', pct: 50, kind: 'net', text: 'GET /api/v1/orgs/templates → 200 OK (2,410 ms)' },
      { t: '01:52', pct: 84, kind: 'dom', text: 'Event tracked: workspace_created { plan: "team" }' },
    ],
  },
  {
    id: 'ses_58c4v1',
    user: 'kevin.tan@saas.sg',
    meta: 'Singapore · Firefox 133 · Linux',
    duration: '03:05',
    status: 'clean',
    statusLabel: 'Converted',
    campaign: 'utm_github_readme',
    route: '/dashboard/settings',
    cursorX: 74,
    cursorY: 42,
    steps: [
      { t: '00:19', pct: 14, kind: 'dom', text: 'Authenticated via Google OAuth (/auth/callback/google)' },
      { t: '01:28', pct: 48, kind: 'dom', text: 'Event tracked: project_created { platform: "web" }' },
      { t: '02:40', pct: 88, kind: 'dom', text: 'Generated public_client SDK key art_live_…' },
    ],
  },
]

type DevLogEntry = {
  id: string
  time: string
  kind: Exclude<LogKind, 'all'>
  title: string
  detail: string
  occurrences: number
  sessionId: string
}

const DEV_LOGS: DevLogEntry[] = [
  {
    id: 'iss_104',
    time: '2m ago',
    kind: 'exception',
    title: 'TypeError: Cannot read properties of undefined (reading "charge_id")',
    detail: 'PaymentForm.tsx:142:19 · Chrome 131',
    occurrences: 12,
    sessionId: 'ses_94k2p8',
  },
  {
    id: 'iss_103',
    time: '4m ago',
    kind: 'network',
    title: 'POST /api/v1/charge → 502 Bad Gateway (1,840 ms)',
    detail: 'trace_id=tr_88a41f · payload PII redacted',
    occurrences: 19,
    sessionId: 'ses_94k2p8',
  },
  {
    id: 'iss_102',
    time: '18m ago',
    kind: 'console',
    title: 'console.warn: Slow template hydration exceeded 2000ms budget',
    detail: 'route=/onboarding/workspace · p95=2,410 ms · iOS Safari 18',
    occurrences: 7,
    sessionId: 'ses_71m9x3',
  },
  {
    id: 'iss_101',
    time: '41m ago',
    kind: 'exception',
    title: 'UnhandledRejection: NetworkError when attempting to fetch resource',
    detail: 'src/api.ts:52:25 · transient mobile handoff',
    occurrences: 3,
    sessionId: 'ses_71m9x3',
  },
]

const CAMPAIGNS = [
  { name: 'utm_launch_q4', source: 'google / cpc', visitors: '4,820', signups: '412', checkouts: '148', conv: '3.07%' },
  { name: 'utm_github_readme', source: 'github / referral', visitors: '2,910', signups: '328', checkouts: '96', conv: '3.30%' },
  { name: 'utm_ig_reels', source: 'instagram / paid', visitors: '3,440', signups: '219', checkouts: '44', conv: '1.28%' },
  { name: 'utm_newsletter_nov', source: 'email / ops-digest', visitors: '1,180', signups: '104', checkouts: '26', conv: '2.20%' },
]

const FUNNEL_STEPS = [
  { step: 'Landing View ($pageview)', count: '12,350', pct: 100 },
  { step: 'Clicked Login / Signup', count: '3,480', pct: 28 },
  { step: 'Workspace & Project Created', count: '1,063', pct: 9 },
  { step: 'Checkout Completed', count: '314', pct: 3 },
]

type AlertRule = {
  id: string
  name: string
  condition: string
  channel: string
  status: 'firing' | 'armed' | 'muted'
  lastTriggered: string
}

const INITIAL_ALERTS: AlertRule[] = [
  {
    id: 'al_1',
    name: 'Checkout 5xx Spike',
    condition: 'http.status >= 500 on /api/v1/charge > 15 events in 5m',
    channel: 'Slack · #ops-payments',
    status: 'firing',
    lastTriggered: '4m ago',
  },
  {
    id: 'al_2',
    name: 'Crash-Free Session Drop',
    condition: 'error_free_rate < 98.5% over rolling 15m window',
    channel: 'Webhook · PagerDuty',
    status: 'armed',
    lastTriggered: '3d ago',
  },
  {
    id: 'al_3',
    name: 'Campaign Conversion Anomaly',
    condition: 'utm_launch_q4 signup rate drops > 40% vs 7d baseline',
    channel: 'Discord · #growth-alerts',
    status: 'armed',
    lastTriggered: 'Never',
  },
  {
    id: 'al_4',
    name: 'Rage Click Surge on Mobile',
    condition: 'rage_click count > 25 sessions in 10m',
    channel: 'Email · eng@company.com',
    status: 'muted',
    lastTriggered: '6d ago',
  },
]

function SdkStatusBanner({
  activeOrg,
  project,
  isDemo,
  eventsCount,
  onSendTest,
  sendingTest,
}: {
  activeOrg: OrgMembership | null
  project: Project | null
  isDemo: boolean
  eventsCount?: number
  onSendTest?: () => void
  sendingTest?: boolean
}) {
  const [keys, setKeys] = useState<ApiKey[]>([])

  useEffect(() => {
    if (!project || isDemo) return
    let cancelled = false
    void keyApi
      .list(project.id)
      .then((r) => {
        if (!cancelled) setKeys(r.items)
      })
      .catch(() => {
        if (!cancelled) setKeys([])
      })
    return () => {
      cancelled = true
    }
  }, [project, isDemo])

  const activePublicKey = keys.find((k) => k.is_active && k.key_type === 'public_client')
  if (isDemo || (project && activePublicKey)) {
    return (
      <div className="sdk-strip">
        <div className="sdk-strip__left">
          <span className="console-db-dot is-up" aria-hidden="true" />
          <span>
            SDK key{' '}
            <code className="dash-code">{isDemo ? 'art_live_demo9x8k…' : `${activePublicKey?.prefix}…`}</code>{' '}
            active on <strong>{project?.slug ?? 'checkout-web'}</strong>
          </span>
        </div>
        <span className="sdk-strip__hint">
          {eventsCount !== undefined && eventsCount > 0
            ? `🟢 Live PostgreSQL connected (${eventsCount.toLocaleString()} events recorded in DB)`
            : isDemo
              ? 'Sample telemetry displayed in demo mode'
              : 'Waiting for SDK client events at /api/v1/ingest'}
        </span>
        {project && !isDemo && onSendTest && (
          <button
            type="button"
            className="dash-btn"
            onClick={onSendTest}
            disabled={sendingTest}
            style={{ marginLeft: 'auto', padding: '0.25rem 0.6rem', fontSize: '0.75rem' }}
          >
            {sendingTest ? 'Ingesting...' : '⚡ Send Real Test Event'}
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="sdk-strip sdk-strip--setup">
      <div className="sdk-strip__left">
        <span className="console-db-dot" aria-hidden="true" />
        <span>
          {!activeOrg
            ? 'Create an Organization from the left sidebar to begin.'
            : !project
              ? `Create your first Project inside ${activeOrg.name} from the left sidebar.`
              : `No active public SDK key on ${project.name} yet.`}
        </span>
      </div>
      {project && (
        <Link to="/dashboard/settings" className="dash-btn">
          Generate SDK key →
        </Link>
      )}
    </div>
  )
}

function TelemetryAreaChart({
  buckets,
  range,
}: {
  buckets: { label: string; sessions: number; errors: number }[]
  range: RangeKey
}) {
  const [activeIdx, setActiveIdx] = useState<number | null>(null)
  const w = 780
  const h = 200
  const padX = 28
  const padTop = 20
  const padBot = 30
  const plotW = w - padX * 2
  const plotH = h - padTop - padBot
  const maxVal = Math.max(...buckets.map((b) => b.sessions), 1) * 1.12

  const pts = buckets.map((b, i) => {
    const x = padX + (buckets.length === 1 ? plotW / 2 : (i / (buckets.length - 1)) * plotW)
    const y = padTop + plotH - (b.sessions / maxVal) * plotH
    return { x, y, ...b }
  })

  const linePath = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ')
  const areaPath = `${linePath} L${pts[pts.length - 1].x.toFixed(1)},${padTop + plotH} L${pts[0].x.toFixed(1)},${padTop + plotH} Z`
  const activePoint = activeIdx !== null ? pts[activeIdx] : pts[pts.length - 1]

  return (
    <div className="chart-wrap">
      <div className="chart-readout">
        <span className="dash-code">{activePoint.label}</span>
        <strong>{activePoint.sessions.toLocaleString()} sessions</strong>
        {activePoint.errors > 0 && (
          <span className="badge badge--exception">{activePoint.errors} errors</span>
        )}
      </div>

      <svg
        viewBox={`0 0 ${w} ${h}`}
        className="chart-svg"
        role="img"
        aria-label={`Session and error trend over ${range}`}
      >
        <defs>
          <linearGradient id="cobaltArea" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--color-accent)" stopOpacity="0.18" />
            <stop offset="100%" stopColor="var(--color-accent)" stopOpacity="0.0" />
          </linearGradient>
        </defs>

        {[0.25, 0.5, 0.75, 1].map((ratio) => {
          const y = padTop + plotH * ratio
          return (
            <line
              key={ratio}
              x1={padX}
              y1={y}
              x2={w - padX}
              y2={y}
              stroke="var(--color-rule)"
              strokeDasharray={ratio === 1 ? undefined : '4 4'}
              strokeWidth="1"
            />
          )
        })}

        <path d={areaPath} fill="url(#cobaltArea)" />
        <path
          d={linePath}
          fill="none"
          stroke="var(--color-accent)"
          strokeWidth="2.2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />

        {pts.map((p, idx) => (
          <g
            key={p.label}
            className="chart-pt"
            onMouseEnter={() => setActiveIdx(idx)}
            onMouseLeave={() => setActiveIdx(null)}
          >
            <rect
              x={p.x - plotW / (buckets.length * 2)}
              y={padTop}
              width={plotW / buckets.length}
              height={plotH}
              fill="transparent"
            />
            {p.errors >= 2 && (
              <line
                x1={p.x}
                y1={p.y}
                x2={p.x}
                y2={padTop + plotH}
                stroke="var(--color-error)"
                strokeDasharray="2 3"
                strokeWidth="1"
                opacity="0.55"
              />
            )}
            <circle
              cx={p.x}
              cy={p.y}
              r={activeIdx === idx ? 4.5 : p.errors >= 2 ? 3.5 : 2.5}
              fill={p.errors >= 2 ? 'var(--color-error)' : 'var(--color-paper-hi)'}
              stroke={p.errors >= 2 ? 'var(--color-error)' : 'var(--color-accent)'}
              strokeWidth="2"
            />
            <text
              x={p.x}
              y={h - 8}
              textAnchor="middle"
              className="chart-axis-text"
            >
              {p.label}
            </text>
          </g>
        ))}
      </svg>
    </div>
  )
}

function formatAgo(timestamp: string): string {
  const diffMs = Date.now() - new Date(timestamp).getTime()
  if (Number.isNaN(diffMs)) return 'just now'
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return `${Math.max(1, diffSec)}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  return `${Math.floor(diffHr / 24)}d ago`
}

function DashboardInner({
  section,
  range,
  activeOrg,
  project,
  isDemo,
  error,
}: {
  section: ConsoleSection
  range: RangeKey
  activeOrg: OrgMembership | null
  project: Project | null
  isDemo: boolean
  error: string | null
}) {
  const [selectedSessionId, setSelectedSessionId] = useState<string>(REPLAY_SESSIONS[0].id)
  const [scrubPct, setScrubPct] = useState<number>(54)
  const [logFilter, setLogFilter] = useState<LogKind>('all')
  const [alerts, setAlerts] = useState<AlertRule[]>(INITIAL_ALERTS)
  const [realStats, setRealStats] = useState<ProjectStats | null>(null)
  const [realEvents, setRealEvents] = useState<TelemetryEvent[]>([])
  const [sendingTest, setSendingTest] = useState(false)

  const reloadTelemetry = () => {
    if (!project || isDemo) return
    Promise.all([
      telemetryApi.getStats(project.id, range).catch(() => null),
      telemetryApi.listEvents(project.id, 'all', 50).catch(() => ({ items: [] })),
    ]).then(([statsData, eventsData]) => {
      setRealStats(statsData)
      setRealEvents(eventsData?.items ?? [])
    })
  }

  useEffect(() => {
    if (!project || isDemo) {
      setRealStats(null)
      setRealEvents([])
      return
    }

    let active = true
    Promise.all([
      telemetryApi.getStats(project.id, range).catch(() => null),
      telemetryApi.listEvents(project.id, 'all', 50).catch(() => ({ items: [] })),
    ]).then(([statsData, eventsData]) => {
      if (!active) return
      setRealStats(statsData)
      setRealEvents(eventsData?.items ?? [])
    })

    return () => {
      active = false
    }
  }, [project?.id, range, isDemo])

  const handleSendTestEvent = async () => {
    if (!project || isDemo) return
    try {
      setSendingTest(true)
      await telemetryApi.sendTestEvent(project.id)
      setTimeout(() => {
        reloadTelemetry()
        setSendingTest(false)
      }, 350)
    } catch (err) {
      console.error('Send test event failed', err)
      setSendingTest(false)
    }
  }

  const emptyBuckets: BucketPoint[] = [
    { label: '00:00', sessions: 0, errors: 0 },
    { label: '04:00', sessions: 0, errors: 0 },
    { label: '08:00', sessions: 0, errors: 0 },
    { label: '12:00', sessions: 0, errors: 0 },
    { label: '16:00', sessions: 0, errors: 0 },
    { label: '20:00', sessions: 0, errors: 0 },
  ]

  // Pure real database metrics when inside a project
  const stats: RangeStats =
    !isDemo && project
      ? {
          sessions: (realStats?.sessions_count ?? 0).toLocaleString(),
          sessionsDelta: `${(realStats?.events_count ?? 0).toLocaleString()} events`,
          errorFree: realStats?.error_free_pct ?? '100.0%',
          errorNote: realStats?.error_note ?? '0 unhandled',
          conversions: (realStats?.sessions_count ?? 0).toLocaleString(),
          convRate: realStats && realStats.sessions_count > 0 ? '100% active' : '0%',
          buckets:
            realStats?.buckets && realStats.buckets.length > 0
              ? realStats.buckets
              : emptyBuckets,
        }
      : RANGE_DATA[range]

  // Pure real events from PostgreSQL
  const displayLogs: DevLogEntry[] =
    !isDemo && project
      ? realEvents.map((e) => {
          let kind: Exclude<LogKind, 'all'> = 'console'
          if (e.kind === 'log' && (e.level === 'error' || e.level === 'fatal')) {
            kind = 'exception'
          } else if (
            e.name.toLowerCase().includes('http') ||
            e.name.toLowerCase().includes('fetch') ||
            e.name.toLowerCase().includes('network')
          ) {
            kind = 'network'
          }
          return {
            id: e.id,
            time: formatAgo(e.event_timestamp),
            kind,
            title: e.name || e.message || `${e.kind} event`,
            detail:
              e.message ||
              (e.environment ? `env: ${e.environment} · ${e.release_version || 'v1.0'}` : 'telemetry payload'),
            occurrences: 1,
            sessionId: e.session_id || 'n/a',
          }
        })
      : DEV_LOGS

  const replayList: ReplaySession[] =
    !isDemo && project
      ? Array.from(new Set(realEvents.map((e) => e.session_id).filter(Boolean))).map((sessId) => {
          const sessEvents = realEvents.filter((e) => e.session_id === sessId)
          const hasError = sessEvents.some((e) => e.kind === 'log' && (e.level === 'error' || e.level === 'fatal'))
          const firstEv = sessEvents[sessEvents.length - 1]
          return {
            id: sessId,
            user: firstEv.user_id || 'anonymous_user',
            route: '/checkout',
            campaign: 'direct',
            duration: formatAgo(firstEv.event_timestamp),
            status: hasError ? ('error' as const) : ('clean' as const),
            statusLabel: hasError ? 'Exception' : 'Completed',
            meta: `${sessEvents.length} events · ${firstEv.environment || 'production'}`,
            cursorX: 58,
            cursorY: 64,
            steps: sessEvents.map((ev, i) => ({
              t: `00:${String(i * 4).padStart(2, '0')}`,
              kind:
                ev.kind === 'log' && (ev.level === 'error' || ev.level === 'fatal')
                  ? ('error' as const)
                  : ev.kind === 'log' && ev.level === 'warn'
                    ? ('warn' as const)
                    : ('dom' as const),
              text: ev.name || ev.message || `${ev.kind} captured`,
              pct: Math.min(100, Math.round(((i + 1) / sessEvents.length) * 100)),
            })),
          }
        })
      : REPLAY_SESSIONS

  const activeSession: ReplaySession | undefined = replayList.find((s) => s.id === selectedSessionId) ?? replayList[0]

  const filteredLogs = logFilter === 'all' ? displayLogs : displayLogs.filter((l) => l.kind === logFilter)

  const toggleAlert = (id: string) => {
    setAlerts((prev) =>
      prev.map((a) => (a.id === id ? { ...a, status: a.status === 'muted' ? 'armed' : 'muted' } : a)),
    )
  }

  const goSection = (path: string) => navigate(isDemo ? `${path}?demo=1` : path)
  const openReplay = (sessionId: string) => {
    setSelectedSessionId(sessionId)
    goSection('/dashboard/replays')
  }

  return (
    <>
      {error && (
        <p className="dash-error" role="alert">
          {error}
        </p>
      )}

      <SdkStatusBanner
        key={project?.id ?? 'none'}
        activeOrg={activeOrg}
        project={project}
        isDemo={isDemo}
        eventsCount={realStats?.events_count}
        onSendTest={handleSendTestEvent}
        sendingTest={sendingTest}
      />

            {/* ==================== OVERVIEW ==================== */}
            {section === 'overview' && (
              <div className="dash-panel-stack">
                {/* Unified 4-Column Metric Strip */}
                <section className="metric-strip" aria-label="Key telemetry metrics">
                  <div className="metric-cell">
                    <div className="metric-cell__top">
                      <span className="metric-cell__label">Recorded Sessions</span>
                      <span className="badge badge--active">{stats.sessionsDelta}</span>
                    </div>
                    <strong className="metric-cell__value">{stats.sessions}</strong>
                    <span className="metric-cell__sub">Window: {range}</span>
                  </div>

                  <div className="metric-cell">
                    <div className="metric-cell__top">
                      <span className="metric-cell__label">Crash-Free Sessions</span>
                      <span className="badge badge--role">SLO 99%</span>
                    </div>
                    <strong className="metric-cell__value">{stats.errorFree}</strong>
                    <span className="metric-cell__sub">{stats.errorNote} exceptions</span>
                  </div>

                  <div className="metric-cell">
                    <div className="metric-cell__top">
                      <span className="metric-cell__label">Campaign Checkouts</span>
                      <span className="badge badge--network">{stats.convRate}</span>
                    </div>
                    <strong className="metric-cell__value">{stats.conversions}</strong>
                    <span className="metric-cell__sub">Top: utm_launch_q4</span>
                  </div>

                  <div className="metric-cell">
                    <div className="metric-cell__top">
                      <span className="metric-cell__label">Alert Rules</span>
                      <span className="badge badge--exception">
                        {alerts.filter((a) => a.status === 'firing').length} firing
                      </span>
                    </div>
                    <strong className="metric-cell__value">
                      {alerts.filter((a) => a.status !== 'muted').length} armed
                    </strong>
                    <span className="metric-cell__sub">Slack · Discord · Webhook</span>
                  </div>
                </section>

                {/* Area Line Chart */}
                <section className="dash-card dash-card--compact">
                  <div className="dash-card__head">
                    <div>
                      <h2 className="dash-card__title">Session Throughput &amp; Exception Spikes</h2>
                      <p className="dash-card__desc">
                        Hover data points to inspect session volume and correlated error bursts.
                      </p>
                    </div>
                    <div className="dash-legend" aria-hidden="true">
                      <span className="dash-legend__item">
                        <span className="dash-dot dash-dot--accent" /> Sessions
                      </span>
                      <span className="dash-legend__item">
                        <span className="dash-dot dash-dot--error" /> Error burst
                      </span>
                    </div>
                  </div>
                  <TelemetryAreaChart buckets={stats.buckets} range={range} />
                </section>

                {/* Bottom 2-Column Split */}
                <div className="dash-split">
                  <section className="dash-card dash-card--compact">
                    <div className="dash-card__head">
                      <div>
                        <h2 className="dash-card__title">Unresolved Dev Issues</h2>
                        <p className="dash-card__desc">Correlated stacktraces &amp; 5xx network failures.</p>
                      </div>
                      <button type="button" className="dash-btn" onClick={() => goSection('/dashboard/logs')}>
                        All logs →
                      </button>
                    </div>

                    <div className="dash-list">
                      {displayLogs.length === 0 ? (
                        <div style={{ padding: '2.5rem 1rem', textAlign: 'center', color: 'var(--color-fg-muted)' }}>
                          <p style={{ fontWeight: 500, margin: '0 0 0.25rem 0', color: 'var(--color-fg)' }}>
                            No unresolved issues
                          </p>
                          <p style={{ fontSize: '0.8125rem', margin: 0 }}>
                            {!isDemo && project && (realStats?.events_count ?? 0) === 0
                              ? 'No telemetry events ingested yet for this project.'
                              : 'All recorded sessions in this window are clean.'}
                          </p>
                        </div>
                      ) : (
                        displayLogs.slice(0, 3).map((item) => (
                          <div key={item.id} className="dash-list__row">
                            <div className="dash-list__main">
                              <div className="dash-list__top">
                                <span className={`badge badge--${item.kind}`}>{item.kind}</span>
                                <span className="dash-code">{item.occurrences}×</span>
                                <span className="dash-code">{item.time}</span>
                              </div>
                              <p className="dash-list__title">{item.title}</p>
                              <p className="dash-list__sub dash-code">{item.detail}</p>
                            </div>
                            <button
                              type="button"
                              className="dash-btn"
                              onClick={() => openReplay(item.sessionId)}
                            >
                              Replay →
                            </button>
                          </div>
                        ))
                      )}
                    </div>
                  </section>

                  <section className="dash-card dash-card--compact">
                    <div className="dash-card__head">
                      <div>
                        <h2 className="dash-card__title">Top Marketing Campaigns</h2>
                        <p className="dash-card__desc">UTM acquisition and checkout conversion.</p>
                      </div>
                      <button type="button" className="dash-btn" onClick={() => goSection('/dashboard/campaigns')}>
                        Funnel →
                      </button>
                    </div>

                    <div className="dash-table-wrap">
                      <table className="dash-table">
                        <thead>
                          <tr>
                            <th>Campaign</th>
                            <th>Visitors</th>
                            <th>Checkouts</th>
                            <th>Conv.</th>
                          </tr>
                        </thead>
                        <tbody>
                          {CAMPAIGNS.map((c) => (
                            <tr key={c.name}>
                              <td data-label="Campaign">
                                <strong className="dash-code">{c.name}</strong>
                                <div className="dash-cell-sub">{c.source}</div>
                              </td>
                              <td data-label="Visitors">{c.visitors}</td>
                              <td data-label="Checkouts">{c.checkouts}</td>
                              <td data-label="Conv.">
                                <span className="badge badge--role">{c.conv}</span>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </section>
                </div>
              </div>
            )}

            {/* ==================== SESSION REPLAY ==================== */}
            {section === 'replays' && (
              !activeSession || replayList.length === 0 ? (
                <section className="dash-card dash-card--compact" style={{ padding: '3.5rem 1.5rem', textAlign: 'center' }}>
                  <div style={{ maxWidth: 440, margin: '0 auto' }}>
                    <div style={{ fontSize: '2.25rem', marginBottom: '0.75rem' }}>📹</div>
                    <h2 className="dash-card__title" style={{ marginBottom: '0.5rem' }}>No Session Captures Yet</h2>
                    <p className="dash-card__desc" style={{ marginBottom: '1.5rem' }}>
                      Telemetry events or sessions ingested via <code>POST /api/v1/ingest</code> will automatically populate replay timelines here.
                    </p>
                    {project && !isDemo && (
                      <button
                        type="button"
                        className="dash-btn"
                        onClick={handleSendTestEvent}
                        disabled={sendingTest}
                      >
                        {sendingTest ? 'Ingesting...' : '⚡ Send Real Test Event to DB'}
                      </button>
                    )}
                  </div>
                </section>
              ) : (
                <div className="dash-replay-grid">
                  <section className="dash-card dash-card--compact" aria-label="Recorded sessions">
                    <div className="dash-card__head">
                      <div>
                        <h2 className="dash-card__title">Session Queue</h2>
                        <p className="dash-card__desc">{replayList.length} recent captures · PII masked on device</p>
                      </div>
                    </div>

                    <div className="dash-session-list">
                      {replayList.map((s) => {
                        const selected = s.id === activeSession.id
                        return (
                          <button
                            key={s.id}
                            type="button"
                            className={`dash-session-item${selected ? ' is-selected' : ''}`}
                            onClick={() => {
                              setSelectedSessionId(s.id)
                              setScrubPct(s.steps[s.steps.length - 1]?.pct ?? 50)
                            }}
                          >
                            <div className="dash-session-item__top">
                              <span className="dash-code">{s.id}</span>
                              <span
                                className={`badge ${
                                  s.status === 'error'
                                    ? 'badge--exception'
                                    : s.status === 'warn'
                                      ? 'badge--console'
                                      : 'badge--active'
                                }`}
                              >
                                {s.statusLabel}
                              </span>
                            </div>
                            <strong className="dash-session-item__user">{s.user}</strong>
                            <span className="dash-session-item__meta">
                              {s.meta} · {s.duration}
                            </span>
                          </button>
                        )
                      })}
                    </div>
                  </section>

                  <section className="dash-card dash-card--compact" aria-label="Session player">
                    <div className="dash-card__head">
                      <div>
                        <h2 className="dash-card__title">
                          {activeSession.user} · <span className="dash-code">{activeSession.route}</span>
                        </h2>
                        <p className="dash-card__desc">
                          {activeSession.meta} · campaign <code className="dash-code">{activeSession.campaign}</code>
                        </p>
                      </div>
                      <span className="badge badge--role">{activeSession.duration}</span>
                    </div>

                    <div className="replay-player">
                      <div className="replay-player__chrome">
                        <div className="replay-player__dots" aria-hidden="true">
                          <span />
                          <span />
                          <span />
                        </div>
                        <span className="replay-player__url">https://app.customer.dev{activeSession.route}</span>
                        <span className="replay-player__mask">rrweb · PII Masked</span>
                      </div>

                      <div className="replay-player__viewport">
                        <div className="replay-wire-card">
                          <div className="replay-wire-line--title" />
                          <div className="replay-wire-field">•••• •••• •••• 4242 (masked on client)</div>
                          <div className="replay-wire-actions">
                            <span className="replay-wire-btn">Pay Rp 450.000</span>
                            {activeSession.status === 'error' && scrubPct >= 45 && (
                              <span className="badge badge--exception">502 Bad Gateway</span>
                            )}
                          </div>
                        </div>

                        <div
                          className="replay-cursor"
                          style={{
                            left: `${Math.min(Math.max((activeSession.cursorX ?? 50) + (scrubPct - 50) * 0.25, 18), 84)}%`,
                            top: `${activeSession.cursorY ?? 50}%`,
                          }}
                          aria-hidden="true"
                        >
                          <span className="replay-cursor__ring" />
                        </div>
                      </div>

                      <div className="replay-scrubber">
                        <label htmlFor="replay-range" className="replay-scrubber__time">
                          {scrubPct}%
                        </label>
                        <input
                          id="replay-range"
                          type="range"
                          min={0}
                          max={100}
                          value={scrubPct}
                          onChange={(e) => setScrubPct(Number(e.target.value))}
                          className="replay-scrubber__input"
                        />
                        <span className="replay-scrubber__time">{activeSession.duration}</span>
                      </div>
                    </div>

                    <div className="dash-list">
                      {activeSession.steps.map((st) => (
                        <button
                          key={st.t + st.text}
                          type="button"
                          className={`dash-step-row${Math.abs(scrubPct - st.pct) <= 12 ? ' is-near' : ''}`}
                          onClick={() => setScrubPct(st.pct)}
                        >
                          <span className="dash-code">{st.t}</span>
                          <span
                            className={`badge ${
                              st.kind === 'error'
                                ? 'badge--exception'
                                : st.kind === 'warn'
                                  ? 'badge--console'
                                  : st.kind === 'net'
                                    ? 'badge--network'
                                    : 'badge--preview'
                            }`}
                          >
                            {st.kind}
                          </span>
                          <span className="dash-step-row__text">{st.text}</span>
                        </button>
                      ))}
                    </div>
                  </section>
                </div>
              )
            )}

            {/* ==================== EVENT LOG ==================== */}
            {section === 'logs' && (
              <section className="dash-card dash-card--compact">
                <div className="dash-card__head">
                  <div>
                    <h2 className="dash-card__title">Developer Event &amp; Error Stream</h2>
                    <p className="dash-card__desc">
                      Exceptions, HTTP 5xx spans, and console warnings linked directly to session replays.
                    </p>
                  </div>
                  <div className="dash-seg" role="group" aria-label="Filter log type">
                    {(
                      [
                        { id: 'all', label: 'All' },
                        { id: 'exception', label: 'Exceptions' },
                        { id: 'network', label: 'Network' },
                        { id: 'console', label: 'Console' },
                      ] as { id: LogKind; label: string }[]
                    ).map((f) => (
                      <button
                        key={f.id}
                        type="button"
                        className={`dash-seg__btn${logFilter === f.id ? ' is-active' : ''}`}
                        aria-pressed={logFilter === f.id}
                        onClick={() => setLogFilter(f.id)}
                      >
                        {f.label}
                      </button>
                    ))}
                  </div>
                </div>

                <div className="dash-list">
                  {filteredLogs.length === 0 ? (
                    <div style={{ padding: '3rem 1rem', textAlign: 'center', color: 'var(--color-fg-muted)' }}>
                      <p style={{ fontWeight: 500, margin: '0 0 0.5rem 0', color: 'var(--color-fg)' }}>
                        No telemetry logs found in PostgreSQL
                      </p>
                      <p style={{ fontSize: '0.8125rem', margin: '0 0 1.25rem 0' }}>
                        {!isDemo && project && (realStats?.events_count ?? 0) === 0
                          ? 'Send events using your SDK client key to POST /api/v1/ingest or send a live test event.'
                          : 'No events match the selected filter.'}
                      </p>
                      {project && !isDemo && (
                        <button
                          type="button"
                          className="dash-btn"
                          onClick={handleSendTestEvent}
                          disabled={sendingTest}
                        >
                          {sendingTest ? 'Ingesting...' : '⚡ Send Real Test Event to DB'}
                        </button>
                      )}
                    </div>
                  ) : (
                    filteredLogs.map((entry) => (
                      <div key={entry.id} className="dash-list__row">
                        <div className="dash-list__main">
                          <div className="dash-list__top">
                            <span className={`badge badge--${entry.kind}`}>{entry.kind}</span>
                            <span className="dash-code">{entry.occurrences}× events</span>
                            <span className="dash-code">{entry.time}</span>
                            <span className="dash-code">{entry.sessionId}</span>
                          </div>
                          <p className="dash-list__title">{entry.title}</p>
                          <p className="dash-list__sub dash-code">{entry.detail}</p>
                        </div>
                        <button
                          type="button"
                          className="dash-btn"
                          onClick={() => openReplay(entry.sessionId)}
                        >
                          Open Replay →
                        </button>
                      </div>
                    ))
                  )}
                </div>
              </section>
            )}

            {/* ==================== CAMPAIGNS ==================== */}
            {section === 'campaigns' && (
              <div className="dash-panel-stack">
                <section className="dash-card dash-card--compact">
                  <div className="dash-card__head">
                    <div>
                      <h2 className="dash-card__title">Acquisition &amp; Checkout Funnel</h2>
                      <p className="dash-card__desc">End-to-end conversion from landing view to paid activation.</p>
                    </div>
                  </div>

                  <div className="dash-funnel">
                    {FUNNEL_STEPS.map((f) => (
                      <div key={f.step} className="dash-funnel__row">
                        <div className="dash-funnel__meta">
                          <span>{f.step}</span>
                          <span className="dash-code">
                            {f.count} · {f.pct}%
                          </span>
                        </div>
                        <div className="dash-funnel__track">
                          <div className="dash-funnel__fill" style={{ width: `${Math.max(f.pct, 4)}%` }} />
                        </div>
                      </div>
                    ))}
                  </div>
                </section>

                <section className="dash-card dash-card--compact">
                  <div className="dash-card__head">
                    <div>
                      <h2 className="dash-card__title">UTM Campaign Performance</h2>
                      <p className="dash-card__desc">
                        Auto-captured UTM tags combined with custom <code>artracker.track()</code> events.
                      </p>
                    </div>
                  </div>

                  <div className="dash-table-wrap">
                    <table className="dash-table">
                      <thead>
                        <tr>
                          <th>Campaign</th>
                          <th>Source / Medium</th>
                          <th>Visitors</th>
                          <th>Signups</th>
                          <th>Checkouts</th>
                          <th>Conv. Rate</th>
                        </tr>
                      </thead>
                      <tbody>
                        {CAMPAIGNS.map((c) => (
                          <tr key={c.name}>
                            <td data-label="Campaign" className="dash-code">
                              {c.name}
                            </td>
                            <td data-label="Source / Medium">{c.source}</td>
                            <td data-label="Visitors">{c.visitors}</td>
                            <td data-label="Signups">{c.signups}</td>
                            <td data-label="Checkouts">{c.checkouts}</td>
                            <td data-label="Conv. Rate">
                              <span className="badge badge--role">{c.conv}</span>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </section>
              </div>
            )}

            {/* ==================== ALERTS ==================== */}
            {section === 'alerts' && (
              <section className="dash-card dash-card--compact">
                <div className="dash-card__head">
                  <div>
                    <h2 className="dash-card__title">Custom Alert Rules</h2>
                    <p className="dash-card__desc">
                      Automated anomaly &amp; error threshold evaluators routed to Slack, Discord, Email, or Webhook.
                    </p>
                  </div>
                </div>

                <div className="dash-list">
                  {alerts.map((a) => (
                    <div key={a.id} className="dash-list__row">
                      <div className="dash-list__main">
                        <div className="dash-list__top">
                          <span
                            className={`badge ${
                              a.status === 'firing'
                                ? 'badge--exception'
                                : a.status === 'armed'
                                  ? 'badge--active'
                                  : 'badge--revoked'
                            }`}
                          >
                            {a.status}
                          </span>
                          <span className="dash-code">{a.channel}</span>
                          <span className="dash-code">Last: {a.lastTriggered}</span>
                        </div>
                        <p className="dash-list__title">{a.name}</p>
                        <p className="dash-list__sub dash-code">{a.condition}</p>
                      </div>
                      <button type="button" className="dash-btn" onClick={() => toggleAlert(a.id)}>
                        {a.status === 'muted' ? 'Enable' : 'Mute'}
                      </button>
                    </div>
                  ))}
                </div>
              </section>
            )}
    </>
  )
}

export default function DashboardPage({ section }: { section: ConsoleSection }) {
  const [range, setRange] = useState<RangeKey>('24h')

  const toolbar = (
    <div className="dash-seg" role="group" aria-label="Time range">
      {(['24h', '7d', '30d'] as RangeKey[]).map((r) => (
        <button
          key={r}
          type="button"
          className={`dash-seg__btn${range === r ? ' is-active' : ''}`}
          aria-pressed={range === r}
          onClick={() => setRange(r)}
        >
          {r}
        </button>
      ))}
    </div>
  )

  return (
    <DashboardShell sectionTitle={SECTION_TITLES[section]} toolbar={toolbar} allowDemo>
      {({ activeOrg, project, isDemo, error }) => (
        <DashboardInner
          section={section}
          range={range}
          activeOrg={activeOrg}
          project={project}
          isDemo={isDemo}
          error={error}
        />
      )}
    </DashboardShell>
  )
}
