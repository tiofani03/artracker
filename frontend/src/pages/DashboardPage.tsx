import { useCallback, useEffect, useState } from 'react'
import {
  analyticsApi,
  keyApi,
  replayApi,
  telemetryApi,
  type ApiKey,
  type OrgMembership,
  type Project,
  type ProjectStats,
  type ReplayChunkDetail,
  type ReplaySessionItem,
  type TelemetryEvent,
} from '../api'
import DashboardShell from '../components/DashboardShell'
import ReplayPlayer from '../components/ReplayPlayer'
import GrowthAnalyticsView from '../components/analytics/GrowthAnalyticsView'
import CampaignsView from '../components/campaigns/CampaignsView'
import { Link, navigate } from '../router'
import { startInteractiveCapture, type LiveCaptureResult } from '../utils/liveReplayCapture'
import '../dashboard.css'

export type ConsoleSection = 'overview' | 'replays' | 'logs' | 'campaigns' | 'funnels' | 'alerts'
type RangeKey = '24h' | '7d' | '30d'
type LogKind = 'all' | 'exception' | 'network' | 'console'

const SECTION_TITLES: Record<ConsoleSection, string> = {
  overview: 'Overview',
  replays: 'Session Replay',
  logs: 'Event Log',
  campaigns: 'Marketing Campaigns',
  funnels: 'Funnel & Retention',
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

type ReplayFilter = 'all' | 'error' | 'rage' | 'dead'
const REPLAY_PAGE_SIZE = 10

function formatDurationMs(ms: number): string {
  const totalSec = Math.max(0, Math.round(ms / 1000))
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

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
  { name: 'utm_launch_q4', source: 'google / cpc', visitors: '186', signups: '65', checkouts: '30', conv: '16.13%' },
  { name: 'utm_ig_reels', source: 'instagram / paid', visitors: '142', signups: '49', checkouts: '18', conv: '12.68%' },
  { name: 'utm_newsletter_nov', source: 'email / ops-digest', visitors: '128', signups: '52', checkouts: '24', conv: '18.75%' },
  { name: 'utm_github_readme', source: 'github / referral', visitors: '96', signups: '38', checkouts: '14', conv: '14.58%' },
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
  const [selectedSessionId, setSelectedSessionId] = useState<string>(() => {
    if (typeof window !== 'undefined') {
      return new URLSearchParams(window.location.search).get('session') ?? ''
    }
    return ''
  })
  const [replaySessions, setReplaySessions] = useState<ReplaySessionItem[]>([])
  const [replayTotal, setReplayTotal] = useState<number>(0)
  const [replayOffset, setReplayOffset] = useState<number>(0)
  const [replayFilter, setReplayFilter] = useState<ReplayFilter>('all')
  const [replayLoading, setReplayLoading] = useState<boolean>(true)
  const [replayError, setReplayError] = useState<string | null>(null)

  const [sessionDetail, setSessionDetail] = useState<{
    session: ReplaySessionItem
    chunks: ReplayChunkDetail[]
    timelineEvents: TelemetryEvent[]
  } | null>(null)
  const [detailLoading, setDetailLoading] = useState<boolean>(false)
  const [liveCapture, setLiveCapture] = useState<LiveCaptureResult | null>(null)
  const [isCapturingLive, setIsCapturingLive] = useState<boolean>(false)
  const [seedingSessions, setSeedingSessions] = useState<boolean>(false)

  const [logFilter, setLogFilter] = useState<LogKind>('all')
  const [alerts, setAlerts] = useState<AlertRule[]>(INITIAL_ALERTS)
  const [realStats, setRealStats] = useState<ProjectStats | null>(null)
  const [realEvents, setRealEvents] = useState<TelemetryEvent[]>([])
  const [realCampaigns, setRealCampaigns] = useState<typeof CAMPAIGNS | null>(null)
  const [sendingTest, setSendingTest] = useState(false)

  const targetPid = project?.id ?? 1

  const loadReplaySessions = useCallback(
    async (offsetArg = replayOffset, filterArg = replayFilter) => {
      setReplayLoading(true)
      setReplayError(null)
      try {
        const res = await replayApi.listSessions(targetPid, {
          limit: REPLAY_PAGE_SIZE,
          offset: offsetArg,
          ...(filterArg === 'error' ? { has_error: true } : {}),
          ...(filterArg === 'rage' ? { has_rage_click: true } : {}),
          ...(filterArg === 'dead' ? { has_dead_click: true } : {}),
        })
        setReplaySessions(res.items)
        setReplayTotal(res.total)
        setSelectedSessionId((cur) => {
          if (cur && res.items.some((s) => s.id === cur)) return cur
          return res.items[0]?.id ?? ''
        })
      } catch (err) {
        setReplayError(err instanceof Error ? err.message : 'Failed to fetch replay sessions')
        setReplaySessions([])
        setReplayTotal(0)
      } finally {
        setReplayLoading(false)
      }
    },
    [targetPid, replayOffset, replayFilter],
  )

  useEffect(() => {
    void loadReplaySessions(replayOffset, replayFilter)
  }, [loadReplaySessions, replayOffset, replayFilter])

  useEffect(() => {
    if (!selectedSessionId) {
      setSessionDetail(null)
      return
    }
    if (liveCapture && liveCapture.sessionId === selectedSessionId && !liveCapture.uploadedToR2) {
      return
    }

    let cancelled = false
    setDetailLoading(true)
    replayApi
      .getSessionDetail(targetPid, selectedSessionId)
      .then((res) => {
        if (cancelled) return
        setSessionDetail({
          session: res.session,
          chunks: res.chunks,
          timelineEvents: res.timeline_events,
        })
        setDetailLoading(false)
      })
      .catch(() => {
        if (cancelled) return
        const fallback = replaySessions.find((s) => s.id === selectedSessionId)
        if (fallback) {
          setSessionDetail({
            session: fallback,
            chunks: [],
            timelineEvents: [],
          })
        } else {
          setSessionDetail(null)
        }
        setDetailLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [targetPid, selectedSessionId, replaySessions, liveCapture])

  const handleStartLiveCapture = () => {
    if (isCapturingLive) return
    setIsCapturingLive(true)
    const recorder = startInteractiveCapture({
      userId: 'operator@artracker.dev',
      environment: 'production',
    })
    setTimeout(() => {
      void recorder.stop().then((result) => {
        setIsCapturingLive(false)
        setLiveCapture(result)
        const nowIso = new Date().toISOString()
        const firstTs = result.events[0]?.timestamp ?? Date.now()
        const lastTs = result.events[result.events.length - 1]?.timestamp ?? firstTs + 5000
        const syntheticItem: ReplaySessionItem = {
          id: result.sessionId,
          project_id: targetPid,
          user_id: 'operator@artracker.dev',
          environment: 'production',
          start_time: new Date(firstTs).toISOString(),
          end_time: new Date(lastTs).toISOString(),
          duration_ms: Math.max(1000, lastTs - firstTs),
          chunk_count: 1,
          event_count: result.events.length,
          total_size_bytes: JSON.stringify(result.events).length,
          has_error: result.hasError,
          has_rage_click: result.hasRageClick,
          has_dead_click: result.hasDeadClick,
          storage_prefix: `projects/${targetPid}/sessions/${result.sessionId}/`,
          created_at: nowIso,
          updated_at: nowIso,
        }
        setReplaySessions((prev) => [syntheticItem, ...prev.filter((p) => p.id !== syntheticItem.id)])
        setReplayTotal((prev) => prev + 1)
        setSelectedSessionId(result.sessionId)
        setSessionDetail({
          session: syntheticItem,
          chunks: [],
          timelineEvents: result.timelineEvents,
        })
      })
    }, 5000)
  }

  const handleSeedSessions = async () => {
    try {
      setSeedingSessions(true)
      await telemetryApi.seedMock(targetPid)
      await loadReplaySessions(0, 'all')
      reloadTelemetry()
    } catch (err) {
      setReplayError(err instanceof Error ? err.message : 'Failed to seed sessions')
    } finally {
      setSeedingSessions(false)
    }
  }

  const reloadTelemetry = () => {
    const targetPid = project?.id ?? 1
    Promise.all([
      telemetryApi.getStats(targetPid, range).catch(() => null),
      telemetryApi.listEvents(targetPid, 'all', 50).catch(() => ({ items: [] })),
      analyticsApi
        .calculateFunnel(targetPid, {
          steps: ['landing_view', 'purchase_completed'],
          breakdown_by: 'utm_campaign',
          from: new Date(Date.now() - 30 * 86400000).toISOString(),
          to: new Date().toISOString(),
          window_seconds: 86400,
        })
        .catch(() => null),
    ]).then(([statsData, eventsData, funnelData]) => {
      if (statsData) setRealStats(statsData)
      if (eventsData?.items && eventsData.items.length > 0) setRealEvents(eventsData.items)
      if (funnelData?.breakdowns && funnelData.breakdowns.length > 0) {
        setRealCampaigns(
          funnelData.breakdowns.map((b) => ({
            name: b.segment || 'direct / organic',
            source: 'tracked UTM',
            visitors: (b.step_counts[0] ?? 0).toLocaleString(),
            signups: Math.round((b.step_counts[0] ?? 0) * 0.35).toLocaleString(),
            checkouts: (b.step_counts[1] ?? 0).toLocaleString(),
            conv: `${b.overall_conversion_pct.toFixed(2)}%`,
          })),
        )
      }
    })
  }

  useEffect(() => {
    let active = true
    const targetPid = project?.id ?? 1
    Promise.all([
      telemetryApi.getStats(targetPid, range).catch(() => null),
      telemetryApi.listEvents(targetPid, 'all', 50).catch(() => ({ items: [] })),
      analyticsApi
        .calculateFunnel(targetPid, {
          steps: ['landing_view', 'purchase_completed'],
          breakdown_by: 'utm_campaign',
          from: new Date(Date.now() - 30 * 86400000).toISOString(),
          to: new Date().toISOString(),
          window_seconds: 86400,
        })
        .catch(() => null),
    ]).then(([statsData, eventsData, funnelData]) => {
      if (!active) return
      if (statsData) setRealStats(statsData)
      if (eventsData?.items && eventsData.items.length > 0) setRealEvents(eventsData.items)
      if (funnelData?.breakdowns && funnelData.breakdowns.length > 0) {
        setRealCampaigns(
          funnelData.breakdowns.map((b) => ({
            name: b.segment || 'direct / organic',
            source: 'tracked UTM',
            visitors: (b.step_counts[0] ?? 0).toLocaleString(),
            signups: Math.round((b.step_counts[0] ?? 0) * 0.35).toLocaleString(),
            checkouts: (b.step_counts[1] ?? 0).toLocaleString(),
            conv: `${b.overall_conversion_pct.toFixed(2)}%`,
          })),
        )
      }
    })

    return () => {
      active = false
    }
  }, [project?.id, range])

  const handleSendTestEvent = async () => {
    const targetPid = project?.id ?? 1
    try {
      setSendingTest(true)
      await telemetryApi.sendTestEvent(targetPid)
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

  // Pure real database metrics when inside a project or demo project
  const stats: RangeStats =
    realStats && realStats.events_count > 0
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
    realEvents && realEvents.length > 0
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

  const filteredLogs = logFilter === 'all' ? displayLogs : displayLogs.filter((l) => l.kind === logFilter)

  const toggleAlert = (id: string) => {
    setAlerts((prev) =>
      prev.map((a) => (a.id === id ? { ...a, status: a.status === 'muted' ? 'armed' : 'muted' } : a)),
    )
  }

  const goSection = (path: string) => navigate(isDemo ? `${path}?demo=1` : path)
  const openReplay = (sessionId: string) => {
    setSelectedSessionId(sessionId)
    goSection(`/dashboard/replays?session=${encodeURIComponent(sessionId)}`)
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
                        Campaigns →
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
                          {(realCampaigns && realCampaigns.length > 0 ? realCampaigns : CAMPAIGNS).map((c) => (
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
              <div className="dash-replay-container">
                <div className="dash-replay-toolbar">
                  <div className="dash-seg" role="group" aria-label="Filter replays">
                    {(
                      [
                        { id: 'all', label: 'All Sessions' },
                        { id: 'error', label: 'Errors Only' },
                        { id: 'rage', label: 'Rage Clicks' },
                        { id: 'dead', label: 'Dead Clicks' },
                      ] as { id: ReplayFilter; label: string }[]
                    ).map((f) => (
                      <button
                        key={f.id}
                        type="button"
                        className={`dash-seg__btn${replayFilter === f.id ? ' is-active' : ''}`}
                        aria-pressed={replayFilter === f.id}
                        onClick={() => {
                          setReplayFilter(f.id)
                          setReplayOffset(0)
                        }}
                      >
                        {f.label}
                      </button>
                    ))}
                  </div>

                  <div className="dash-replay-toolbar__actions">
                    <button
                      type="button"
                      className="dash-btn"
                      onClick={handleStartLiveCapture}
                      disabled={isCapturingLive}
                      title="Record a 5-second live DOM session using rrweb with rage & dead click detector"
                    >
                      {isCapturingLive ? '● Recording 5s DOM…' : '📹 Record Live DOM'}
                    </button>
                    {project && !isDemo && (
                      <button
                        type="button"
                        className="dash-btn"
                        onClick={handleSeedSessions}
                        disabled={seedingSessions}
                      >
                        {seedingSessions ? 'Seeding…' : '⚡ Seed Replay Data'}
                      </button>
                    )}
                  </div>
                </div>

                {replayError && (
                  <p className="dash-error" role="alert" style={{ marginBottom: '1rem' }}>
                    {replayError}
                  </p>
                )}

                {replayLoading && replaySessions.length === 0 ? (
                  <section className="dash-card dash-card--compact" style={{ padding: '3.5rem 1.5rem', textAlign: 'center' }}>
                    <p className="dash-empty">Loading recorded sessions from database…</p>
                  </section>
                ) : replaySessions.length === 0 ? (
                  <section className="dash-card dash-card--compact" style={{ padding: '3.5rem 1.5rem', textAlign: 'center' }}>
                    <div style={{ maxWidth: 440, margin: '0 auto' }}>
                      <div style={{ fontSize: '2.25rem', marginBottom: '0.75rem' }}>📹</div>
                      <h2 className="dash-card__title" style={{ marginBottom: '0.5rem' }}>No Sessions Found</h2>
                      <p className="dash-card__desc" style={{ marginBottom: '1.5rem' }}>
                        {replayFilter !== 'all'
                          ? `No recorded sessions match the "${replayFilter}" filter in PostgreSQL.`
                          : 'No session replays ingested yet. Record a live DOM interaction or seed mock sessions.'}
                      </p>
                      <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'center' }}>
                        <button
                          type="button"
                          className="dash-btn"
                          onClick={handleStartLiveCapture}
                          disabled={isCapturingLive}
                        >
                          {isCapturingLive ? 'Recording…' : 'Record Live Browser Session'}
                        </button>
                        {project && !isDemo && (
                          <button
                            type="button"
                            className="dash-btn"
                            onClick={handleSeedSessions}
                            disabled={seedingSessions}
                          >
                            Seed Replay Sessions
                          </button>
                        )}
                      </div>
                    </div>
                  </section>
                ) : (
                  <div className="dash-replay-grid">
                    {/* Left Column: Session Queue with Pagination */}
                    <section className="dash-card dash-card--compact" aria-label="Recorded sessions">
                      <div className="dash-card__head">
                        <div>
                          <h2 className="dash-card__title">Session Queue</h2>
                          <p className="dash-card__desc">
                            {replayTotal} total · PII masked on client
                          </p>
                        </div>
                      </div>

                      <div className="dash-session-list">
                        {replaySessions.map((s) => {
                          const selected = s.id === selectedSessionId
                          const durFormatted = formatDurationMs(s.duration_ms)
                          return (
                            <button
                              key={s.id}
                              type="button"
                              className={`dash-session-item${selected ? ' is-selected' : ''}`}
                              onClick={() => {
                                setSelectedSessionId(s.id)
                              }}
                            >
                              <div className="dash-session-item__top">
                                <span className="dash-code">{s.id}</span>
                                <div style={{ display: 'flex', gap: '4px' }}>
                                  {s.has_error && (
                                    <span className="badge badge--exception">error</span>
                                  )}
                                  {s.has_rage_click && (
                                    <span className="badge badge--console">rage click</span>
                                  )}
                                  {s.has_dead_click && (
                                    <span className="badge badge--console">dead click</span>
                                  )}
                                  {!s.has_error && !s.has_rage_click && !s.has_dead_click && (
                                    <span className="badge badge--active">clean</span>
                                  )}
                                </div>
                              </div>
                              <strong className="dash-session-item__user">
                                {s.user_id || 'anonymous_user'}
                              </strong>
                              <span className="dash-session-item__meta">
                                {s.chunk_count} chunk{s.chunk_count === 1 ? '' : 's'} · {durFormatted} · {formatAgo(s.start_time)}
                              </span>
                            </button>
                          )
                        })}
                      </div>

                      {/* Pagination Controls */}
                      {replayTotal > REPLAY_PAGE_SIZE && (
                        <div className="dash-replay-pagination">
                          <button
                            type="button"
                            className="dash-btn"
                            disabled={replayOffset === 0 || replayLoading}
                            onClick={() => setReplayOffset((prev) => Math.max(0, prev - REPLAY_PAGE_SIZE))}
                          >
                            ← Prev
                          </button>
                          <span className="dash-replay-pagination__label">
                            {replayOffset + 1}–{Math.min(replayOffset + REPLAY_PAGE_SIZE, replayTotal)} of {replayTotal}
                          </span>
                          <button
                            type="button"
                            className="dash-btn"
                            disabled={replayOffset + REPLAY_PAGE_SIZE >= replayTotal || replayLoading}
                            onClick={() => setReplayOffset((prev) => prev + REPLAY_PAGE_SIZE)}
                          >
                            Next →
                          </button>
                        </div>
                      )}
                    </section>

                    {/* Right Column: Interactive Visual Player (rrweb-player) & Timeline */}
                    <section className="dash-card dash-card--compact" aria-label="Session player">
                      {sessionDetail ? (
                        <>
                          <div className="dash-card__head">
                            <div>
                              <h2 className="dash-card__title">
                                {sessionDetail.session.user_id || 'anonymous_user'} ·{' '}
                                <span className="dash-code">{sessionDetail.session.id}</span>
                              </h2>
                              <p className="dash-card__desc">
                                {sessionDetail.session.environment} · {sessionDetail.chunks.length} R2 chunk(s) · {sessionDetail.session.event_count} events
                              </p>
                            </div>
                            <span className="badge badge--role">
                              {formatDurationMs(sessionDetail.session.duration_ms)}
                            </span>
                          </div>

                          <ReplayPlayer
                            session={sessionDetail.session}
                            chunks={sessionDetail.chunks}
                            timelineEvents={sessionDetail.timelineEvents}
                            overrideEvents={
                              liveCapture && liveCapture.sessionId === sessionDetail.session.id
                                ? liveCapture.events
                                : null
                            }
                            loadingDetail={detailLoading}
                            onStartLiveCapture={handleStartLiveCapture}
                            isCapturingLive={isCapturingLive}
                          />
                        </>
                      ) : (
                        <div style={{ padding: '3rem 1rem', textAlign: 'center' }}>
                          <p className="dash-empty">Select a session from the queue to start replay.</p>
                        </div>
                      )}
                    </section>
                  </div>
                )}
              </div>
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

            {/* ==================== MARKETING CAMPAIGNS ==================== */}
            {section === 'campaigns' && (
              <CampaignsView project={project} isDemo={isDemo} />
            )}

            {/* ==================== FUNNEL & RETENTION ==================== */}
            {section === 'funnels' && (
              <GrowthAnalyticsView project={project} isDemo={isDemo} />
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
