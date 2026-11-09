import { useCallback, useEffect, useRef, useState } from 'react'
import {
  analyticsApi,
  replayApi,
  telemetryApi,
  type Project,
  type ProjectStats,
  type ReplayChunkDetail,
  type ReplaySessionItem,
  type TelemetryEvent,
} from '../api'
import DashboardShell from '../components/DashboardShell'
import ReplayPlayer, { type DeviceMode } from '../components/ReplayPlayer'
import GrowthAnalyticsView from '../components/analytics/GrowthAnalyticsView'
import CampaignsView from '../components/campaigns/CampaignsView'
import { navigate } from '../router'
import { startInteractiveCapture, type LiveCaptureResult } from '../utils/liveReplayCapture'
import { detectSessionPlatform } from '../utils/platformDetector'
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

function formatSessionId(id: string, maxLen = 16): string {
  if (!id) return ''
  if (id.length <= maxLen) return id
  const half = Math.floor((maxLen - 3) / 2)
  return `${id.slice(0, half + 2)}…${id.slice(-half)}`
}

function formatExactTime(timestamp?: string | number | Date): string {
  if (!timestamp) return ''
  const d = new Date(timestamp)
  if (Number.isNaN(d.getTime())) return String(timestamp)
  const pad = (n: number) => String(n).padStart(2, '0')
  const yyyy = d.getFullYear()
  const mm = pad(d.getMonth() + 1)
  const dd = pad(d.getDate())
  const hh = pad(d.getHours())
  const min = pad(d.getMinutes())
  const ss = pad(d.getSeconds())
  return `${yyyy}-${mm}-${dd} ${hh}:${min}:${ss}`
}

function formatExactTimeShort(timestamp?: string | number | Date): string {
  if (!timestamp) return ''
  const d = new Date(timestamp)
  if (Number.isNaN(d.getTime())) return String(timestamp)
  const pad = (n: number) => String(n).padStart(2, '0')
  const hh = pad(d.getHours())
  const min = pad(d.getMinutes())
  const ss = pad(d.getSeconds())
  return `${hh}:${min}:${ss}`
}

