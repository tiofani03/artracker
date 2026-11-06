import { useEffect, useMemo, useRef, useState } from 'react'
import { EventType, type eventWithTime } from 'rrweb'
import rrwebPlayer from 'rrweb-player'
import 'rrweb-player/dist/style.css'
import type { ReplayChunkDetail, ReplaySessionItem, TelemetryEvent } from '../api'
import { loadSessionChunks } from '../utils/replayChunkLoader'

export type TimelineMarker = {
  id: string
  offsetMs: number
  pct: number
  formattedTime: string
  tone: 'error' | 'warn'
  badgeLabel: string
  text: string
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
  onStartLiveCapture,
  isCapturingLive,
}: {
  session: ReplaySessionItem
  chunks: ReplayChunkDetail[]
  timelineEvents: TelemetryEvent[]
  overrideEvents?: eventWithTime[] | null
  loadingDetail?: boolean
  onStartLiveCapture?: () => void
  isCapturingLive?: boolean
}) {
  const stageWrapRef = useRef<HTMLDivElement | null>(null)
  const mountTargetRef = useRef<HTMLDivElement | null>(null)
  const playerRef = useRef<rrwebPlayer | null>(null)

  const [fetchedEvents, setFetchedEvents] = useState<eventWithTime[]>([])
  const [chunkLoading, setChunkLoading] = useState(false)
  const [chunkError, setChunkError] = useState<string | null>(null)
  const [currentTimeMs, setCurrentTimeMs] = useState(0)
  const [stageWidth, setStageWidth] = useState(720)

  const presignedChunkCount = useMemo(
    () => chunks.filter((c) => Boolean(c.url && c.url.trim())).length,
    [chunks],
  )

  // Observe player container width for responsive pixel-perfect scaling
  useEffect(() => {
    const el = stageWrapRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const updateWidth = () => {
      const w = Math.floor(el.clientWidth)
      if (w > 240) setStageWidth(w)
    }
    updateWidth()
    const ro = new ResizeObserver(updateWidth)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  // Fetch & gunzip R2 chunks when session chunks update
  useEffect(() => {
    if (overrideEvents && overrideEvents.length > 0) {
      setFetchedEvents([])
      setChunkError(null)
      setChunkLoading(false)
      return
    }

    if (presignedChunkCount === 0) {
      setFetchedEvents([])
      setChunkError(null)
      setChunkLoading(false)
      return
    }

    const controller = new AbortController()
    setChunkLoading(true)
    setChunkError(null)
    setCurrentTimeMs(0)

    loadSessionChunks(chunks, controller.signal)
      .then((loaded) => {
        if (controller.signal.aborted) return
        setFetchedEvents(loaded)
        setChunkLoading(false)
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setChunkError(err instanceof Error ? err.message : 'Failed to fetch R2 chunks')
        setFetchedEvents([])
        setChunkLoading(false)
      })

    return () => controller.abort()
  }, [session.id, chunks, presignedChunkCount, overrideEvents])

  const activeDomEvents = overrideEvents && overrideEvents.length > 0 ? overrideEvents : fetchedEvents

  const durationMs = useMemo(() => {
    if (activeDomEvents.length >= 2) {
      const span =
        activeDomEvents[activeDomEvents.length - 1].timestamp - activeDomEvents[0].timestamp
      if (span > 0) return span
    }
    return Math.max(session.duration_ms || 0, 1000)
  }, [activeDomEvents, session.duration_ms])

  const baseTimestampMs = useMemo(() => {
    if (activeDomEvents.length > 0) {
      return activeDomEvents[0].timestamp
    }
    const parsed = new Date(session.start_time).getTime()
    return Number.isNaN(parsed) ? Date.now() : parsed
  }, [activeDomEvents, session.start_time])

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

  // Inject custom events so rrweb-player's internal timeline also paints colored ticks
  const playerEventsWithTags = useMemo(() => {
    if (activeDomEvents.length < 2) return activeDomEvents
    const hasFullSnapshot = activeDomEvents.some((e) => e.type === EventType.FullSnapshot)
    if (!hasFullSnapshot) return []

    const startTs = activeDomEvents[0].timestamp
    const syntheticCustom: eventWithTime[] = markers.map((m) => ({
      type: EventType.Custom,
      data: {
        tag: m.tone,
        payload: { id: m.id, text: m.text },
      },
      timestamp: startTs + m.offsetMs,
    }))

    const combined = [...activeDomEvents, ...syntheticCustom]
    combined.sort((a, b) => a.timestamp - b.timestamp)
    return combined
  }, [activeDomEvents, markers])

  // Mount / unmount real rrwebPlayer instance
  useEffect(() => {
    const target = mountTargetRef.current
    if (!target) return
    target.innerHTML = ''
    playerRef.current = null

    if (playerEventsWithTags.length < 2) {
      return
    }

    const playerHeight = Math.max(300, Math.min(460, Math.round(stageWidth * 0.56)))
    let instance: rrwebPlayer | null = null

    try {
      instance = new rrwebPlayer({
        target,
        props: {
          events: playerEventsWithTags,
          width: stageWidth,
          height: playerHeight,
          autoPlay: false,
          showController: true,
          skipInactive: true,
          speedOption: [1, 2, 4, 8],
          tags: {
            error: '#ef4444',
            warn: '#f59e0b',
          },
        },
      })
      playerRef.current = instance

      instance.addEventListener('ui-update-current-time', (ev: unknown) => {
        const payload = ev as { payload?: number } | number
        const ms =
          typeof payload === 'number'
            ? payload
            : typeof payload?.payload === 'number'
              ? payload.payload
              : null
        if (ms !== null) {
          setCurrentTimeMs(Math.max(0, ms))
        }
      })
    } catch (err) {
      setChunkError(err instanceof Error ? err.message : 'Failed to initialize rrweb-player')
    }

    return () => {
      if (instance) {
        try {
          instance.pause()
          ;(instance as unknown as { $destroy?: () => void }).$destroy?.()
        } catch {
          // ignore teardown errors
        }
      }
      playerRef.current = null
      target.innerHTML = ''
    }
  }, [playerEventsWithTags, stageWidth])

  const seekTo = (offsetMs: number) => {
    setCurrentTimeMs(offsetMs)
    if (playerRef.current) {
      try {
        playerRef.current.goto(offsetMs, false)
      } catch {
        // ignore if player not ready
      }
    }
  }

  const progressPct =
    durationMs > 0 ? Math.min(100, Math.max(0, (currentTimeMs / durationMs) * 100)) : 0

  return (
    <div className="replay-player-shell">
      <div className="replay-player" ref={stageWrapRef}>
        <div className="replay-player__chrome">
          <div className="replay-player__dots" aria-hidden="true">
            <span />
            <span />
            <span />
          </div>
          <span className="replay-player__url">
            session/{session.id} · {chunks.length} chunk{chunks.length === 1 ? '' : 's'} (
            {(session.total_size_bytes / 1024).toFixed(1)} KB)
          </span>
          <span className="replay-player__mask">
            {overrideEvents && overrideEvents.length > 0
              ? 'Live DOM Capture · PII Masked'
              : 'rrweb R2 Stream · PII Masked'}
          </span>
        </div>

        {/* Interactive DOM Stage */}
        {loadingDetail || chunkLoading ? (
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
        ) : chunkError ? (
          <div className="replay-player__viewport replay-player__state" role="alert">
            <div className="replay-state-box">
              <strong className="replay-state-box__title">Unable to stream R2 replay chunks</strong>
              <p className="replay-state-box__desc">{chunkError}</p>
              {onStartLiveCapture && (
                <button
                  type="button"
                  className="dash-btn replay-state-box__btn"
                  onClick={onStartLiveCapture}
                  disabled={isCapturingLive}
                >
                  {isCapturingLive ? 'Recording browser DOM…' : 'Record Live Browser Session'}
                </button>
              )}
            </div>
          </div>
        ) : playerEventsWithTags.length >= 2 ? (
          <div className="replay-rrweb-mount" ref={mountTargetRef} />
        ) : (
          <div className="replay-player__viewport replay-player__state">
            <div className="replay-state-box">
              <strong className="replay-state-box__title">
                {chunks.length === 0
                  ? 'No DOM chunks uploaded to R2 for this session'
                  : 'R2 Object Storage presigned URLs unavailable'}
              </strong>
              <p className="replay-state-box__desc">
                {chunks.length === 0
                  ? 'Session metadata is stored in PostgreSQL, but no .gz rrweb chunks were uploaded for this session ID yet.'
                  : 'Configure R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY on the backend to sign chunk GET URLs, or record a live session right now.'}
              </p>
              {onStartLiveCapture && (
                <button
                  type="button"
                  className="dash-btn replay-state-box__btn"
                  onClick={onStartLiveCapture}
                  disabled={isCapturingLive}
                >
                  {isCapturingLive ? 'Recording 5s DOM interaction…' : 'Record Live 5s DOM Session'}
                </button>
              )}
            </div>
          </div>
        )}

        {/* Synchronized Timeline Marker Scrubber (Red = error log, Yellow = warn / click / frustration) */}
        <div className="replay-marker-bar" aria-label="Session timeline markers">
          <span className="replay-scrubber__time">{formatOffsetMs(currentTimeMs)}</span>
          <div className="replay-marker-track">
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
          <span className="replay-scrubber__time">{formatOffsetMs(durationMs)}</span>
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
