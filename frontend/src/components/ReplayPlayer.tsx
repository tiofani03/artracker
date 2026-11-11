import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { EventType, type eventWithTime } from 'rrweb'
import rrwebPlayer from 'rrweb-player'
import 'rrweb-player/dist/style.css'
import {
  replayApi,
  type Platform,
  type ReplayChunkDetail,
  type ReplaySessionItem,
  type SessionPerfResponse,
  type TelemetryEvent,
} from '../api'
import { loadChunkPayloads } from '../utils/replayChunkLoader'
import { ScreenChunkSource } from '../utils/screenChunkSource'
import { buildReplayModel } from '../replay/normalize'
import type { ReplayModel } from '../replay/model'
import {
  activeFrameAt,
  deviceClock,
  fallbackNotice,
  replayModeLabel,
  routeStrip,
} from '../replay/playback'
import { activeScreenFrameAt } from '../replay/screen'
import { fitScale } from '../replay/fit'
import { filterMarkers, type MarkerFilter } from '../replay/markerFilter'
import { ReplaySidePanel, type SidePanelTab } from './replay/ReplaySidePanel'
import { PerformancePanel } from './replay/PerformancePanel'
import { TimelineTracks } from './replay/TimelineTracks'
import { describeBreach, detectBreaches, memTrackMax, perfValueAt, toSeries } from '../replay/perf'
import { browserStorage, readBool, writeBool } from '../replay/prefs'
import { mergeScreenRuns, type TrackDef } from '../replay/tracks'
import { adjacentEventOffset, resolveHotkey, SEEK_STEP_MS } from '../replay/hotkeys'
import { fitScreenStage } from '../replay/screenPlanner'
import { probeScreenSession } from '../replay/screenProbe'
import type { ScreenReplaySession } from '../replay/screenSession'
import { chunkSignature } from '../replay/wireframe'
import { detectSessionPlatform, type DetectedPlatform } from '../utils/platformDetector'
import { MobileWireframeRenderer } from './MobileWireframeRenderer'
import { ScreenReplayRenderer } from './ScreenReplayRenderer'
export type TimelineMarker = {
  id: string
  offsetMs: number
  pct: number
  formattedTime: string
  tone: 'error' | 'warn' | 'info'
  badgeLabel: string
  text: string
  category: 'error' | 'network' | 'gesture' | 'perf' | 'other'
  rawEvent?: TelemetryEvent
}

export type DeviceMode = 'auto' | 'web' | 'ios' | 'android'

/** A phone chassis may be enlarged up to this factor to fill a tall stage. */
const PHONE_MAX_SCALE = 1.6

/** Upper bound for a screenshot stage's height; taller chassis are scaled down by the stage fit. */
const SCREEN_MAX_HEIGHT = 900

/** Aspect of the last screenshot stage shown, and the fallback (a modern tall phone) for a first-ever load. */
let lastScreenRatio: { w: number; h: number } = { w: 1080, h: 2340 }

/** Parses a "1080x2400" style device screen string; null when it is not that shape. */
function parseDeviceScreen(raw: unknown): { w: number; h: number } | null {
  if (typeof raw !== 'string') return null
  const m = /^\s*(\d{2,5})\s*[x×*]\s*(\d{2,5})\s*$/i.exec(raw)
  if (!m) return null
  const a = Number(m[1])
  const b = Number(m[2])
  // device_info may list landscape (w>h) for a phone; the chassis is portrait, so order by the longer side
  return { w: Math.min(a, b), h: Math.max(a, b) }
}

/** rrweb-player is a Svelte component; its typings omit the Svelte instance API. */
type SvelteHandle = { $set?: (props: Record<string, unknown>) => void; $destroy?: () => void }

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

