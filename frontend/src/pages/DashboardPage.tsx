import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import {
  analyticsApi,
  issueApi,
  replayApi,
  telemetryApi,
  type Issue,
  type Project,
  type ProjectStats,
  type ReplayChunkDetail,
  type ReplaySessionItem,
  type TelemetryEvent,
} from '../api'
import DashboardShell from '../components/DashboardShell'
import ReplayPlayer, { type DeviceMode } from '../components/ReplayPlayer'
import { fillHeight } from '../replay/fit'
import { browserStorage, defaultQueueOpen, readBool, writeBool } from '../replay/prefs'
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
  topCampaign: string
  buckets: BucketPoint[]
}

type OverviewCampaignItem = {
  name: string
  source: string
  visitors: string
  signups: string
  checkouts: string
  conv: string
}

type ReplayFilter = 'all' | 'error' | 'rage' | 'dead'
const REPLAY_PAGE_SIZE = 10
const QUEUE_OPEN_KEY = 'artracker.replay.queueOpen'

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
  issueId?: number
  environment?: string
  releaseVersion?: string
  userId?: string
  attributes?: Record<string, unknown>
}

type AlertRule = {
  id: string
  name: string
  condition: string
  channel: string
  status: 'firing' | 'armed' | 'muted'
  lastTriggered: string
}

