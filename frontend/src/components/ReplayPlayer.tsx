import { useEffect, useLayoutEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { EventType, type eventWithTime } from 'rrweb'
import rrwebPlayer from 'rrweb-player'
import 'rrweb-player/dist/style.css'
import type { Platform, ReplayChunkDetail, ReplaySessionItem, TelemetryEvent } from '../api'
import { loadChunkPayloads } from '../utils/replayChunkLoader'
import { buildReplayModel } from '../replay/normalize'
import type { ReplayModel } from '../replay/model'
import { activeFrameAt, deviceClock, fallbackNotice, offsetFromPointer, routeStrip } from '../replay/playback'
import { chunkSignature } from '../replay/wireframe'
import { detectSessionPlatform, type DetectedPlatform } from '../utils/platformDetector'
import { MobileWireframeRenderer } from './MobileWireframeRenderer'

export type TimelineMarker = {
  id: string
  offsetMs: number
  pct: number
  formattedTime: string
  tone: 'error' | 'warn'
  badgeLabel: string
  text: string
}

export type DeviceMode = 'auto' | 'web' | 'ios' | 'android'

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

function classifyMarker(ev: TelemetryEvent): { tone: 'error' | 'warn'; badgeLabel: string } {
  const lvl = (ev.level || '').toLowerCase()
  const name = (ev.name || '').toLowerCase()
  const msg = (ev.message || '').toLowerCase()

  if (
    lvl === 'error' ||
    lvl === 'fatal' ||
    name.includes('error') ||
    name.includes('exception') ||
    name.includes('anr') ||
    name.includes('hang') ||
    msg.includes('anr') ||
    msg.includes('hang') ||
    msg.includes('typeerror') ||
    msg.includes('502') ||
    msg.includes('500')
  ) {
    if (name.includes('anr') || msg.includes('anr')) {
      return { tone: 'error', badgeLabel: 'anr' }
    }
    if (name.includes('hang') || msg.includes('hang')) {
      return { tone: 'error', badgeLabel: 'ui hang' }
    }
    return { tone: 'error', badgeLabel: 'error' }
  }
  if (name === 'rage_tap' || msg.includes('rage tap')) {
    return { tone: 'warn', badgeLabel: 'rage tap' }
  }
  if (name === 'rage_click' || msg.includes('rage click')) {
    return { tone: 'warn', badgeLabel: 'rage click' }
  }
  if (name === 'dead_click' || msg.includes('dead click')) {
    return { tone: 'warn', badgeLabel: 'dead click' }
  }
  if (lvl === 'warn' || lvl === 'warning') {
    return { tone: 'warn', badgeLabel: 'warn' }
  }
  return { tone: 'warn', badgeLabel: ev.kind === 'event' ? 'event' : 'log' }
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
}: {
  session: ReplaySessionItem
  chunks: ReplayChunkDetail[]
  timelineEvents: TelemetryEvent[]
  overrideEvents?: eventWithTime[] | null
  loadingDetail?: boolean
  projectPlatform?: Platform
  deviceMode?: DeviceMode
  onDeviceModeChange?: (mode: DeviceMode) => void
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

  // Fetch & gunzip chunks; interpretation happens in buildReplayModel
  useEffect(() => {
    setCurrentTimeMs(0)
    setIsPlaying(false)
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

    loadChunkPayloads(chunksRef.current, controller.signal)
      .then((r) => {
        if (controller.signal.aborted) return
        setPayloads(r.payloads)
        setFailedChunks(r.failed)
        setChunkLoading(false)
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setChunkError(err instanceof Error ? err.message : 'Failed to fetch replay chunks')
        setPayloads([])
        setFailedChunks(0)
        setChunkLoading(false)
      })

    return () => controller.abort()
  }, [session.id, chunkSig, presignedChunkCount, hasOverride])

  // Single source of truth for what can be rendered
  const model: ReplayModel = useMemo(() => {
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
  }, [hasOverride, overrideEvents, payloads, timelineEvents, projectPlatform, chunkError, chunks.length, presignedChunkCount])

  const webEvents = model.kind === 'web' ? model.events : null

  const effectivePlatform: DetectedPlatform =
    deviceMode !== 'auto'
      ? deviceMode
      : model.kind === 'mobile'
        ? model.platform
        : model.kind === 'web'
          ? 'web'
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

  const playerWidth = effectivePlatform === 'ios' ? 322 : effectivePlatform === 'android' ? 312 : stageWidth
  const playerHeight =
    effectivePlatform === 'ios'
      ? 520
      : effectivePlatform === 'android'
        ? 500
        : Math.max(340, Math.min(520, Math.round(stageWidth * 0.5625)))

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
    for (const ev of webEvents ?? []) {
      if (ev.type === EventType.Meta && (ev.data as { href?: string })?.href) {
        return (ev.data as { href?: string }).href!
      }
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
  }, [webEvents, timelineEvents])

  // Active mobile screen at the playhead (frames come from chunks or telemetry view trees)
  const activeScreen =
    model.kind === 'mobile' ? (activeFrameAt(model.frames, model.startMs + currentTimeMs)?.screen ?? null) : null

  // Only recorded values: no invented URLs / activity names when nothing was captured
  const strip = routeStrip({ platform: effectivePlatform, screen: activeScreen, recordedUrl })
  const clock = deviceClock(baseTimestampMs + currentTimeMs) ?? '--:--'

  const displayPath = useMemo(() => {
    if (!recordedUrl) return strip.value
    try {
      const u = new URL(recordedUrl)
      return `${u.pathname}${u.search}` || '/'
    } catch {
      return recordedUrl
    }
  }, [recordedUrl, strip.value])

  // Scrubber: click or drag anywhere on the track to seek
  const scrubbingRef = useRef(false)
  const seekToPointer = (e: ReactPointerEvent<HTMLDivElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    seekTo(offsetFromPointer(e.clientX, { left: r.left, width: r.width }, durationMs))
  }

  // Build timeline markers (red = error, yellow = warn / click / frustration)
  const markers: TimelineMarker[] = useMemo(() => {
    if (timelineEvents.length === 0) return []
    return timelineEvents.map((ev, idx) => {
      const evTs = new Date(ev.event_timestamp).getTime()
      let offsetMs: number
      if (!Number.isNaN(evTs) && evTs >= baseTimestampMs && evTs <= baseTimestampMs + durationMs) {
        offsetMs = evTs - baseTimestampMs
      } else if (!Number.isNaN(evTs) && evTs > baseTimestampMs) {
        offsetMs = Math.min(durationMs, evTs - baseTimestampMs)
      } else {
        // Fallback proportional placement when DB clocks differ slightly from chunk monotonic timestamps
        offsetMs = Math.round(((idx + 1) / (timelineEvents.length + 1)) * durationMs)
      }

      const pct = durationMs > 0 ? Math.min(99, Math.max(1, (offsetMs / durationMs) * 100)) : 50
      const { tone, badgeLabel } = classifyMarker(ev)
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
      }
    })
  }, [timelineEvents, baseTimestampMs, durationMs])

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

  const progressPct =
    durationMs > 0 ? Math.min(100, Math.max(0, (currentTimeMs / durationMs) * 100)) : 0

  // Active marker synchronized to the scrubber playback timestamp
  const activeMarker = useMemo(() => {
    if (markers.length === 0) return null
    const past = markers.filter((m) => m.offsetMs <= currentTimeMs)
    return past.length > 0 ? past[past.length - 1] : markers[0]
  }, [markers, currentTimeMs])

  const notice = fallbackNotice(model)

  const renderViewportContent = () => {
    if (loadingDetail || chunkLoading) {
      return (
        <div className="replay-player__viewport replay-player__state">
          <div className="replay-state-box">
            <strong className="replay-state-box__title">
              {chunkLoading
                ? `Fetching & decompressing ${presignedChunkCount} gzipped R2 chunk(s)…`
                : 'Loading session metadata…'}
            </strong>
            <p className="replay-state-box__desc">
              Streaming DOM snapshots and reconstructing pixel-accurate replay in worker.
            </p>
          </div>
        </div>
      )
    }

    // 1. Web DOM Replay (rrweb)
    if (model.kind === 'web') {
      return <div className="replay-rrweb-mount" ref={setMountEl} />
    }

    // 2. Mobile Visual Wireframe Replay (native view hierarchy from chunks or telemetry)
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
      <div className="or-theater" ref={stageWrapRef}>
        {/* Unified Single Header (Replaces Staked Duplicate Headers) */}
        <div className="or-unified-header">
          <div className="or-unified-header__left">
            <span className="or-user-avatar" aria-hidden="true">
              {(session.user_id || 'U')[0].toUpperCase()}
            </span>
            <strong className="or-unified-header__user" title={session.user_id || 'anonymous_user'}>
              {session.user_id || 'anonymous_user'}
            </strong>
            <span className="or-unified-header__id" title={session.id}>
              {formatSessionId(session.id, 14)}
            </span>
          </div>

          <div className="or-unified-header__right">
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

            <span className="or-res-chip">
              {effectivePlatform === 'ios'
                ? '393 × 852'
                : effectivePlatform === 'android'
                  ? '412 × 915'
                  : '1440 × 900'}
            </span>

            {failedChunks > 0 && (
              <span className="or-res-chip or-res-chip--warn" title="Some replay chunks could not be downloaded or parsed">
                {failedChunks} chunk{failedChunks === 1 ? '' : 's'} could not be read
              </span>
            )}

            <span className="or-unified-header__duration">
              {formatDurationMs(durationMs)}
            </span>
          </div>
        </div>

        {/* Dedicated Full-Width Route / Screen Location Strip */}
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

        {/* OpenReplay Studio Stage Canvas */}
        <div className="or-stage">
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
            <div className="or-phone-frame or-phone-frame--ios">
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
            <div className="or-phone-frame or-phone-frame--android">
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

        {/* Integrated Modern Transport Controls Bar */}
        <div className="or-transport-bar">
          <button
            type="button"
            className="or-transport-btn"
            onClick={togglePlay}
            aria-label={isPlaying ? 'Pause replay' : 'Play replay'}
            title={isPlaying ? 'Pause replay (Space)' : 'Play replay (Space)'}
          >
            {isPlaying ? '⏸' : '▶'}
          </button>

          <span className="or-transport-time">
            {formatOffsetMs(currentTimeMs)} / {formatOffsetMs(durationMs)}
          </span>

          <div
            className="replay-marker-track"
            style={{ flex: 1, touchAction: 'none' }}
            role="slider"
            aria-label="Seek replay"
            aria-valuemin={0}
            aria-valuemax={Math.round(durationMs / 1000)}
            aria-valuenow={Math.round(currentTimeMs / 1000)}
            onPointerDown={(e) => {
              if ((e.target as HTMLElement).closest('.replay-marker-dot')) return
              scrubbingRef.current = true
              e.currentTarget.setPointerCapture(e.pointerId)
              seekToPointer(e)
            }}
            onPointerMove={(e) => {
              if (scrubbingRef.current) seekToPointer(e)
            }}
            onPointerUp={(e) => {
              scrubbingRef.current = false
              e.currentTarget.releasePointerCapture(e.pointerId)
            }}
          >
            <div className="replay-marker-track__rail" />
            <div
              className="replay-marker-track__fill"
              style={{ width: `${progressPct}%` }}
            />
            {markers.map((m) => (
              <button
                key={m.id}
                type="button"
                className={`replay-marker-dot replay-marker-dot--${m.tone}`}
                style={{ left: `${m.pct}%` }}
                aria-label={`${m.formattedTime} ${m.badgeLabel}: ${m.text}`}
                onClick={() => seekTo(m.offsetMs)}
              >
                <span className="replay-marker-tooltip" aria-hidden="true">
                  <strong>{m.formattedTime}</strong> · {m.text}
                </span>
              </button>
            ))}
          </div>

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

      {/* Legend & Correlated Timeline Events */}
      <div className="replay-timeline-header">
        <span className="replay-timeline-header__title">
          Correlated Timeline ({markers.length} event{markers.length === 1 ? '' : 's'})
        </span>
        <div className="replay-marker-legend" aria-hidden="true">
          <span className="replay-marker-legend__item">
            <span className="replay-legend-dot replay-legend-dot--error" /> Error log
          </span>
          <span className="replay-marker-legend__item">
            <span className="replay-legend-dot replay-legend-dot--warn" /> Warn / Click / Frustration
          </span>
        </div>
      </div>

      {markers.length === 0 ? (
        <p className="dash-empty" style={{ padding: '1rem 0', margin: 0 }}>
          No correlated telemetry events recorded for session <code className="dash-code">{session.id}</code>.
        </p>
      ) : (
        <div className="dash-list">
          {markers.map((m) => {
            const isNear = Math.abs(currentTimeMs - m.offsetMs) <= Math.max(1500, durationMs * 0.05)
            return (
              <button
                key={m.id}
                type="button"
                className={`dash-step-row${isNear ? ' is-near' : ''}`}
                onClick={() => seekTo(m.offsetMs)}
              >
                <span className="dash-code">{m.formattedTime}</span>
                <span
                  className={`badge ${
                    m.tone === 'error' ? 'badge--exception' : 'badge--console'
                  }`}
                >
                  {m.badgeLabel}
                </span>
                <span className="dash-step-row__text">{m.text}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