function formatOffsetMs(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000))
  const min = Math.floor(totalSec / 60)
  const sec = totalSec % 60
  return `${String(min).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
}

function classifyMarker(ev: TelemetryEvent): {
  tone: 'error' | 'warn' | 'info'
  badgeLabel: string
  category: 'error' | 'network' | 'gesture' | 'other'
} {
  const lvl = (ev.level || '').toLowerCase()
  const name = (ev.name || '').toLowerCase()
  const msg = (ev.message || '').toLowerCase()
  const attrs = (ev.attributes || {}) as Record<string, unknown>

  if (
    lvl === 'error' ||
    lvl === 'fatal' ||
    name.includes('error') ||
    name.includes('exception') ||
    name.includes('crash') ||
    msg.includes('error') ||
    msg.includes('uncaught') ||
    msg.includes('exception') ||
    msg.includes('typeerror')
  ) {
    if (name.includes('anr') || msg.includes('anr')) {
      return { tone: 'error', badgeLabel: 'anr', category: 'error' }
    }
    if (name.includes('hang') || msg.includes('hang')) {
      return { tone: 'error', badgeLabel: 'ui hang', category: 'error' }
    }
    return { tone: 'error', badgeLabel: 'error', category: 'error' }
  }
  if (name === 'rage_tap' || msg.includes('rage tap') || name === 'rage_click' || msg.includes('rage click')) {
    return { tone: 'warn', badgeLabel: 'rage click', category: 'gesture' }
  }
  if (name === 'dead_click' || msg.includes('dead click')) {
    return { tone: 'warn', badgeLabel: 'dead click', category: 'gesture' }
  }
  if (name === 'click' || name === 'touch' || Boolean(attrs.gesture)) {
    return { tone: 'info', badgeLabel: 'tap', category: 'gesture' }
  }
  if (attrs.method || attrs.endpoint || attrs.status_code || name === 'network' || name.startsWith('http')) {
    const status = typeof attrs.status_code === 'number' ? attrs.status_code : typeof attrs.status === 'number' ? attrs.status : 200
    return {
      tone: status >= 400 ? 'error' : 'info',
      badgeLabel: `${attrs.method || 'HTTP'} ${status}`,
      category: 'network',
    }
  }
  if (lvl === 'warn' || lvl === 'warning') {
    return { tone: 'warn', badgeLabel: 'warn', category: 'other' }
  }
  return { tone: 'info', badgeLabel: ev.kind === 'event' ? 'event' : 'log', category: 'other' }
}

export default function ReplayPlayer({
  session,
  chunks,
  timelineEvents,
  overrideEvents,
  loadingDetail,
  projectPlatform,
  deviceMode = 'auto',
  onDeviceModeChange,
  queueOpen,
  onToggleQueue,
  perf,
}: {
  session: ReplaySessionItem
  chunks: ReplayChunkDetail[]
  timelineEvents: TelemetryEvent[]
  overrideEvents?: eventWithTime[] | null
  loadingDetail?: boolean
  projectPlatform?: Platform
  deviceMode?: DeviceMode
  onDeviceModeChange?: (mode: DeviceMode) => void
  queueOpen?: boolean
  onToggleQueue?: () => void
  /** Profiling samples for this session (CPU / memory / FPS); drawn as timeline tracks. */
  perf?: SessionPerfResponse | null
}) {
  const stageWrapRef = useRef<HTMLDivElement | null>(null)
  const playerRef = useRef<rrwebPlayer | null>(null)
  // Callback-ref state: the rrweb mount node can unmount/remount (loading state, device chrome switch)
  const [mountEl, setMountEl] = useState<HTMLDivElement | null>(null)

  // OpenReplay-style dynamic platform detection (used only when the replay model has no platform)
  const detection = useMemo(
    () => detectSessionPlatform(session, timelineEvents, projectPlatform),
    [session, timelineEvents, projectPlatform],
  )

  const [payloads, setPayloads] = useState<unknown[]>([])
  // Screenshot sessions (artracker-screen-v1) load progressively through this instead of `payloads`
  const [screenSession, setScreenSession] = useState<ScreenReplaySession | null>(null)
  const [screenScanning, setScreenScanning] = useState(false)
  const [failedChunks, setFailedChunks] = useState(0)
  const [chunkLoading, setChunkLoading] = useState(false)
  const [chunkError, setChunkError] = useState<string | null>(null)
  const [currentTimeMs, setCurrentTimeMs] = useState(0)
  const [isPlaying, setIsPlaying] = useState(false)
  const [playbackSpeed, setPlaybackSpeed] = useState<number>(1)
  const [stageWidth, setStageWidth] = useState(720)

  const presignedChunkCount = useMemo(
    () => chunks.filter((c) => Boolean(c.url && c.url.trim())).length,
    [chunks],
  )
  const hasOverride = Boolean(overrideEvents && overrideEvents.length > 0)

  // Session-detail refetches return the same chunks with freshly signed URLs; only a real change
  // in the chunk set (or session) may reload chunks and reset the playhead.
  const chunkSig = useMemo(() => chunkSignature(chunks), [chunks])
  const chunksRef = useRef(chunks)
  useLayoutEffect(() => {
    chunksRef.current = chunks
  })

  // Fetch & gunzip chunks. A screenshot session (artracker-screen-v1) is recognised by probing its first and
  // last chunk and then loads progressively; any other session is fetched eagerly and interpreted by
  // buildReplayModel.
  useEffect(() => {
    setCurrentTimeMs(0)
    setIsPlaying(false)
    setScreenSession(null)
    setScreenScanning(false)
    if (hasOverride || presignedChunkCount === 0) {
      setPayloads([])
      setFailedChunks(0)
      setChunkError(null)
      setChunkLoading(false)
      return
    }

    const controller = new AbortController()
    setChunkLoading(true)
    setChunkError(null)

    const run = async () => {
      const source = new ScreenChunkSource(chunksRef.current, (from, to) =>
        replayApi.listSessionChunks(session.project_id, session.id, from, to).then((r) => r.chunks),
      )
      const startMs = Date.parse(session.start_time)
      const screen = await probeScreenSession({
        chunks: chunksRef.current,
        sessionStartMs: Number.isFinite(startMs) ? startMs : 0,
        fetchChunk: (seq, signal) => source.fetchChunk(seq, signal),
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      if (screen) {
        setScreenSession(screen)
        setScreenScanning(true)
        setPayloads([])
        setFailedChunks(0)
        setChunkLoading(false)
        void screen.start(controller.signal).finally(() => {
          if (!controller.signal.aborted) setScreenScanning(false)
        })
        return
      }
      const r = await loadChunkPayloads(chunksRef.current, controller.signal)
      if (controller.signal.aborted) return
      setPayloads(r.payloads)
      setFailedChunks(r.failed)
      setChunkLoading(false)
    }

    run().catch((err: unknown) => {
      if (controller.signal.aborted) return
      if (err instanceof DOMException && err.name === 'AbortError') return
      setChunkError(err instanceof Error ? err.message : 'Failed to fetch replay chunks')
      setPayloads([])
      setFailedChunks(0)
      setChunkLoading(false)
    })

    return () => controller.abort()
  }, [session.id, session.project_id, session.start_time, chunkSig, presignedChunkCount, hasOverride])

  // Re-render whenever the screenshot session indexes more chunks
  const subscribeScreen = useCallback(
    (fn: () => void) => (screenSession ? screenSession.subscribe(fn) : () => {}),
    [screenSession],
  )
  const screenVersion = useSyncExternalStore(subscribeScreen, () => (screenSession ? screenSession.version : 0))

  // Single source of truth for what can be rendered
  const model: ReplayModel = useMemo(() => {
    if (screenSession && !hasOverride) {
      // the session index is mutable: `screenVersion` (a dependency below) is what invalidates this memo
      const idx = screenSession.index
      if (idx.frames.length > 0) {
        const projectMobile = projectPlatform === 'ios' || projectPlatform === 'android' ? projectPlatform : null
        return {
          kind: 'screen',
          platform: idx.platform ?? projectMobile ?? 'android',
          startMs: idx.startMs,
          // the scan may not have reached the last chunk yet: the acknowledged session duration is the floor
          endMs: Math.max(idx.endMs, idx.startMs + session.duration_ms),
          session: screenSession,
        }
      }
    }
    const built = buildReplayModel({
      payloads: hasOverride ? [overrideEvents] : payloads,
      timeline: timelineEvents,
      projectPlatform,
    })
    if (built.kind !== 'none') return built
    if (chunkError) return { kind: 'none', reason: 'load-error', detail: chunkError }
    if (!hasOverride && chunks.length > 0 && presignedChunkCount === 0) {
      return {
        kind: 'none',
        reason: 'load-error',
        detail: `${chunks.length} replay chunk(s) exist but no download URL was issued (replay storage is not configured on the API).`,
      }
    }
    return built
  }, [
    hasOverride,
    overrideEvents,
    payloads,
    timelineEvents,
    projectPlatform,
    chunkError,
    chunks.length,
    presignedChunkCount,
    screenSession,
    screenVersion,
    session.duration_ms,
  ])

  const webEvents = model.kind === 'web' ? model.events : null

  const effectivePlatform: DetectedPlatform =
    deviceMode !== 'auto'
      ? deviceMode
      : model.kind === 'mobile' || model.kind === 'screen'
        ? model.platform
        : model.kind === 'web' && model.inferredPlatform && model.inferredPlatform !== 'web'
          ? model.inferredPlatform
          : detection.platform

  // Observe player container width for responsive scaling
  useEffect(() => {
    const el = stageWrapRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const updateWidth = () => {
      const w = Math.floor(el.clientWidth)
      if (w > 240) setStageWidth(Math.min(w, 880))
    }
    updateWidth()
    const ro = new ResizeObserver(updateWidth)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const playerWidth =
    effectivePlatform === 'ios'
      ? 308
      : effectivePlatform === 'android'
        ? 308
        : stageWidth
  const playerHeight =
    effectivePlatform === 'ios'
      ? 502
      : effectivePlatform === 'android'
        ? 502
        : Math.max(340, Math.min(520, Math.round(stageWidth * 0.5625)))

  // Screenshot stage size, shared by the loading placeholder and the renderer so the chassis never resizes
  // between phases. Frame ratio when known, else the device-reported or last seen ratio.
  const isScreenSession = screenSession !== null || model.kind === 'screen'
  const firstScreenFrame = model.kind === 'screen' ? (model.session.index.frames[0] ?? null) : null
  if (firstScreenFrame) lastScreenRatio = { w: firstScreenFrame.w, h: firstScreenFrame.h }
  const screenStagePx = fitScreenStage(
    firstScreenFrame ?? parseDeviceScreen(session.device_info?.screen) ?? lastScreenRatio,
    { width: playerWidth, maxHeight: SCREEN_MAX_HEIGHT, minHeight: 120 },
  )

  // a screenshot session whose first chunk is still being indexed has nothing to show yet
  const screenPending = screenSession !== null && screenScanning && model.kind === 'none'
  const isLoadingReplay = loadingDetail || chunkLoading || screenPending

  const durationMs = useMemo(() => {
    if (model.kind !== 'none') return Math.max(1000, model.endMs - model.startMs)
    return Math.max(session.duration_ms || 0, 1000)
  }, [model, session.duration_ms])

  const baseTimestampMs = useMemo(() => {
    if (model.kind !== 'none') return model.startMs
    const parsed = new Date(session.start_time).getTime()
    return Number.isNaN(parsed) ? Date.now() : parsed
  }, [model, session.start_time])

  // Values read inside long-lived callbacks without re-creating the rrweb player
  const sizeRef = useRef({ width: playerWidth, height: playerHeight })
  const durationRef = useRef(durationMs)
  const currentTimeRef = useRef(currentTimeMs)
  const isPlayingRef = useRef(isPlaying)
  const speedRef = useRef(playbackSpeed)
  // Layout effects run before passive effects, so the refs are current when the player effects read them
  useLayoutEffect(() => {
    sizeRef.current = { width: playerWidth, height: playerHeight }
    durationRef.current = durationMs
    currentTimeRef.current = currentTimeMs
    isPlayingRef.current = isPlaying
    speedRef.current = playbackSpeed
  })

  // Dynamically extract real recorded URL / screen from rrweb Meta events or timeline events
  const recordedUrl = useMemo(() => {
    if (webEvents && webEvents.length > 0) {
      const targetTs = baseTimestampMs + currentTimeMs
      let currentHref: string | null = null
      for (const ev of webEvents) {
        if (ev.type === EventType.Meta) {
          const href = (ev.data as { href?: string })?.href
          if (href) {
            if (ev.timestamp <= targetTs + 50 || currentHref === null) {
              currentHref = href
            } else {
              break
            }
          }
        }
      }
      if (currentHref) return currentHref
    }
    for (const ev of timelineEvents) {
      const attrs = ev.attributes as Record<string, unknown> | undefined
      if (attrs) {
        if (typeof attrs.url === 'string' && attrs.url.trim()) return attrs.url.trim()
        if (typeof attrs.path === 'string' && attrs.path.trim()) return attrs.path.trim()
        if (typeof attrs.screen === 'string' && attrs.screen.trim()) return attrs.screen.trim()
        if (typeof attrs.route === 'string' && attrs.route.trim()) return attrs.route.trim()
      }
    }
    return null
  }, [webEvents, timelineEvents, baseTimestampMs, currentTimeMs])

  // Active mobile screen at the playhead (frames come from chunks or telemetry view trees)
  const activeScreen =
    model.kind === 'mobile'
      ? (activeFrameAt(model.frames, model.startMs + currentTimeMs)?.screen ?? null)
      : model.kind === 'screen'
        ? (activeScreenFrameAt(model.session.index.frames, model.startMs + currentTimeMs)?.screen ?? null)
        : null

  // Only recorded values: no invented URLs / activity names when nothing was captured
  const strip = routeStrip({ platform: effectivePlatform, screen: activeScreen, recordedUrl })
  const clock = deviceClock(baseTimestampMs + currentTimeMs) ?? '--:--'

  const displayPath = useMemo(() => {
    if (!recordedUrl) return strip.value
    if (recordedUrl.startsWith('android://') || recordedUrl.startsWith('ios://')) {
      return strip.value
    }
    try {
      const u = new URL(recordedUrl)
      return `${u.pathname}${u.search}` || '/'
    } catch {
      return strip.value
    }
  }, [recordedUrl, strip.value])

  // Scrubber: click or drag anywhere on the track to seek
  // Build timeline markers (red = error, yellow = warn / click / frustration)
  const eventMarkers: TimelineMarker[] = useMemo(() => {
    if (timelineEvents.length === 0) return []
    return timelineEvents.map((ev, idx) => {
      const evTs = new Date(ev.event_timestamp).getTime()
      let offsetMs: number
      if (!Number.isNaN(evTs) && evTs >= baseTimestampMs && evTs <= baseTimestampMs + durationMs) {
        offsetMs = evTs - baseTimestampMs
      } else if (!Number.isNaN(evTs) && evTs > baseTimestampMs + durationMs) {
        offsetMs = durationMs
      } else if (!Number.isNaN(evTs) && evTs < baseTimestampMs && baseTimestampMs > 0) {
        offsetMs = 0
      } else {
        // Fallback proportional placement when DB clocks differ slightly from chunk monotonic timestamps
        offsetMs = Math.round(((idx + 1) / (timelineEvents.length + 1)) * durationMs)
      }

      const pct = durationMs > 0 ? Math.min(99, Math.max(1, (offsetMs / durationMs) * 100)) : 50
      const { tone, badgeLabel, category } = classifyMarker(ev)
      const text =
        ev.message && ev.name
          ? `${ev.name}: ${ev.message}`
          : ev.message || ev.name || `${ev.kind} recorded`

      return {
        id: ev.id || `${idx}`,
        offsetMs,
        pct,
        formattedTime: formatOffsetMs(offsetMs),
        tone,
        badgeLabel,
        text,
        category,
        rawEvent: ev,
      }
    })
  }, [timelineEvents, baseTimestampMs, durationMs])

  // Profiling breaches (high CPU, low FPS, jank bursts) are shown as events too, so they can be found,
  // filtered and sought to like any other marker.
  const perfBreaches = useMemo(
    () =>
      perf && perf.samples.length > 0
        ? detectBreaches(perf.samples, baseTimestampMs, perf.interval_ms, perf.device.refresh_hz)
        : [],
    [perf, baseTimestampMs],
  )
  const markers: TimelineMarker[] = useMemo(() => {
    if (perfBreaches.length === 0) return eventMarkers
    const refresh = perf?.device.refresh_hz
    const breachMarkers: TimelineMarker[] = perfBreaches.map((b) => {
      const offsetMs = Math.min(durationMs, Math.max(0, b.startMs))
      const d = describeBreach(b, refresh)
      return {
        id: `perf-${b.kind}-${b.startMs}`,
        offsetMs,
        pct: durationMs > 0 ? Math.min(99, Math.max(1, (offsetMs / durationMs) * 100)) : 50,
        formattedTime: formatOffsetMs(offsetMs),
        tone: 'warn' as const,
        badgeLabel: d.badge,
        text: d.text,
        category: 'perf' as const,
      }
    })
    return [...eventMarkers, ...breachMarkers].sort((a, b) => a.offsetMs - b.offsetMs)
  }, [eventMarkers, perfBreaches, perf, durationMs])

  // Screen spans from frames or telemetry
  const screenSpans = useMemo(() => {
    const raw: { screen: string; startMs: number; endMs: number }[] = []
    if (model.kind === 'mobile' && model.frames.length > 0) {
      for (let i = 0; i < model.frames.length; i++) {
        const f = model.frames[i]
        const next = model.frames[i + 1]
        const start = Math.max(0, f.ts - baseTimestampMs)
        const end = next ? Math.max(start + 500, next.ts - baseTimestampMs) : durationMs
        raw.push({ screen: f.screen, startMs: start, endMs: Math.min(durationMs, end) })
      }
    } else if (model.kind === 'screen' && model.session.index.frames.length > 0) {
      for (let i = 0; i < model.session.index.frames.length; i++) {
        const f = model.session.index.frames[i]
        const next = model.session.index.frames[i + 1]
        const start = Math.max(0, f.ts - baseTimestampMs)
        const end = next ? Math.max(start + 500, next.ts - baseTimestampMs) : durationMs
        raw.push({ screen: f.screen, startMs: start, endMs: Math.min(durationMs, end) })
      }
    }
    return mergeScreenRuns(raw).map((s) => ({
      ...s,
      durationMs: Math.max(1, s.endMs - s.startMs),
      pctStart: durationMs > 0 ? (s.startMs / durationMs) * 100 : 0,
      pctWidth: durationMs > 0 ? Math.max(2, ((s.endMs - s.startMs) / durationMs) * 100) : 100,
    }))
  }, [model, baseTimestampMs, durationMs])

  // Real HTTP spans from telemetry
  const networkSpans = useMemo(() => {
    const spans: { id: string; method: string; url: string; statusCode: number; startMs: number; durationMs: number; pctStart: number; pctWidth: number }[] = []
    for (const ev of timelineEvents) {
      const attrs = (ev.attributes || {}) as Record<string, unknown>
      const method = typeof attrs.method === 'string' ? attrs.method.toUpperCase() : null
      const url = typeof attrs.url === 'string' ? attrs.url : typeof attrs.endpoint === 'string' ? attrs.endpoint : null
      if (!method && !url && ev.name !== 'network' && !ev.name?.startsWith('http')) continue

      const evTs = new Date(ev.event_timestamp).getTime()
      const startMs = !Number.isNaN(evTs) && evTs >= baseTimestampMs ? Math.min(durationMs, Math.max(0, evTs - baseTimestampMs)) : 0
      const latency = typeof attrs.latency_ms === 'number' ? attrs.latency_ms : typeof attrs.duration_ms === 'number' ? attrs.duration_ms : 300
      const statusCode = typeof attrs.status_code === 'number' ? attrs.status_code : typeof attrs.status === 'number' ? attrs.status : (ev.level === 'error' ? 500 : 200)

      spans.push({
        id: ev.id,
        method: method || 'HTTP',
        url: url || ev.message || '/api',
        statusCode,
        startMs,
        durationMs: latency,
        pctStart: durationMs > 0 ? (startMs / durationMs) * 100 : 0,
        pctWidth: durationMs > 0 ? Math.max(1.5, (latency / durationMs) * 100) : 2,
      })
    }
    return spans
  }, [timelineEvents, baseTimestampMs, durationMs])

  const [activeFilter, setActiveFilter] = useState<MarkerFilter>('all')
  const [followPlayhead, setFollowPlayhead] = useState(true)
  const [sideTab, setSideTab] = useState<SidePanelTab>('events')
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const fitRef = useRef<HTMLDivElement | null>(null)
  const [fit, setFit] = useState(1)

  const visibleMarkers = useMemo(() => filterMarkers(markers, activeFilter), [markers, activeFilter])

  const [timelineExpanded, setTimelineExpanded] = useState(() =>
    readBool(browserStorage(), 'artracker.replay.timelineExpanded', true),
  )
  const changeTimelineExpanded = (v: boolean) => {
    setTimelineExpanded(v)
    writeBool(browserStorage(), 'artracker.replay.timelineExpanded', v)
  }

  const trackDefs = useMemo<TrackDef[]>(() => {
    const defs: TrackDef[] = []
    if (screenSpans.length > 1) {
      defs.push({
        id: 'screens',
        label: 'Screens',
        kind: 'spans',
        spans: screenSpans.map((s) => ({ startMs: s.startMs, endMs: s.endMs, label: `${s.screen} (${formatDurationMs(s.durationMs)})` })),
      })
    }
    if (networkSpans.length > 0) {
      defs.push({
        id: 'network',
        label: 'Network',
        kind: 'spans',
        spans: networkSpans.map((n) => ({
          startMs: n.startMs,
          endMs: n.startMs + n.durationMs,
          label: `${n.method} ${n.url} (${n.statusCode}) · ${n.durationMs}ms`,
          tone: n.statusCode >= 400 ? ('error' as const) : undefined,
        })),
      })
    }
    if (perf && perf.samples.length > 0) {
      const at = (tMs: number) => perfValueAt(perf.samples, baseTimestampMs + tMs, perf.interval_ms)
      if (perf.metrics.includes('cpu')) {
        defs.push({
          id: 'cpu',
          label: 'CPU',
          kind: 'series',
          points: toSeries(perf.samples, 'cpu', baseTimestampMs),
          max: 100,
          unit: '%',
          threshold: 80,
          bands: perfBreaches.filter((b) => b.kind === 'high_cpu').map((b) => ({ startMs: b.startMs, endMs: b.endMs })),
          valueAt: (t) => {
            const v = at(t)?.cpu
            return v === undefined ? null : `${Math.round(v)}%`
          },
        })
      }
      if (perf.metrics.includes('mem_mb')) {
        defs.push({
          id: 'mem',
          label: 'RAM',
          kind: 'series',
          points: toSeries(perf.samples, 'mem_mb', baseTimestampMs),
          max: memTrackMax(perf.samples),
          unit: 'MB',
          valueAt: (t) => {
            const v = at(t)?.mem_mb
            return v === undefined ? null : `${Math.round(v)} MB`
          },
        })
      }
      if (perf.metrics.includes('fps')) {
        defs.push({
          id: 'fps',
          label: 'FPS',
          kind: 'series',
          points: toSeries(perf.samples, 'fps', baseTimestampMs),
          max: perf.device.refresh_hz ?? 60,
          threshold: (perf.device.refresh_hz ?? 60) * 0.5,
          bands: perfBreaches.filter((b) => b.kind === 'low_fps').map((b) => ({ startMs: b.startMs, endMs: b.endMs })),
          valueAt: (t) => {
            const v = at(t)?.fps
            return v === undefined ? null : String(Math.round(v))
          },
        })
      }
    }
    defs.push({
      id: 'events',
      label: 'Events',
      kind: 'markers',
      markers: visibleMarkers.map((m) => ({
        id: m.id,
        offsetMs: m.offsetMs,
        tone: m.tone,
        label: `${m.formattedTime} ${m.badgeLabel}: ${m.text}`,
      })),
    })
    return defs
  }, [screenSpans, networkSpans, visibleMarkers, perf, perfBreaches, baseTimestampMs])

  // Close the header ⋯ menu on an outside press or Escape
  useEffect(() => {
    if (!menuOpen) return
    const onDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenuOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [menuOpen])

  // Scale the device chassis to fit the stage: down when it is short, up (phones only) when it is tall
  useLayoutEffect(() => {
    const platform = effectivePlatform
    const stageEl = stageWrapRef.current
    const fitEl = fitRef.current
    if (!stageEl || !fitEl || typeof ResizeObserver === 'undefined') return
    const update = () => {
      const cs = getComputedStyle(stageEl)
      const padX = parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight)
      const padY = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom)
      const next = fitScale(
        { w: stageEl.clientWidth - padX, h: stageEl.clientHeight - padY },
        { w: fitEl.offsetWidth, h: fitEl.offsetHeight },
        // a phone may be enlarged to fill a tall stage; a web frame is already sized to the stage width
        platform === 'web' ? 1 : PHONE_MAX_SCALE,
      )
      setFit(Math.round(next * 1000) / 1000)
    }
    update()
    const ro = new ResizeObserver(update)
    ro.observe(stageEl)
    ro.observe(fitEl)
    return () => ro.disconnect()
  }, [effectivePlatform, model.kind, isScreenSession, isLoadingReplay, screenStagePx.height])

  // Mount the rrweb player once per web event set; resizes go through $set below (Review Focus #4)
  useEffect(() => {
    const target = mountEl
    if (!target || !webEvents) return
    let instance: rrwebPlayer | null = null

    try {
      instance = new rrwebPlayer({
        target,
        props: {
          events: webEvents,
          width: sizeRef.current.width,
          height: sizeRef.current.height,
          autoPlay: false,
          showController: false,
          skipInactive: true,
          speed: speedRef.current,
          speedOption: [1, 2, 4, 8],
        },
      })
      playerRef.current = instance
      // A re-created player (device chrome switch, new event set) resumes where the old one was
      const offset = Math.min(currentTimeRef.current, durationRef.current)
      if (offset > 0 || isPlayingRef.current) instance.goto(offset, isPlayingRef.current)

      instance.getReplayer()?.on('finish', () => {
        setIsPlaying(false)
        setCurrentTimeMs(durationRef.current)
      })
    } catch (err) {
      setChunkError(err instanceof Error ? err.message : 'Failed to initialize rrweb-player')
    }

    return () => {
      if (instance) {
        try {
          instance.pause()
          ;(instance as unknown as SvelteHandle).$destroy?.()
        } catch {
          // ignore teardown errors
        }
      }
      if (playerRef.current === instance) playerRef.current = null
    }
  }, [webEvents, mountEl])

  // Resize the existing rrweb player in place instead of rebuilding it
  useEffect(() => {
    const p = playerRef.current
    if (!p) return
    try {
      ;(p as unknown as SvelteHandle).$set?.({ width: playerWidth, height: playerHeight })
      p.triggerResize()
    } catch {
      // ignore if player not ready
    }
  }, [playerWidth, playerHeight])

  // Playback clock: rrweb's replayer drives web; a rAF clock drives mobile / telemetry
  useEffect(() => {
    if (!isPlaying) return
    let raf = 0
    let last = performance.now()
    const tick = (now: number) => {
      const dt = now - last
      last = now
      const replayer = playerRef.current?.getReplayer()
      const cur = replayer ? replayer.getCurrentTime() : currentTimeRef.current + dt * playbackSpeed
      const next = Math.min(durationRef.current, Math.max(0, Number.isNaN(cur) ? 0 : cur))
      setCurrentTimeMs(next)
      if (next >= durationRef.current) {
        setIsPlaying(false)
        return
      }
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [isPlaying, playbackSpeed])

  const seekTo = (offsetMs: number) => {
    const clamped = Math.max(0, Math.min(offsetMs, durationMs))
    setCurrentTimeMs(clamped)
    currentTimeRef.current = clamped
    if (playerRef.current) {
      try {
        playerRef.current.goto(clamped, isPlaying)
      } catch {
        // ignore if player not ready
      }
    }
  }

  const togglePlay = () => {
    if (currentTimeMs >= durationMs) {
      seekTo(0)
    }
    setIsPlaying((p) => {
      const next = !p
      if (playerRef.current) {
        try {
          if (next) playerRef.current.play()
          else playerRef.current.pause()
        } catch {
          // ignore
        }
      }
      return next
    })
  }

  const cycleSpeed = () => {
    const nextSpeed = playbackSpeed === 1 ? 2 : playbackSpeed === 2 ? 4 : 1
    setPlaybackSpeed(nextSpeed)
    if (playerRef.current) {
      try {
        playerRef.current.setSpeed(nextSpeed)
      } catch {
        // ignore
      }
    }
  }

  // Active marker synchronized to the scrubber playback timestamp
  const activeMarker = useMemo(() => {
    if (markers.length === 0) return null
    const past = markers.filter((m) => m.offsetMs <= currentTimeMs)
    return past.length > 0 ? past[past.length - 1] : markers[0]
  }, [markers, currentTimeMs])

  // Keyboard shortcuts. The handlers are re-created every render, so the single listener reads them via a ref.
  const hotkeyRef = useRef({ togglePlay, seekTo, cycleSpeed, markers, currentTimeMs })
  useEffect(() => {
    hotkeyRef.current = { togglePlay, seekTo, cycleSpeed, markers, currentTimeMs }
  })
  useEffect(() => {
    let swallowSpaceUp = false
    const onKeyDown = (e: KeyboardEvent) => {
      const action = resolveHotkey({
        key: e.key,
        ctrlKey: e.ctrlKey,
        metaKey: e.metaKey,
        altKey: e.altKey,
        target: e.target as HTMLElement | null,
      })
      if (!action) return
      const h = hotkeyRef.current
      if (action === 'toggle-play') {
        h.togglePlay()
        swallowSpaceUp = true
      } else if (action === 'seek-back') h.seekTo(h.currentTimeMs - SEEK_STEP_MS)
      else if (action === 'seek-forward') h.seekTo(h.currentTimeMs + SEEK_STEP_MS)
      else if (action === 'cycle-speed') h.cycleSpeed()
      else {
        const target = adjacentEventOffset(h.markers, h.currentTimeMs, action === 'next-event' ? 1 : -1)
        if (target !== null) h.seekTo(target)
      }
      e.preventDefault()
    }
    // A focused button would otherwise also "click" on Space release.
    const onKeyUp = (e: KeyboardEvent) => {
      if (e.key === ' ' && swallowSpaceUp) {
        swallowSpaceUp = false
        e.preventDefault()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('keyup', onKeyUp)
    }
  }, [])

  const notice = fallbackNotice(model)

  const renderViewportContent = () => {
    if (isLoadingReplay) return null

    // 1. Web DOM Replay (rrweb)
    if (model.kind === 'web') {
      return <div className="replay-rrweb-mount" ref={setMountEl} />
    }

    // 2a. Screenshot replay (artracker-screen-v1): the device's own pixels
    if (model.kind === 'screen') {
      const stage = screenStagePx
      return (
        <ScreenReplayRenderer
          session={model.session}
          currentTs={model.startMs + currentTimeMs}
          width={stage.width}
          height={stage.height}
        />
      )
    }

    // 2b. Mobile Visual Wireframe Replay (native view hierarchy from chunks or telemetry)
    if (model.kind === 'mobile') {
      return (
        <MobileWireframeRenderer
          frames={model.frames}
          touches={model.touches}
          currentTs={model.startMs + currentTimeMs}
          platform={model.platform}
          containerWidth={model.platform === 'ios' ? 308 : 300}
          maxHeight={486}
        />
      )
    }

    // 3. Fallback: High-Precision Telemetry Replay Canvas (when no visual snapshots exist)
    return (
      <div className="or-telemetry-canvas">
        {/* Top Screen Breadcrumb & Live Player Ticker */}
        {notice && (
          <div className="or-telemetry-canvas__notice" role="status">
            {notice}
          </div>
        )}
        <div className="or-telemetry-canvas__topbar">
          <div className="or-telemetry-canvas__breadcrumb">
            <span className="or-canvas-dot" />
            <span className="or-canvas-path">
              {displayPath}
            </span>
          </div>
          <div className="or-telemetry-canvas__status">
            <span className={`or-status-pill ${isPlaying ? 'or-status-pill--live' : ''}`}>
              {isPlaying ? '● PLAYING' : '⏸ PAUSED'}
            </span>
            <span className="or-canvas-clock">{formatOffsetMs(currentTimeMs)}</span>
          </div>
        </div>

        {/* Telemetry Execution Stage */}
        <div className="or-telemetry-canvas__body">
          {/* Active Event Inspection Card */}
          <div className="or-telemetry-active-card">
            <div className="or-telemetry-active-card__header">
              <div className="or-telemetry-active-card__type">
                <span className={`or-event-badge or-event-badge--${activeMarker?.tone || 'info'}`}>
                  {activeMarker?.badgeLabel ? activeMarker.badgeLabel.toUpperCase() : 'TELEMETRY'}
                </span>
                <span className="or-telemetry-active-card__time">
                  at {activeMarker ? activeMarker.formattedTime : formatOffsetMs(currentTimeMs)}
                </span>
              </div>
              {session.has_error && activeMarker?.tone === 'error' && (
                <span className="or-alert-tag or-alert-tag--error">Uncaught Error</span>
              )}
              {session.has_rage_click && activeMarker?.tone === 'warn' && (
                <span className="or-alert-tag or-alert-tag--warn">Frustration Signal</span>
              )}
            </div>

            <div className="or-telemetry-active-card__title">
              {activeMarker ? activeMarker.text : 'Session initialized · Telemetry monitoring active'}
            </div>

            <div className="or-telemetry-active-card__meta">
              <span>Environment: <strong>{session.environment || 'production'}</strong></span>
              <span>Platform: <strong>{detection.label}</strong></span>
              <span>Session: <strong>{formatSessionId(session.id, 12)}</strong></span>
            </div>
          </div>

          {/* Sequential Telemetry Journey Log */}
          <div className="or-telemetry-flow">
            <div className="or-telemetry-flow__label">Correlated Events in Session ({markers.length})</div>
            <div className="or-telemetry-flow__list">
              {markers.slice(0, 6).map((m) => {
                const isPassed = m.offsetMs <= currentTimeMs
                const isActive = activeMarker?.id === m.id
                return (
                  <div
                    key={m.id}
                    className={`or-telemetry-flow__item ${isActive ? 'is-active' : ''} ${isPassed ? 'is-passed' : ''}`}
                    onClick={() => seekTo(m.offsetMs)}
                  >
                    <span className={`or-flow-dot or-flow-dot--${m.tone}`} />
                    <span className="or-flow-time">{m.formattedTime}</span>
                    <span className="or-flow-text">{m.text}</span>
                  </div>
                )
              })}
            </div>
          </div>
        </div>

        {/* Bottom Technical HUD */}
        <div className="or-telemetry-canvas__hud">
          <div className="or-telemetry-hud__left">
            <span>Platform: <strong>{effectivePlatform.toUpperCase()}</strong></span>
            <span>Detection: <strong>{detection.source}</strong></span>
            <span>Chunks: <strong>{session.chunk_count}</strong></span>
          </div>
          <div className="or-telemetry-hud__right">
            <span>{timelineEvents.length} event{timelineEvents.length === 1 ? '' : 's'}</span>
            {session.has_error && (
              <span className="or-hud-error">
                {markers.filter((m) => m.tone === 'error').length || 1} error
              </span>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="replay-player-shell">
      <div className="or-theater or-workspace">
        {/* Unified Single Header (Replaces Staked Duplicate Headers) */}
        <div className="or-unified-header">
          <div className="or-unified-header__left">
            {onToggleQueue && (
              <button
                type="button"
                className={`or-queue-toggle${queueOpen ? ' is-active' : ''}`}
                aria-pressed={queueOpen}
                aria-label="Toggle session list"
                onClick={onToggleQueue}
              >
                ≡ Sessions
              </button>
            )}
            <strong className="or-unified-header__user" title={session.user_id || 'anonymous_user'}>
              {session.user_id || 'anonymous_user'}
            </strong>
            <span className="or-unified-header__id" title={session.id}>
              {formatSessionId(session.id, 14)}
            </span>
          </div>

          <div className="or-unified-header__right">
            {failedChunks > 0 && (
              <span className="or-res-chip or-res-chip--warn" title="Some replay chunks could not be downloaded or parsed">
                {failedChunks} chunk{failedChunks === 1 ? '' : 's'} could not be read
              </span>
            )}

            <span className="or-unified-header__duration">
              {formatDurationMs(durationMs)}
            </span>

            <div className="or-header-menu" ref={menuRef}>
              <button
                type="button"
                className="or-header-menu__btn"
                aria-label="Player options"
                aria-haspopup="true"
                aria-expanded={menuOpen}
                onClick={() => setMenuOpen((o) => !o)}
              >
                ⋯
              </button>
              {menuOpen && (
                <div className="or-header-menu__panel" role="menu" aria-label="Player options">
                  <div className="or-header-menu__row">
                    <span className="or-header-menu__label">Device viewport</span>
                    <div className="dash-seg" role="group" aria-label="Device viewport mode">
                      {(
                        [
                          { id: 'auto', label: 'Auto' },
                          { id: 'web', label: 'Web' },
                          { id: 'ios', label: 'iOS' },
                          { id: 'android', label: 'Android' },
                        ] as { id: DeviceMode; label: string }[]
                      ).map((m) => (
                        <button
                          key={m.id}
                          type="button"
                          className={`dash-seg__btn${deviceMode === m.id ? ' is-active' : ''}`}
                          aria-pressed={deviceMode === m.id}
                          onClick={() => onDeviceModeChange?.(m.id)}
                          title={m.id === 'auto' ? `Auto-detect: ${detection.label}` : `Switch to ${m.label}`}
                        >
                          {m.label}
                        </button>
                      ))}
                    </div>
                  </div>
                  <div className="or-header-menu__row">
                    <span className="or-header-menu__label">Recording</span>
                    <span className="or-res-chip" title="How this replay was recorded">
                      {replayModeLabel(model) ?? 'Standard'}
                    </span>
                  </div>
                  <div className="or-header-menu__row">
                    <span className="or-header-menu__label">Resolution</span>
                    <span className="or-res-chip">
                      {model.kind === 'web' && model.viewport
                        ? `${model.viewport.width} × ${model.viewport.height}`
                        : model.kind === 'screen' && model.session.index.frames[0]
                          ? `${model.session.index.frames[0].vw} × ${model.session.index.frames[0].vh}`
                          : effectivePlatform === 'ios'
                            ? '393 × 852'
                            : effectivePlatform === 'android'
                              ? '412 × 915'
                              : '1440 × 900'}
                    </span>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Dedicated Full-Width Route / Screen Location Strip */}
        <div className="or-stage-col">
        {effectivePlatform !== 'web' && (
          <div className="or-route-strip">
            <span className="or-route-strip__label">
              {strip.label}
            </span>
            <span
              className={`or-route-strip__path${strip.value === 'Not recorded' ? ' is-empty' : ''}`}
              title={activeScreen ?? recordedUrl ?? undefined}
            >
              {strip.value}
            </span>
          </div>
        )}

        {/* OpenReplay Studio Stage Canvas */}
        <div className="or-stage" ref={stageWrapRef}>
          {isLoadingReplay && (
            <div className="or-stage__loading" aria-label="Loading session replay">
              <div className="replay-loading-spinner" role="status">
                <div className="replay-spinner-ring" />
                <div className="replay-spinner-glow" />
              </div>
            </div>
          )}
          <div
            className={`or-stage__fit or-stage__fit--${effectivePlatform === 'web' ? 'web' : 'mobile'}`}
            ref={fitRef}
            style={{ transform: `scale(${fit})`, ...(isLoadingReplay ? { display: 'none' } : null) }}
          >
          {effectivePlatform === 'web' && (
            <div className="or-web-frame">
              <div className="or-web-frame__chrome">
                <div className="or-web-dots" aria-hidden="true">
                  <span className="or-web-dot or-web-dot--red" />
                  <span className="or-web-dot or-web-dot--yellow" />
                  <span className="or-web-dot or-web-dot--green" />
                </div>
                <div className="or-url-pill" title={recordedUrl ?? undefined}>
                  <svg viewBox="0 0 24 24" width="11" height="11" fill="none" stroke="currentColor" strokeWidth="2" style={{ marginRight: 5, opacity: 0.6 }}>
                    <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
                    <path d="M7 11V7a5 5 0 0 1 10 0v4" />
                  </svg>
                  {strip.value}
                </div>
              </div>
              {renderViewportContent()}
            </div>
          )}

          {effectivePlatform === 'ios' && (
            <div className={`or-phone-frame or-phone-frame--ios${isScreenSession ? ' or-phone-frame--auto' : ''}`}>
              <div className="or-phone-statusbar">
                <span title="Recorded device time">{clock}</span>
                <div className="or-phone-status-icons">
                  <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M5 12.55a11 11 0 0 1 14.08 0" />
                    <path d="M1.42 9a16 16 0 0 1 21.16 0" />
                    <path d="M8.53 16.11a6 6 0 0 1 6.95 0" />
                    <line x1="12" y1="20" x2="12.01" y2="20" />
                  </svg>
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
                    <rect x="2" y="7" width="16" height="10" rx="2" ry="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
                    <rect x="4" y="9" width="10" height="6" rx="1" ry="1" />
                    <path d="M20 11v2" />
                  </svg>
                </div>
              </div>
              {renderViewportContent()}
              <div className="or-phone-homebar">
                <div className="or-phone-pill" />
              </div>
            </div>
          )}

          {effectivePlatform === 'android' && (
            <div className={`or-phone-frame or-phone-frame--android${isScreenSession ? ' or-phone-frame--auto' : ''}`}>
              <div className="or-phone-camera-dot" />
              <div className="or-phone-statusbar">
                <span title="Recorded device time">{clock}</span>
                <div className="or-phone-status-icons">
                  <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M5 12.55a11 11 0 0 1 14.08 0" />
                    <path d="M1.42 9a16 16 0 0 1 21.16 0" />
                  </svg>
                  <svg viewBox="0 0 24 24" width="13" height="13" fill="currentColor">
                    <rect x="3" y="6" width="14" height="12" rx="2" ry="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
                    <path d="M19 11v2" />
                  </svg>
                </div>
              </div>
              {renderViewportContent()}
              <div className="or-phone-homebar">
                <div className="or-phone-pill" />
              </div>
            </div>
          )}
          </div>
        </div>
        </div>

        <ReplaySidePanel
          markers={markers}
          activeMarkerId={activeMarker?.id ?? null}
          activeFilter={activeFilter}
          onFilterChange={setActiveFilter}
          followPlayhead={followPlayhead}
          onFollowChange={setFollowPlayhead}
          onSeek={seekTo}
          tab={sideTab}
          onTabChange={setSideTab}
          performance={
            <PerformancePanel
              perf={perf ?? null}
              currentAbsTs={baseTimestampMs + currentTimeMs}
              startMs={baseTimestampMs}
              onSeek={seekTo}
            />
          }
        />

        {/* Unified Integrated Transport Bar & Multi-Lane Gantt Scrubber */}
        <div className="or-transport-bar">
          <div className="or-transport-bar__row">
            <div className="or-transport-bar__actions">
              <button
                type="button"
                className="or-transport-btn"
                onClick={togglePlay}
                aria-label={isPlaying ? 'Pause replay' : 'Play replay'}
                title={isPlaying ? 'Pause replay (Space)' : 'Play replay (Space)'}
              >
                <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="currentColor">
                  {isPlaying ? (
                    <path d="M4 2h3v12H4zM9 2h3v12H9z" />
                  ) : (
                    <path d="M4 2.5v11a.5.5 0 0 0 .76.43l9-5.5a.5.5 0 0 0 0-.86l-9-5.5A.5.5 0 0 0 4 2.5z" />
                  )}
                </svg>
              </button>

              <span className="or-transport-time">
                {formatOffsetMs(currentTimeMs)} <span className="or-transport-time__sep">/</span> {formatOffsetMs(durationMs)}
              </span>

              <button
                type="button"
                className="or-transport-speed-btn"
                onClick={cycleSpeed}
                title="Playback Speed"
              >
                {playbackSpeed}x
              </button>
            </div>
          </div>

          <TimelineTracks
            defs={trackDefs}
            durationMs={durationMs}
            currentMs={currentTimeMs}
            onSeek={seekTo}
            expanded={timelineExpanded}
            onExpandedChange={changeTimelineExpanded}
          />
        </div>
      </div>
    </div>
  )
}