const ALERT_DEFINITIONS: Omit<AlertRule, 'status' | 'lastTriggered'>[] = [
  {
    id: 'al_1',
    name: '5xx HTTP Error Spike',
    condition: 'http.status >= 500 on project endpoints in active window',
    channel: 'Slack · #ops-alerts',
  },
  {
    id: 'al_2',
    name: 'Crash-Free Session Drop',
    condition: 'error_free_rate < 98.5% over active window',
    channel: 'Webhook · PagerDuty',
  },
  {
    id: 'al_3',
    name: 'Open Crash / Exception Issues',
    condition: 'open_issues > 0 or unhandled exception logged',
    channel: 'Discord · #eng-alerts',
  },
  {
    id: 'al_4',
    name: 'Frustrated Session Activity',
    condition: 'rage_click or dead_click detected in recorded sessions',
    channel: 'Email · eng@company.com',
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
  // The replay workspace is exactly viewport-high (no page scroll); measured from its position on the page.
  const replayContainerRef = useRef<HTMLDivElement | null>(null)
  const [queueOpen, setQueueOpen] = useState<boolean>(() =>
    readBool(browserStorage(), QUEUE_OPEN_KEY, defaultQueueOpen(typeof window === 'undefined' ? 0 : window.innerWidth)),
  )
  const toggleQueue = () => {
    const next = !queueOpen
    setQueueOpen(next)
    writeBool(browserStorage(), QUEUE_OPEN_KEY, next)
  }
  const [replayHeight, setReplayHeight] = useState<number | null>(null)
  const [replayLoading, setReplayLoading] = useState<boolean>(true)

  useLayoutEffect(() => {
    if (section !== 'replays') return
    const measure = () => {
      const el = replayContainerRef.current
      if (!el) return
      const top = el.getBoundingClientRect().top + window.scrollY
      setReplayHeight(fillHeight(window.innerHeight, top))
    }
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [section, replayLoading, replaySessions.length])
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
  const [logSearch, setLogSearch] = useState<string>('')
  const [logPage, setLogPage] = useState<number>(1)
  const [logPageSize, setLogPageSize] = useState<number>(() => {
    try {
      const stored = localStorage.getItem('artracker.logs.pagesize')
      const val = stored ? parseInt(stored, 10) : 50
      return !isNaN(val) && val > 0 ? val : 50
    } catch {
      return 50
    }
  })
  const [expandedLogIds, setExpandedLogIds] = useState<Set<string>>(new Set())

  const activeReplaySession = useMemo(() => {
    if (sessionDetail?.session) return sessionDetail.session
    if (selectedSessionId) {
      const found = replaySessions.find((s) => s.id === selectedSessionId)
      if (found) return found
    }
    return replaySessions[0] ?? null
  }, [sessionDetail, selectedSessionId, replaySessions])
  // Never hide the queue while nothing is selected, or the user would have no way to pick a session
  const queueVisible = queueOpen || !activeReplaySession

  const handleLogPageSizeChange = (size: number) => {
    const validSize = Math.max(1, Math.min(500, size))
    setLogPageSize(validSize)
    setLogPage(1)
    try {
      localStorage.setItem('artracker.logs.pagesize', String(validSize))
    } catch {
      // storage unavailable
    }
  }

  const toggleLogExpanded = (id: string) => {
    setExpandedLogIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const [mutedAlertIds, setMutedAlertIds] = useState<Set<string>>(new Set())
  const [realStats, setRealStats] = useState<ProjectStats | null>(null)
  const [realEvents, setRealEvents] = useState<TelemetryEvent[]>([])
  const [realIssues, setRealIssues] = useState<Issue[]>([])
  const [realCampaigns, setRealCampaigns] = useState<OverviewCampaignItem[]>([])
  const [funnelSummary, setFunnelSummary] = useState<{ totalCheckouts: number; convPct: number } | null>(null)

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

  const fetchDashboardTelemetry = useCallback(
    (pid: number, rangeArg: RangeKey) => {
      const rangeDays = rangeArg === '24h' ? 1 : rangeArg === '7d' ? 7 : 30
      return Promise.all([
        telemetryApi.getStats(pid, rangeArg).catch(() => null),
        telemetryApi.listEvents(pid, 'all', 250).catch(() => ({ items: [] as TelemetryEvent[] })),
        issueApi.list(pid, { status: 'open', sort: 'frequency' }).catch(() => ({ items: [] as Issue[] })),
        analyticsApi
          .calculateFunnel(pid, {
            steps: ['landing_view', 'view_product', 'purchase_completed'],
            breakdown_by: 'utm_campaign',
            from: new Date(Date.now() - rangeDays * 86400000).toISOString(),
            to: new Date().toISOString(),
            window_seconds: 86400,
          })
          .catch(() => null),
      ])
    },
    [],
  )

  const applyDashboardTelemetry = useCallback(
    ([statsData, eventsData, issuesData, funnelData]: Awaited<ReturnType<typeof fetchDashboardTelemetry>>) => {
      setRealStats(statsData)
      setRealEvents(eventsData?.items ?? [])
      setRealIssues(issuesData?.items ?? [])

      if (funnelData) {
        const lastStepIdx = Math.max(0, (funnelData.steps?.length ?? 1) - 1)
        const totalCheckouts = funnelData.steps?.[lastStepIdx]?.count ?? 0
        setFunnelSummary({
          totalCheckouts,
          convPct: funnelData.overall_conversion_pct ?? 0,
        })

        const validBreakdowns = (funnelData.breakdowns ?? []).filter(
          (b) => (b.step_counts[0] ?? 0) > 0,
        )
        setRealCampaigns(
          validBreakdowns.map((b) => {
            const seg = b.segment && b.segment !== '(none)' ? b.segment : 'direct / organic'
            const visitorsCount = b.step_counts[0] ?? 0
            const signupsCount = b.step_counts.length >= 3 ? (b.step_counts[1] ?? 0) : visitorsCount
            const checkoutsCount = b.step_counts[b.step_counts.length - 1] ?? 0
            return {
              name: seg,
              source: b.segment && b.segment !== '(none)' ? 'tracked UTM' : 'direct traffic',
              visitors: visitorsCount.toLocaleString(),
              signups: signupsCount.toLocaleString(),
              checkouts: checkoutsCount.toLocaleString(),
              conv: `${b.overall_conversion_pct.toFixed(2)}%`,
            }
          }),
        )
      } else {
        setFunnelSummary(null)
        setRealCampaigns([])
      }
    },
    [],
  )

  const reloadTelemetry = () => {
    if (!projectReady) return
    void fetchDashboardTelemetry(targetPid, range).then(applyDashboardTelemetry)
  }

  const projectId = project?.id ?? null
  useEffect(() => {
    if (projectId == null) return
    let active = true
    void fetchDashboardTelemetry(projectId, range).then((result) => {
      if (!active) return
      applyDashboardTelemetry(result)
    })

    return () => {
      active = false
    }
  }, [projectId, range, fetchDashboardTelemetry, applyDashboardTelemetry])

  const emptyBuckets: BucketPoint[] = [
    { label: '00:00', sessions: 0, errors: 0 },
    { label: '04:00', sessions: 0, errors: 0 },
    { label: '08:00', sessions: 0, errors: 0 },
    { label: '12:00', sessions: 0, errors: 0 },
    { label: '16:00', sessions: 0, errors: 0 },
    { label: '20:00', sessions: 0, errors: 0 },
  ]

  // Pure real database metrics from PostgreSQL
  const stats: RangeStats = {
    sessions: (realStats?.sessions_count ?? 0).toLocaleString(),
    sessionsDelta: `${(realStats?.events_count ?? 0).toLocaleString()} events`,
    errorFree: realStats?.error_free_pct ?? '100.0%',
    errorNote: realStats?.error_note ?? '0 unhandled',
    conversions: (funnelSummary?.totalCheckouts ?? 0).toLocaleString(),
    convRate: `${(funnelSummary?.convPct ?? 0).toFixed(1)}% conv.`,
    topCampaign:
      realCampaigns.length > 0 ? `Top: ${realCampaigns[0].name}` : 'No active campaigns',
    buckets:
      realStats?.buckets && realStats.buckets.length > 0
        ? realStats.buckets
        : emptyBuckets,
  }

  // Pure real events from PostgreSQL
  const displayLogs: DevLogEntry[] = useMemo(
    () =>
      realEvents
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
          const status =
            (e.attributes?.status as number | string) ||
            (e.attributes?.status_code as number | string) ||
            undefined
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
        }),
    [realEvents],
  )

  // Real unresolved issues combining open crash/exception issues and error/5xx telemetry logs
  const unresolvedItems: DevLogEntry[] = useMemo(() => {
    const fromIssues: DevLogEntry[] = realIssues.map((iss) => {
      const isNetwork =
        iss.type.toLowerCase().includes('network') ||
        iss.title.includes('50') ||
        iss.title.includes('HTTP')
      return {
        id: `issue_${iss.id}`,
        issueId: iss.id,
        time: formatAgo(iss.last_seen),
        exactTime: formatExactTime(iss.last_seen),
        exactTimeShort: formatExactTimeShort(iss.last_seen),
        exactDateShort: formatExactDateShort(iss.last_seen),
        rawTimestamp: iss.last_seen,
        kind: isNetwork ? 'network' : 'exception',
        level: 'error',
        title: iss.title,
        detail: iss.latest_message && iss.latest_message !== iss.title ? iss.latest_message : iss.type,
        occurrences: iss.count,
        sessionId: '',
      }
    })

    const issueTitles = new Set(realIssues.map((i) => i.title))
    const fromErrorLogs = displayLogs.filter(
      (l) =>
        !issueTitles.has(l.title) &&
        (l.kind === 'exception' ||
          l.level === 'error' ||
          l.level === 'fatal' ||
          (l.statusCode !== undefined && Number(l.statusCode) >= 500)),
    )

    return [...fromIssues, ...fromErrorLogs]
  }, [realIssues, displayLogs])

  // Real-time alert rule evaluation against PostgreSQL project data
  const alerts: AlertRule[] = useMemo(() => {
    const http5xxLog = displayLogs.find(
      (l) =>
        (l.statusCode !== undefined && Number(l.statusCode) >= 500) ||
        /\b50[0234]\b/.test(l.title) ||
        /\b50[0234]\b/.test(l.detail),
    )
    const http5xxIssue = realIssues.find((i) => /\b50[0234]\b/.test(i.title))

    const errorFreeNum = realStats ? parseFloat(realStats.error_free_pct) : 100
    const crashFreeDropped =
      realStats != null && realStats.sessions_count > 0 && !Number.isNaN(errorFreeNum) && errorFreeNum < 98.5

    const latestIssue = realIssues[0]
    const latestErrorLog = displayLogs.find((l) => l.kind === 'exception' || l.level === 'error')
    const hasOpenIssuesOrErrors =
      realIssues.length > 0 || (realStats?.open_issues_count ?? 0) > 0 || (realStats?.errors_count ?? 0) > 0

    const frustratedSession = replaySessions.find((s) => s.has_rage_click || s.has_dead_click || s.has_error)
    const hasFrustrated =
      frustratedSession != null || (realStats?.frustrated_sessions_count ?? 0) > 0

    return ALERT_DEFINITIONS.map((def) => {
      if (mutedAlertIds.has(def.id)) {
        return { ...def, status: 'muted', lastTriggered: 'Muted' }
      }
      if (def.id === 'al_1') {
        const firing = Boolean(http5xxLog || http5xxIssue)
        const lastTriggered = http5xxLog
          ? http5xxLog.time
          : http5xxIssue
            ? formatAgo(http5xxIssue.last_seen)
            : 'Never'
        return { ...def, status: firing ? 'firing' : 'armed', lastTriggered }
      }
      if (def.id === 'al_2') {
        const lastTriggered = crashFreeDropped
          ? (latestErrorLog?.time ?? (latestIssue ? formatAgo(latestIssue.last_seen) : 'Active window'))
          : 'Never'
        return { ...def, status: crashFreeDropped ? 'firing' : 'armed', lastTriggered }
      }
      if (def.id === 'al_3') {
        const lastTriggered = latestIssue
          ? formatAgo(latestIssue.last_seen)
          : latestErrorLog
            ? latestErrorLog.time
            : hasOpenIssuesOrErrors
              ? 'Active window'
              : 'Never'
        return { ...def, status: hasOpenIssuesOrErrors ? 'firing' : 'armed', lastTriggered }
      }
      const lastTriggered = frustratedSession
        ? formatAgo(frustratedSession.start_time)
        : hasFrustrated
          ? 'Active window'
          : 'Never'
      return { ...def, status: hasFrustrated ? 'firing' : 'armed', lastTriggered }
    })
  }, [displayLogs, realIssues, realStats, replaySessions, mutedAlertIds])

  const logCounts = useMemo(
    () => ({
      all: displayLogs.length,
      exception: displayLogs.filter((l) => l.kind === 'exception').length,
      network: displayLogs.filter((l) => l.kind === 'network').length,
      console: displayLogs.filter((l) => l.kind === 'console').length,
    }),
    [displayLogs],
  )

  const filteredLogs = useMemo(() => {
    const byKind = logFilter === 'all' ? displayLogs : displayLogs.filter((l) => l.kind === logFilter)
    const q = logSearch.trim().toLowerCase()
    if (!q) return byKind
    return byKind.filter(
      (l) =>
        l.title.toLowerCase().includes(q) ||
        l.detail.toLowerCase().includes(q) ||
        (l.eventName && l.eventName.toLowerCase().includes(q)) ||
        (l.sessionId && l.sessionId.toLowerCase().includes(q)) ||
        (l.environment && l.environment.toLowerCase().includes(q)) ||
        (l.statusCode !== undefined && String(l.statusCode).includes(q)),
    )
  }, [displayLogs, logFilter, logSearch])
  const totalLogs = filteredLogs.length
  const totalLogPages = Math.max(1, Math.ceil(totalLogs / logPageSize))
  const currentLogPage = Math.min(logPage, totalLogPages)
  const paginatedLogs = filteredLogs.slice(
    (currentLogPage - 1) * logPageSize,
    currentLogPage * logPageSize,
  )

  const toggleAlert = (id: string) => {
    setMutedAlertIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
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
                    <span className="metric-cell__sub">{stats.topCampaign}</span>
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
                      <button type="button" className="dash-btn" onClick={() => goSection('/dashboard/issues')}>
                        All issues →
                      </button>
                    </div>

                    <div className="dash-list">
                      {unresolvedItems.length === 0 ? (
                        <div style={{ padding: '2.5rem 1rem', textAlign: 'center', color: 'var(--color-fg-muted)' }}>
                          <p style={{ fontWeight: 500, margin: '0 0 0.25rem 0', color: 'var(--color-fg)' }}>
                            No unresolved issues
                          </p>
                          <p style={{ fontSize: '0.8125rem', margin: 0 }}>
                            {!isDemo && project && (realStats?.events_count ?? 0) === 0 && (realStats?.sessions_count ?? 0) === 0
                              ? 'No telemetry events ingested yet for this project.'
                              : 'All recorded sessions in this window are clean.'}
                          </p>
                        </div>
                      ) : (
                        unresolvedItems.slice(0, 3).map((item) => (
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
                            {item.issueId ? (
                              <button
                                type="button"
                                className="dash-btn"
                                onClick={() => goSection(`/dashboard/issues/${item.issueId}`)}
                              >
                                Inspect →
                              </button>
                            ) : (
                              <button
                                type="button"
                                className="dash-btn"
                                onClick={() => openReplay(item.sessionId)}
                              >
                                Replay →
                              </button>
                            )}
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
                          {realCampaigns.length === 0 ? (
                            <tr>
                              <td
                                colSpan={4}
                                style={{
                                  padding: '2.5rem 1rem',
                                  textAlign: 'center',
                                  color: 'var(--color-fg-muted)',
                                }}
                              >
                                No marketing campaign traffic recorded in this window.
                              </td>
                            </tr>
                          ) : (
                            realCampaigns.map((c) => (
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
                            ))
                          )}
                        </tbody>
                      </table>
                    </div>
                  </section>
                </div>
              </div>
            )}

            {/* ==================== SESSION REPLAY ==================== */}
            {section === 'replays' && (
              <div
                className="dash-replay-container"
                ref={replayContainerRef}
                style={replayHeight ? { height: replayHeight } : undefined}
              >
                {replayError && (
                  <p className="dash-error" role="alert" style={{ marginBottom: '1rem' }}>
                    {replayError}
                  </p>
                )}

                {replayLoading && replaySessions.length === 0 ? (
                  <div className="replay-queue-skeleton" role="status" aria-label="Loading recorded sessions">
                    {[1, 2, 3, 4, 5].map((i) => (
                      <div key={i} className="replay-queue-skeleton__item">
                        <div className="replay-queue-skeleton__dot" />
                        <div className="replay-queue-skeleton__body">
                          <div className="replay-queue-skeleton__bar replay-queue-skeleton__bar--title" />
                          <div className="replay-queue-skeleton__bar replay-queue-skeleton__bar--sub" />
                        </div>
                      </div>
                    ))}
                  </div>
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
                  <div className={`dash-replay-grid${queueVisible ? '' : ' is-rail-closed'}`}>
                    {/* Left Column: Session Queue with Pagination */}
                    {queueVisible && (
                    <section className="dash-card dash-card--compact" aria-label="Recorded sessions">
                      <div className="dash-card__head">
                        <div>
                          <h2 className="dash-card__title">Session Queue</h2>
                          <p className="dash-card__desc">
                            {replayTotal} total · PII masked on client
                          </p>
                        </div>
                      </div>
                      <div className="dash-seg dash-replay-filters" role="group" aria-label="Filter replays">
                        {(
                          [
                            { id: 'all', label: 'All' },
                            { id: 'error', label: 'Errors' },
                            { id: 'rage', label: 'Rage clicks' },
                            { id: 'dead', label: 'Dead clicks' },
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
                    )}

                    {/* Right Column: OpenReplay Unified Studio Theater */}
                    <div className="or-player-col" aria-label="Session player">
                      {activeReplaySession ? (
                        <ReplayPlayer
                          session={activeReplaySession}
                          chunks={
                            sessionDetail && sessionDetail.session.id === activeReplaySession.id
                              ? sessionDetail.chunks
                              : []
                          }
                          timelineEvents={
                            sessionDetail && sessionDetail.session.id === activeReplaySession.id
                              ? sessionDetail.timelineEvents
                              : []
                          }
                          overrideEvents={
                            liveCapture && liveCapture.sessionId === activeReplaySession.id
                              ? liveCapture.events
                              : null
                          }
                          loadingDetail={
                            detailLoading ||
                            !sessionDetail ||
                            sessionDetail.session.id !== activeReplaySession.id
                          }
                          projectPlatform={project?.platform}
                          deviceMode={replayDeviceMode}
                          onDeviceModeChange={setReplayDeviceMode}
                          queueOpen={queueVisible}
                          onToggleQueue={toggleQueue}
                        />
                      ) : replayLoading ? (
                        <div className="or-theater or-theater--placeholder" role="status" aria-label="Loading session replay">
                          <div className="or-unified-header">
                            <div className="or-unified-header__left">
                              <span className="or-user-avatar or-user-avatar--skeleton" aria-hidden="true" />
                              <span className="replay-queue-skeleton__bar replay-queue-skeleton__bar--title" style={{ width: 140, height: 16 }} />
                            </div>
                            <div className="or-unified-header__right">
                              <span className="or-res-chip">Loading…</span>
                            </div>
                          </div>
                          <div className="or-stage">
                            {replayDeviceMode === 'web' || project?.platform === 'web' ? (
                              <div className="or-web-frame">
                                <div className="or-web-frame__chrome">
                                  <div className="or-web-dots" aria-hidden="true">
                                    <span className="or-web-dot or-web-dot--red" />
                                    <span className="or-web-dot or-web-dot--yellow" />
                                    <span className="or-web-dot or-web-dot--green" />
                                  </div>
                                  <div className="or-url-pill">Loading session…</div>
                                </div>
                                <div className="replay-player__viewport replay-player__state">
                                  <div className="replay-loading-spinner">
                                    <div className="replay-spinner-ring" />
                                    <div className="replay-spinner-glow" />
                                  </div>
                                </div>
                              </div>
                            ) : (
                              <div className={`or-phone-frame ${replayDeviceMode === 'android' || project?.platform === 'android' ? 'or-phone-frame--android' : 'or-phone-frame--ios'}`}>
                                <div className="or-phone-statusbar">
                                  <span>9:41</span>
                                  <div className="or-phone-status-icons">
                                    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2">
                                      <path d="M5 12.55a11 11 0 0 1 14.08 0" />
                                      <path d="M1.42 9a16 16 0 0 1 21.16 0" />
                                    </svg>
                                    <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
                                      <rect x="2" y="7" width="16" height="10" rx="2" ry="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
                                      <rect x="4" y="9" width="10" height="6" rx="1" ry="1" />
                                    </svg>
                                  </div>
                                </div>
                                <div className="replay-player__viewport replay-player__state">
                                  <div className="replay-loading-spinner">
                                    <div className="replay-spinner-ring" />
                                    <div className="replay-spinner-glow" />
                                  </div>
                                </div>
                                <div className="or-phone-homebar">
                                  <div className="or-phone-pill" />
                                </div>
                              </div>
                            )}
                          </div>
                        </div>
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
              <section className="dash-card dash-card--compact mo-stream-shell">
                <div className="mo-stream-header">
                  <div className="mo-stream-header__title-wrap">
                    <div className="mo-stream-header__title-row">
                      <h2 className="dash-card__title">Event &amp; Error Stream</h2>
                      <span className="badge badge--role mo-stream-count-badge">
                        {totalLogs} {totalLogs === 1 ? 'event' : 'events'}
                      </span>
                    </div>
                    <p className="dash-card__desc">
                      Exceptions, HTTP spans, and telemetry warnings with exact timestamps and correlated session replays.
                    </p>
                  </div>

                  <div className="mo-stream-header__controls">
                    <div className="dash-seg mo-stream-seg" role="group" aria-label="Filter log type">
                      {(
                        [
                          { id: 'all', label: 'All', count: logCounts.all },
                          { id: 'exception', label: 'Exceptions', count: logCounts.exception },
                          { id: 'network', label: 'Network', count: logCounts.network },
                          { id: 'console', label: 'Console', count: logCounts.console },
                        ] as { id: LogKind; label: string; count: number }[]
                      ).map((f) => (
                        <button
                          key={f.id}
                          type="button"
                          className={`dash-seg__btn mo-stream-seg__btn${logFilter === f.id ? ' is-active' : ''}`}
                          aria-pressed={logFilter === f.id}
                          onClick={() => {
                            setLogFilter(f.id)
                            setLogPage(1)
                          }}
                        >
                          <span>{f.label}</span>
                          <span className="mo-stream-seg__count">{f.count}</span>
                        </button>
                      ))}
                    </div>

                    <button
                      type="button"
                      className="dash-btn dash-btn--sm"
                      onClick={reloadTelemetry}
                      title="Refresh event stream"
                    >
                      Refresh
                    </button>
                  </div>
                </div>

                <div className="mo-stream-toolbar">
                  <div className="mo-stream-search">
                    <svg
                      className="mo-stream-search__icon"
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      aria-hidden="true"
                    >
                      <circle cx="11" cy="11" r="8" />
                      <path d="m21 21-4.3-4.3" />
                    </svg>
                    <input
                      type="search"
                      className="mo-stream-search__input"
                      placeholder="Filter by event name, message, HTTP status, environment, or session ID..."
                      value={logSearch}
                      onChange={(e) => {
                        setLogSearch(e.target.value)
                        setLogPage(1)
                      }}
                    />
                    {logSearch && (
                      <button
                        type="button"
                        className="mo-stream-search__clear"
                        onClick={() => {
                          setLogSearch('')
                          setLogPage(1)
                        }}
                        aria-label="Clear search"
                      >
                        ×
                      </button>
                    )}
                  </div>

                  <div className="mo-stream-summary" aria-label="Event category summary">
                    <span className="mo-stream-summary__pill">
                      <span className="mo-card__indicator mo-card__indicator--exception" />
                      <span>Exceptions</span>
                      <strong>{logCounts.exception}</strong>
                    </span>
                    <span className="mo-stream-summary__pill">
                      <span className="mo-card__indicator mo-card__indicator--network" />
                      <span>Network</span>
                      <strong>{logCounts.network}</strong>
                    </span>
                    <span className="mo-stream-summary__pill">
                      <span className="mo-card__indicator mo-card__indicator--console" />
                      <span>Console</span>
                      <strong>{logCounts.console}</strong>
                    </span>
                  </div>
                </div>

                {filteredLogs.length === 0 ? (
                  <div className="mo-empty-state" role="status">
                    <div className="mo-empty-state__icon" aria-hidden="true">
                      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M4 6h16M4 12h16M4 18h10" />
                        <circle cx="18" cy="18" r="3" />
                      </svg>
                    </div>
                    {displayLogs.length === 0 ? (
                      <>
                        <h3 className="mo-empty-state__title">No telemetry events ingested yet</h3>
                        <p className="mo-empty-state__desc">
                          Exceptions, HTTP network spans, and custom telemetry events sent from your application SDK will stream here in real time.
                        </p>
                        <div className="mo-empty-state__meta">
                          <span className="dash-code">POST /api/v1/ingest</span>
                          {project && <span className="dash-code">project: {project.slug}</span>}
                        </div>
                        <div className="mo-empty-state__actions">
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm"
                            onClick={reloadTelemetry}
                          >
                            Refresh Stream
                          </button>
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm dash-btn--primary"
                            onClick={() => goSection('/dashboard/settings')}
                          >
                            API Keys &amp; SDK Setup →
                          </button>
                        </div>
                      </>
                    ) : (
                      <>
                        <h3 className="mo-empty-state__title">No events match the active filter</h3>
                        <p className="mo-empty-state__desc">
                          Showing 0 of {displayLogs.length} ingested events
                          {logFilter !== 'all' ? ` in "${logFilter}"` : ''}
                          {logSearch.trim() ? ` matching "${logSearch.trim()}"` : ''}.
                        </p>
                        <div className="mo-empty-state__actions">
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm"
                            onClick={() => {
                              setLogFilter('all')
                              setLogSearch('')
                              setLogPage(1)
                            }}
                          >
                            Reset Filters
                          </button>
                        </div>
                      </>
                    )}
                  </div>
                ) : (
                  <>
                    <div className="mo-stream">
                      <div className="mo-stream__cols" aria-hidden="true">
                        <span>Type</span>
                        <span>Event &amp; Message</span>
                        <span>Session / Env</span>
                        <span style={{ textAlign: 'right' }}>Timestamp</span>
                        <span style={{ textAlign: 'right' }}>Actions</span>
                      </div>
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
                              <div className="mo-card__col-type">
                                <span className={`mo-card__indicator mo-card__indicator--${entry.kind}`} />
                                <span className={`badge badge--${entry.kind} mo-card__type-badge`}>{entry.kind}</span>
                              </div>

                              <div className="mo-card__content">
                                <div className="mo-card__title-row">
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
                                  {entry.durationMs !== undefined && (
                                    <span className="mo-card__duration">{entry.durationMs}ms</span>
                                  )}
                                </div>

                                <p className="mo-card__subtitle">
                                  {entry.detail || entry.eventName || 'Telemetry event'}
                                </p>
                              </div>

                              <div className="mo-card__col-meta">
                                {entry.sessionId && entry.sessionId !== 'n/a' ? (
                                  <span className="mo-card__session-chip" title={entry.sessionId}>
                                    {entry.sessionId.length > 14
                                      ? `${entry.sessionId.slice(0, 12)}…`
                                      : entry.sessionId}
                                  </span>
                                ) : (
                                  <span className="mo-card__session-empty">—</span>
                                )}
                                {entry.environment && (
                                  <span className="mo-card__env-tag">{entry.environment}</span>
                                )}
                              </div>

                              <div
                                className="mo-card__time"
                                title={`Exact timestamp: ${entry.exactTime}${entry.rawTimestamp ? ` (${entry.rawTimestamp})` : ''}`}
                              >
                                <span className="mo-card__time-exact">
                                  {entry.exactDateShort}, {entry.exactTimeShort}
                                </span>
                                <span className="mo-card__time-ago">{entry.time}</span>
                              </div>

                              <div className="mo-card__actions" onClick={(e) => e.stopPropagation()}>
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
                                  className="dash-btn dash-btn--sm mo-card__expand-btn"
                                  onClick={() => toggleLogExpanded(entry.id)}
                                  title="Toggle event details"
                                >
                                  <span className="mo-card__expand-icon">▾</span>
                                </button>
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

                    {/* Precision Stream Pagination */}
                    {totalLogs > 0 && (
                      <div className="mo-pagination">
                        <div className="mo-pagination__left">
                          <span className="mo-pagination__info">
                            Showing <strong>{(currentLogPage - 1) * logPageSize + 1}–{Math.min(currentLogPage * logPageSize, totalLogs)}</strong> of <strong>{totalLogs}</strong> events
                          </span>

                          <div className="mo-pagination__size-control">
                            <span className="mo-pagination__size-label">Per page:</span>
                            <div className="dash-seg mo-pagination__seg" role="group" aria-label="Page size options">
                              {[10, 25, 50, 100].map((size) => (
                                <button
                                  key={size}
                                  type="button"
                                  className={`dash-seg__btn mo-pagination__seg-btn${logPageSize === size ? ' is-active' : ''}`}
                                  aria-pressed={logPageSize === size}
                                  onClick={() => handleLogPageSizeChange(size)}
                                >
                                  {size}
                                </button>
                              ))}
                            </div>

                            <div className="mo-pagination__custom-wrap" title="Custom items per page (5-500)">
                              <input
                                type="number"
                                min={5}
                                max={500}
                                step={5}
                                placeholder="Custom"
                                className="mo-pagination__custom-input"
                                value={[10, 25, 50, 100].includes(logPageSize) ? '' : logPageSize}
                                onChange={(e) => {
                                  const val = parseInt(e.target.value, 10)
                                  if (!isNaN(val) && val > 0) {
                                    handleLogPageSizeChange(val)
                                  }
                                }}
                              />
                            </div>
                          </div>
                        </div>

                        <div className="mo-pagination__nav">
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm"
                            disabled={currentLogPage <= 1}
                            onClick={() => setLogPage(1)}
                            title="First page"
                          >
                            « First
                          </button>
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm"
                            disabled={currentLogPage <= 1}
                            onClick={() => setLogPage((p) => Math.max(1, p - 1))}
                            title="Previous page"
                          >
                            ← Prev
                          </button>
                          <span className="mo-pagination__pages">
                            Page <strong>{currentLogPage}</strong> of <strong>{totalLogPages}</strong>
                          </span>
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm"
                            disabled={currentLogPage >= totalLogPages}
                            onClick={() => setLogPage((p) => Math.min(totalLogPages, p + 1))}
                            title="Next page"
                          >
                            Next →
                          </button>
                          <button
                            type="button"
                            className="dash-btn dash-btn--sm"
                            disabled={currentLogPage >= totalLogPages}
                            onClick={() => setLogPage(totalLogPages)}
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