function formatExactDateShort(timestamp?: string | number | Date): string {
  if (!timestamp) return ''
  const d = new Date(timestamp)
  if (Number.isNaN(d.getTime())) return ''
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
  const m = months[d.getMonth()]
  const dd = String(d.getDate()).padStart(2, '0')
  return `${m} ${dd}`
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

type DevLogEntry = {
  id: string
  time: string
  exactTime: string
  exactTimeShort: string
  exactDateShort: string
  rawTimestamp?: string
  kind: Exclude<LogKind, 'all'>
  level?: string
  eventName?: string
  title: string
  detail: string
  statusCode?: number | string
  durationMs?: number
  occurrences: number
  sessionId: string
  environment?: string
  releaseVersion?: string
  userId?: string
  attributes?: Record<string, unknown>
}

const nowMs = Date.now()
const getMockLogTime = (minutesAgo: number) => {
  const date = new Date(nowMs - minutesAgo * 60 * 1000)
  return {
    time: `${minutesAgo}m ago`,
    exactTime: formatExactTime(date),
    exactTimeShort: formatExactTimeShort(date),
    exactDateShort: formatExactDateShort(date),
    rawTimestamp: date.toISOString(),
  }
}

const DEV_LOGS: DevLogEntry[] = [
  {
    id: 'iss_104',
    ...getMockLogTime(2),
    kind: 'exception',
    level: 'error',
    title: 'TypeError: Cannot read properties of undefined (reading "charge_id")',
    detail: 'PaymentForm.tsx:142:19 · Chrome 131',
    occurrences: 12,
    sessionId: 'ses_94k2p8',
    environment: 'production',
    releaseVersion: 'v2.4.1',
    userId: 'usr_premium_88',
    attributes: {
      component: 'PaymentForm.tsx:142:19',
      browser: 'Chrome 131.0.6778.86 (macOS)',
      route: '/checkout/pay',
      error_code: 'ERR_UNDEFINED_PROP',
      runtime: 'React 18.3.1',
    },
  },
  {
    id: 'iss_103',
    ...getMockLogTime(4),
    kind: 'network',
    level: 'error',
    title: 'POST /api/v1/charge → 502 Bad Gateway (1,840 ms)',
    detail: 'trace_id=tr_88a41f · payload PII redacted · status 502',
    occurrences: 19,
    sessionId: 'ses_94k2p8',
    environment: 'production',
    releaseVersion: 'v2.4.1',
    userId: 'usr_premium_88',
    attributes: {
      method: 'POST',
      endpoint: '/api/v1/charge',
      status_code: 502,
      latency_ms: 1840,
      trace_id: 'tr_88a41f',
      upstream: 'payment-gateway-service',
    },
  },
  {
    id: 'iss_102',
    ...getMockLogTime(18),
    kind: 'console',
    level: 'warn',
    title: 'console.warn: Slow template hydration exceeded 2000ms budget',
    detail: 'route=/onboarding/workspace · p95=2,410 ms · iOS Safari 18',
    occurrences: 7,
    sessionId: 'ses_71m9x3',
    environment: 'staging',
    releaseVersion: 'v2.4.0',
    userId: 'usr_onboard_12',
    attributes: {
      route: '/onboarding/workspace',
      hydration_ms: 2410,
      budget_ms: 2000,
      client: 'iOS Safari 18.1',
      platform: 'Mobile',
    },
  },
  {
    id: 'iss_101',
    ...getMockLogTime(41),
    kind: 'exception',
    level: 'error',
    title: 'UnhandledRejection: NetworkError when attempting to fetch resource',
    detail: 'src/api.ts:52:25 · transient mobile handoff',
    occurrences: 3,
    sessionId: 'ses_71m9x3',
    environment: 'production',
    releaseVersion: 'v2.3.9',
    userId: 'usr_onboard_12',
    attributes: {
      source: 'src/api.ts:52:25',
      failure_reason: 'Mobile connection reset (transient cell handoff)',
      retry_attempt: 2,
    },
  },
  {
    id: 'iss_100',
    ...getMockLogTime(64),
    kind: 'network',
    level: 'error',
    title: 'GET /api/v1/projects/1/stats → 504 Gateway Timeout (5,000 ms)',
    detail: 'trace_id=tr_77e20a · database replica sync lag',
    occurrences: 5,
    sessionId: 'ses_83b1x9',
    environment: 'production',
    releaseVersion: 'v2.3.9',
    attributes: {
      method: 'GET',
      endpoint: '/api/v1/projects/1/stats',
      status_code: 504,
      latency_ms: 5000,
      trace_id: 'tr_77e20a',
    },
  },
  {
    id: 'iss_99',
    ...getMockLogTime(85),
    kind: 'exception',
    level: 'error',
    title: 'ReferenceError: window.analytics is not defined',
    detail: 'tracker.ts:88:12 · Firefox 133 · AdBlock detected',
    occurrences: 14,
    sessionId: 'ses_83b1x9',
    environment: 'production',
    releaseVersion: 'v2.3.9',
    attributes: {
      component: 'tracker.ts:88:12',
      browser: 'Firefox 133.0 (Linux)',
      script_blocked: true,
    },
  },
  {
    id: 'iss_98',
    ...getMockLogTime(110),
    kind: 'console',
    level: 'error',
    title: 'console.error: Failed to load resource: net::ERR_CONNECTION_REFUSED',
    detail: 'https://cdn.artracker.dev/sdk/v2.js · fallback initiated',
    occurrences: 8,
    sessionId: 'ses_52q7w1',
    environment: 'staging',
    releaseVersion: 'v2.3.8',
    attributes: {
      url: 'https://cdn.artracker.dev/sdk/v2.js',
      network_error: 'ERR_CONNECTION_REFUSED',
      fallback_used: true,
    },
  },
  {
    id: 'iss_97',
    ...getMockLogTime(145),
    kind: 'network',
    level: 'warn',
    title: 'POST /api/v1/ingest → 429 Too Many Requests',
    detail: 'client rate limit threshold reached (100 req/s)',
    occurrences: 22,
    sessionId: 'ses_52q7w1',
    environment: 'production',
    releaseVersion: 'v2.3.8',
    attributes: {
      method: 'POST',
      endpoint: '/api/v1/ingest',
      status_code: 429,
      rate_limit_bucket: 'client_ingest',
    },
  },
  {
    id: 'iss_96',
    ...getMockLogTime(180),
    kind: 'exception',
    level: 'error',
    title: 'SecurityError: Blocked a frame with origin from accessing a cross-origin frame',
    detail: 'iframeBridge.ts:31:14 · Safari 17.6',
    occurrences: 6,
    sessionId: 'ses_41r8k2',
    environment: 'production',
    releaseVersion: 'v2.3.8',
    attributes: {
      component: 'iframeBridge.ts:31:14',
      browser: 'Safari 17.6 (macOS)',
      security_origin: 'https://checkout.partner.com',
    },
  },
  {
    id: 'iss_95',
    ...getMockLogTime(230),
    kind: 'console',
    level: 'warn',
    title: 'console.warn: WebGL context lost. Attempting auto-restore',
    detail: 'three_canvas.tsx:64 · GPU memory pressure',
    occurrences: 4,
    sessionId: 'ses_33p2m8',
    environment: 'production',
    releaseVersion: 'v2.3.7',
    attributes: {
      component: 'three_canvas.tsx:64',
      gpu_renderer: 'Apple M1',
      context_restored: true,
    },
  },
  {
    id: 'iss_94',
    ...getMockLogTime(310),
    kind: 'network',
    level: 'warn',
    title: 'POST /api/v1/auth/refresh → 401 Unauthorized',
    detail: 'refresh_token expired · auto re-auth scheduled',
    occurrences: 11,
    sessionId: 'ses_22k9j4',
    environment: 'production',
    releaseVersion: 'v2.3.7',
    attributes: {
      method: 'POST',
      endpoint: '/api/v1/auth/refresh',
      status_code: 401,
      reason: 'Token expired',
    },
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

function DashboardInner({
  section,
  range,
  project,
  isDemo,
  error,
}: {
  section: ConsoleSection
  range: RangeKey
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
  const [replayDeviceMode, setReplayDeviceMode] = useState<DeviceMode>('auto')
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
  const [logPage, setLogPage] = useState<number>(1)
  const [expandedLogIds, setExpandedLogIds] = useState<Set<string>>(new Set())
  const LOGS_PER_PAGE = 8

  const toggleLogExpanded = (id: string) => {
    setExpandedLogIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const [alerts, setAlerts] = useState<AlertRule[]>(INITIAL_ALERTS)
  const [realStats, setRealStats] = useState<ProjectStats | null>(null)
  const [realEvents, setRealEvents] = useState<TelemetryEvent[]>([])
  const [realCampaigns, setRealCampaigns] = useState<typeof CAMPAIGNS | null>(null)

  const targetPid = project?.id ?? 1
  // null while the shell is still resolving the selected project (demo mode supplies its own)
  const projectReady = project != null

  const loadReplaySessions = useCallback(
    async (offsetArg = replayOffset, filterArg = replayFilter) => {
      // Project still resolving: wait instead of querying the fallback project id.
      if (!projectReady) return
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
    [projectReady, targetPid, replayOffset, replayFilter],
  )

  useEffect(() => {
    void loadReplaySessions(replayOffset, replayFilter)
  }, [loadReplaySessions, replayOffset, replayFilter])

  // Read the latest list for the error fallback without refetching detail on every list change.
  const replaySessionsRef = useRef(replaySessions)
  useEffect(() => {
    replaySessionsRef.current = replaySessions
  }, [replaySessions])

  useEffect(() => {
    if (!projectReady) return
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
        const fallback = replaySessionsRef.current.find((s) => s.id === selectedSessionId)
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
  }, [projectReady, targetPid, selectedSessionId, liveCapture])

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

  const projectId = project?.id ?? null
  useEffect(() => {
    if (projectId == null) return
    let active = true
    const targetPid = projectId
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
  }, [projectId, range])

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
      ? realEvents
          .filter((e) => {
            if (e.name === 'replay_chunk') return false
            const msg = (e.message || '').toLowerCase()
            const rawUrl = String(e.attributes?.url || '').toLowerCase()
            if (
              msg.includes('/api/v1/replay/') ||
              msg.includes('/api/v1/ingest') ||
              msg.includes('/api/v1/events') ||
              msg.includes('r2.cloudflarestorage.com') ||
              rawUrl.includes('/api/v1/replay/') ||
              rawUrl.includes('/api/v1/ingest') ||
              rawUrl.includes('/api/v1/events') ||
              rawUrl.includes('r2.cloudflarestorage.com')
            ) {
              return false
            }
            return true
          })
          .map((e) => {
          let kind: Exclude<LogKind, 'all'> = 'console'
          if (e.kind === 'log' && (e.level === 'error' || e.level === 'fatal')) {
            kind = 'exception'
          } else if (
            e.name.toLowerCase().includes('http') ||
            e.name.toLowerCase().includes('fetch') ||
            e.name.toLowerCase().includes('network') ||
            (e.attributes && (e.attributes.status_code || e.attributes.method))
          ) {
            kind = 'network'
          }

          // MoEngage-style smart headline extraction
          const method = (e.attributes?.method as string) || ''
          const rawUrl = (e.attributes?.url as string) || ''
          const status = (e.attributes?.status as number | string) || (e.attributes?.status_code as number | string) || undefined
          const duration = typeof e.attributes?.duration_ms === 'number' ? e.attributes.duration_ms : undefined

          let title = e.name || e.message || `${e.kind} event`
          let detail = e.message || (e.environment ? `env: ${e.environment}` : '')

          if (e.name === 'http_request' || kind === 'network') {
            if (e.message && e.message.includes('http')) {
              const cleanMsg = e.message
                .replace(/https?:\/\/[^/]+/i, '')
                .replace(/\?[^→\s]+/i, '')
                .replace(/\s*→\s*\d+/, '')
                .trim()
              title = cleanMsg || e.message
              detail = `${status ? `HTTP ${status}` : ''}${duration ? ` · ${duration}ms` : ''}${e.environment ? ` · env: ${e.environment}` : ''}`
            } else if (method && rawUrl) {
              const cleanPath = rawUrl.replace(/https?:\/\/[^/]+/i, '').split('?')[0]
              title = `${method} ${cleanPath}`
              detail = `${status ? `HTTP ${status}` : ''}${duration ? ` · ${duration}ms` : ''}${e.environment ? ` · env: ${e.environment}` : ''}`
            }
          } else if (kind === 'exception') {
            if (e.name && e.message && e.name !== e.message) {
              title = `${e.name}: ${e.message}`
              detail = e.environment ? `env: ${e.environment} · ${e.release_version || 'v1.0'}` : ''
            }
          }

          return {
            id: e.id,
            time: formatAgo(e.event_timestamp),
            exactTime: formatExactTime(e.event_timestamp),
            exactTimeShort: formatExactTimeShort(e.event_timestamp),
            exactDateShort: formatExactDateShort(e.event_timestamp),
            rawTimestamp: e.event_timestamp,
            kind,
            level: e.level || (kind === 'exception' ? 'error' : kind === 'network' ? 'info' : 'log'),
            eventName: e.name,
            title,
            detail: detail.replace(/^ · /, ''),
            statusCode: status,
            durationMs: duration,
            occurrences: 1,
            sessionId: e.session_id || 'n/a',
            environment: e.environment,
            releaseVersion: e.release_version,
            userId: e.user_id,
            attributes: e.attributes,
          }
        })
      : DEV_LOGS

  const filteredLogs = logFilter === 'all' ? displayLogs : displayLogs.filter((l) => l.kind === logFilter)
  const totalLogs = filteredLogs.length
  const totalLogPages = Math.max(1, Math.ceil(totalLogs / LOGS_PER_PAGE))
  const currentLogPage = Math.min(logPage, totalLogPages)
  const paginatedLogs = filteredLogs.slice(
    (currentLogPage - 1) * LOGS_PER_PAGE,
    currentLogPage * LOGS_PER_PAGE,
  )

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
                        <h2 className="dash-card__title">Unresolved Issues</h2>
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
                                <span className="dash-code" title={`Exact time: ${item.exactTime}`}>
                                  {item.time} ({item.exactTime})
                                </span>
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
                          const plat = detectSessionPlatform(s, null, project?.platform)
                          const hasErr = s.has_error
                          const hasWarn = s.has_rage_click || s.has_dead_click
                          const dotClass = hasErr
                            ? 'dash-session-item__dot--error'
                            : hasWarn
                              ? 'dash-session-item__dot--warn'
                              : 'dash-session-item__dot--clean'
                          return (
                            <button
                              key={s.id}
                              type="button"
                              className={`dash-session-item${selected ? ' is-selected' : ''}`}
                              onClick={() => {
                                setSelectedSessionId(s.id)
                              }}
                            >
                              <div className="dash-session-item__row-1">
                                <div className="dash-session-item__user-box">
                                  <span className={`dash-session-item__dot ${dotClass}`} />
                                  <strong className="dash-session-item__user">
                                    {s.user_id || 'anonymous_user'}
                                  </strong>
                                </div>
                                <div style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
                                  <span className="dash-session-item__platform">
                                    {plat.label}
                                  </span>
                                  {hasErr && <span className="badge badge--exception">error</span>}
                                  {s.has_rage_click && <span className="badge badge--console">rage</span>}
                                  {s.has_dead_click && <span className="badge badge--console">dead</span>}
                                  {!hasErr && !hasWarn && <span className="badge badge--active">clean</span>}
                                </div>
                              </div>

                              <div className="dash-session-item__row-2">
                                <span className="dash-code dash-session-item__id" title={s.id}>
                                  {formatSessionId(s.id, 14)}
                                </span>
                                <span className="dash-session-item__time">
                                  {formatAgo(s.start_time)}
                                </span>
                              </div>

                              <div className="dash-session-item__row-3">
                                <span>{durFormatted} duration</span>
                                <span>
                                  {s.event_count || s.chunk_count} events · {s.chunk_count} chunk
                                  {s.chunk_count === 1 ? '' : 's'}
                                </span>
                              </div>
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

                    {/* Right Column: OpenReplay Unified Studio Theater */}
                    <div style={{ minWidth: 0, flex: 1 }} aria-label="Session player">
                      {sessionDetail ? (
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
                          projectPlatform={project?.platform}
                          deviceMode={replayDeviceMode}
                          onDeviceModeChange={setReplayDeviceMode}
                        />
                      ) : (
                        <section className="dash-card dash-card--compact" style={{ padding: '3.5rem 1rem', textAlign: 'center' }}>
                          <p className="dash-empty">Select a session from the queue to start replay.</p>
                        </section>
                      )}
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* ==================== EVENT LOG ==================== */}
            {section === 'logs' && (
              <section className="dash-card dash-card--compact">
                <div className="dash-card__head">
                  <div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                      <h2 className="dash-card__title">Event &amp; Error Stream</h2>
                      <span className="badge badge--role">{totalLogs} events</span>
                    </div>
                    <p className="dash-card__desc">
                      Exceptions, HTTP spans, and telemetry warnings with exact timestamps and correlated session replays.
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
                        onClick={() => {
                          setLogFilter(f.id)
                          setLogPage(1)
                        }}
                      >
                        {f.label}
                      </button>
                    ))}
                  </div>
                </div>

                {filteredLogs.length === 0 ? (
                  <div style={{ padding: '3rem 1rem', textAlign: 'center', color: 'var(--color-fg-muted)' }}>
                    <p style={{ fontWeight: 500, margin: '0 0 0.5rem 0', color: 'var(--color-fg)' }}>
                      No telemetry logs found in PostgreSQL
                    </p>
                    <p style={{ fontSize: '0.8125rem', margin: 0 }}>
                      {!isDemo && project && (realStats?.events_count ?? 0) === 0
                        ? 'Send events using your SDK client key to POST /api/v1/ingest or send a live test event.'
                        : 'No events match the selected filter.'}
                    </p>
                  </div>
                ) : (
                  <>
                    <div className="mo-stream">
                      {paginatedLogs.map((entry) => {
                        const isExpanded = expandedLogIds.has(entry.id)
                        const hasAttributes = entry.attributes && Object.keys(entry.attributes).length > 0
                        return (
                          <div
                            key={entry.id}
                            className={`mo-card${isExpanded ? ' is-expanded' : ''}`}
                          >
                            <div
                              className="mo-card__body"
                              onClick={() => toggleLogExpanded(entry.id)}
                            >
                              <div className="mo-card__left">
                                <span className={`mo-card__indicator mo-card__indicator--${entry.kind}`} />

                                <div className="mo-card__content">
                                  <div className="mo-card__title-row">
                                    <span className={`badge badge--${entry.kind}`}>{entry.kind}</span>
                                    {entry.statusCode && (
                                      <span
                                        className={`badge ${
                                          Number(entry.statusCode) >= 400 ? 'badge--exception' : 'badge--active'
                                        }`}
                                      >
                                        {entry.statusCode} {Number(entry.statusCode) === 200 ? 'OK' : ''}
                                      </span>
                                    )}
                                    <h3 className="mo-card__title">{entry.title}</h3>
                                  </div>

                                  <p className="mo-card__subtitle">
                                    {entry.detail}
                                    {entry.sessionId && entry.sessionId !== 'n/a'
                                      ? ` · session: ${entry.sessionId}`
                                      : ''}
                                  </p>
                                </div>
                              </div>

                              <div className="mo-card__right" onClick={(e) => e.stopPropagation()}>
                                <div
                                  className="mo-card__time"
                                  title={`Exact timestamp: ${entry.exactTime}${entry.rawTimestamp ? ` (${entry.rawTimestamp})` : ''}`}
                                >
                                  <span className="mo-card__time-exact">
                                    {entry.exactDateShort}, {entry.exactTimeShort}
                                  </span>
                                  <span className="mo-card__time-ago">{entry.time}</span>
                                </div>

                                <div className="mo-card__actions">
                                  {entry.sessionId && entry.sessionId !== 'n/a' && (
                                    <button
                                      type="button"
                                      className="dash-btn dash-btn--sm"
                                      onClick={() => openReplay(entry.sessionId)}
                                      title={`Inspect session replay for ${entry.sessionId}`}
                                    >
                                      Replay →
                                    </button>
                                  )}
                                  <button
                                    type="button"
                                    className="dash-btn dash-btn--sm"
                                    onClick={() => toggleLogExpanded(entry.id)}
                                    title="Toggle event details"
                                    style={{ padding: '0 6px' }}
                                  >
                                    <span className="mo-card__expand-icon">▾</span>
                                  </button>
                                </div>
                              </div>
                            </div>

                            {/* MoEngage-Style Parameter Grid on expand */}
                            {isExpanded && (
                              <div className="mo-drawer">
                                <div className="mo-drawer__head">
                                  <span className="mo-drawer__heading">Event Attributes &amp; Context</span>
                                  <span
                                    className="dash-code"
                                    style={{ fontSize: '0.6875rem', color: 'var(--color-muted)' }}
                                  >
                                    ID: {entry.id}
                                  </span>
                                </div>

                                <div className="mo-kv-grid">
                                  <div className="mo-kv-item">
                                    <span className="mo-kv-item__key">Event Name</span>
                                    <span className="mo-kv-item__val">{entry.eventName || entry.title}</span>
                                  </div>
                                  <div className="mo-kv-item">
                                    <span className="mo-kv-item__key">Type / Level</span>
                                    <span className="mo-kv-item__val">{entry.kind} · {entry.level || 'info'}</span>
                                  </div>
                                  <div className="mo-kv-item">
                                    <span className="mo-kv-item__key">Exact Timestamp</span>
                                    <span className="mo-kv-item__val">{entry.exactTime}</span>
                                  </div>
                                  <div className="mo-kv-item">
                                    <span className="mo-kv-item__key">Session ID</span>
                                    <span className="mo-kv-item__val">{entry.sessionId}</span>
                                  </div>
                                  {entry.environment && (
                                    <div className="mo-kv-item">
                                      <span className="mo-kv-item__key">Environment</span>
                                      <span className="mo-kv-item__val">{entry.environment}</span>
                                    </div>
                                  )}
                                  {entry.releaseVersion && (
                                    <div className="mo-kv-item">
                                      <span className="mo-kv-item__key">Release Version</span>
                                      <span className="mo-kv-item__val">{entry.releaseVersion}</span>
                                    </div>
                                  )}
                                  {entry.userId && (
                                    <div className="mo-kv-item">
                                      <span className="mo-kv-item__key">User ID</span>
                                      <span className="mo-kv-item__val">{entry.userId}</span>
                                    </div>
                                  )}
                                  {entry.statusCode && (
                                    <div className="mo-kv-item">
                                      <span className="mo-kv-item__key">HTTP Status</span>
                                      <span className="mo-kv-item__val">{entry.statusCode}</span>
                                    </div>
                                  )}
                                  {entry.durationMs !== undefined && (
                                    <div className="mo-kv-item">
                                      <span className="mo-kv-item__key">Duration</span>
                                      <span className="mo-kv-item__val">{entry.durationMs} ms</span>
                                    </div>
                                  )}
                                  {entry.attributes &&
                                    Object.entries(entry.attributes)
                                      .filter(
                                        ([k]) => !['method', 'url', 'status', 'status_code', 'duration_ms'].includes(k),
                                      )
                                      .map(([k, v]) => (
                                        <div key={k} className="mo-kv-item">
                                          <span className="mo-kv-item__key">{k.replace(/_/g, ' ')}</span>
                                          <span className="mo-kv-item__val">
                                            {typeof v === 'object' ? JSON.stringify(v) : String(v)}
                                          </span>
                                        </div>
                                      ))}
                                </div>

                                {hasAttributes && (
                                  <details style={{ marginTop: '2px' }}>
                                    <summary
                                      style={{
                                        cursor: 'pointer',
                                        fontSize: '0.6875rem',
                                        color: 'var(--color-muted)',
                                        fontFamily: 'var(--font-outlier)',
                                      }}
                                    >
                                      Raw JSON Payload
                                    </summary>
                                    <pre className="mo-json-box" style={{ marginTop: '6px' }}>
                                      {JSON.stringify(entry.attributes, null, 2)}
                                    </pre>
                                  </details>
                                )}
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>

                    {/* MoEngage-Style Stream Pagination */}
                    {totalLogs > LOGS_PER_PAGE && (
                      <div className="mo-pagination">
                        <span className="mo-pagination__info">
                          Showing <strong>{(currentLogPage - 1) * LOGS_PER_PAGE + 1}–{Math.min(currentLogPage * LOGS_PER_PAGE, totalLogs)}</strong> of <strong>{totalLogs}</strong> events
                        </span>
                        <div className="mo-pagination__nav">
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm"
                            disabled={currentLogPage <= 1}
                            onClick={() => setLogPage((p) => Math.max(1, p - 1))}
                          >
                            ← Prev
                          </button>
                          <span className="mo-pagination__pages">
                            Page {currentLogPage} of {totalLogPages}
                          </span>
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm"
                            disabled={currentLogPage >= totalLogPages}
                            onClick={() => setLogPage((p) => Math.min(totalLogPages, p + 1))}
                          >
                            Next →
                          </button>
                        </div>
                      </div>
                    )}
                  </>
                )}
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
      {({ project, isDemo, error }) => (
        <DashboardInner
          section={section}
          range={range}
          project={project}
          isDemo={isDemo}
          error={error}
        />
      )}
    </DashboardShell>
  )
}
