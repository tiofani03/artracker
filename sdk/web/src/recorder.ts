import { addCustomEvent, record, type eventWithTime } from 'rrweb'
import {
  attachFrustrationListeners,
  FrustrationEngine,
  type DeadClickSignal,
  type RageClickSignal,
} from './frustration.ts'
import { gzipString } from './gzip.ts'

export type ReplayRecorderConfig = {
  /** Base API URL, e.g. "https://art.exlm.my.id/api/v1" or "/api/v1" */
  endpoint: string
  /** Public client SDK key ("art_live_...") */
  apiKey: string
  /** Session identifier (8-128 alphanumeric/_/- chars). Auto-generated if omitted. */
  sessionId?: string
  /** Optional authenticated end-user identifier */
  userId?: string
  /** Deployment environment (default: "production") */
  environment?: string
  /** Mask all form inputs on client before serialization (default: true) */
  maskAllInputs?: boolean
  /** CSS class whose subtree should be blocked from DOM capture (default: "art-block") */
  blockClass?: string
  /** Flush interval in milliseconds (default: 10000ms) */
  flushIntervalMs?: number
  /** Max events buffered before immediate chunk flush (default: 200) */
  maxEventsPerChunk?: number
  /** Custom fetch implementation for testing */
  fetchImpl?: typeof fetch
  /** Custom rrweb record function for testing without real DOM */
  recordImpl?: (opts: { emit: (event: eventWithTime) => void }) => (() => void) | undefined
  onRageClick?: (signal: RageClickSignal) => void
  onDeadClick?: (signal: DeadClickSignal) => void
  onChunkFlushed?: (info: { chunkSeq: number; eventCount: number; sizeBytes: number }) => void
  onError?: (err: unknown) => void
}

export type ReplayRecorderHandle = {
  readonly sessionId: string
  flush: () => Promise<void>
  stop: () => Promise<void>
  markError: (message?: string) => void
}

function generateSessionId(): string {
  const rand =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID().replace(/-/g, '').slice(0, 16)
      : Math.random().toString(36).slice(2, 14) + Date.now().toString(36)
  return `sess_${rand}`
}

export function startReplayRecorder(config: ReplayRecorderConfig): ReplayRecorderHandle {
  const base = config.endpoint.replace(/\/+$/, '')
  const sessionId = config.sessionId ?? generateSessionId()
  const environment = config.environment ?? 'production'
  const flushIntervalMs = config.flushIntervalMs ?? 10_000
  const maxEventsPerChunk = config.maxEventsPerChunk ?? 200
  const fetchFn = config.fetchImpl ?? fetch

  const startedAtMs = Date.now()
  let firstEventTs: number | null = null
  let latestEventTs: number | null = null
  let chunkSeq = 0
  let chunkHasError = false
  let stopped = false

  let buffer: eventWithTime[] = []
  let telemetryQueue: Array<{
    id: string
    type: 'log' | 'event'
    name: string
    level?: string
    message?: string
    timestamp: string
    session_id: string
    user_id?: string
    attributes?: Record<string, unknown>
  }> = []

  const enqueueTelemetry = (item: {
    type: 'log' | 'event'
    name: string
    level?: string
    message?: string
    attributes?: Record<string, unknown>
  }) => {
    telemetryQueue.push({
      id: `${sessionId}_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
      type: item.type,
      name: item.name,
      level: item.level,
      message: item.message,
      timestamp: new Date().toISOString(),
      session_id: sessionId,
      user_id: config.userId,
      attributes: item.attributes,
    })
  }

  const frustration = new FrustrationEngine({
    onRageClick: (sig) => {
      try {
        addCustomEvent('rage-click', sig)
      } catch {
        // recorder may use custom recordImpl in unit tests
      }
      enqueueTelemetry({
        type: 'log',
        name: 'rage_click',
        level: 'warn',
        message: `Rage click ×${sig.count} on ${sig.selector} within ${sig.windowMs}ms`,
        attributes: { selector: sig.selector, count: sig.count, window_ms: sig.windowMs, x: sig.x, y: sig.y },
      })
      config.onRageClick?.(sig)
    },
    onDeadClick: (sig) => {
      try {
        addCustomEvent('dead-click', sig)
      } catch {
        // recorder may use custom recordImpl in unit tests
      }
      enqueueTelemetry({
        type: 'log',
        name: 'dead_click',
        level: 'warn',
        message: `Dead click on ${sig.selector} (no DOM mutation or network call in ${sig.waitedMs}ms)`,
        attributes: { selector: sig.selector, waited_ms: sig.waitedMs, x: sig.x, y: sig.y },
      })
      config.onDeadClick?.(sig)
    },
  })

  const detachFrustration = attachFrustrationListeners(frustration, {
    ignoreSelector: config.blockClass ? `.${config.blockClass}` : undefined,
  })

  const onWindowError = (ev: ErrorEvent) => {
    chunkHasError = true
    const msg = ev.message || 'Unhandled runtime error'
    try {
      addCustomEvent('error', { message: msg })
    } catch {
      // ignore if custom recordImpl
    }
    enqueueTelemetry({
      type: 'log',
      name: 'runtime_error',
      level: 'error',
      message: msg,
    })
  }

  const onUnhandledRejection = (ev: PromiseRejectionEvent) => {
    chunkHasError = true
    const msg =
      ev.reason instanceof Error ? ev.reason.message : String(ev.reason ?? 'Unhandled rejection')
    try {
      addCustomEvent('error', { message: msg })
    } catch {
      // ignore if custom recordImpl
    }
    enqueueTelemetry({
      type: 'log',
      name: 'unhandled_rejection',
      level: 'error',
      message: msg,
    })
  }

  if (typeof window !== 'undefined') {
    window.addEventListener('error', onWindowError)
    window.addEventListener('unhandledrejection', onUnhandledRejection)
  }

  let flushChain: Promise<void> = Promise.resolve()

  const flushOnce = async (): Promise<void> => {
    // Flush correlated telemetry events first (best effort)
    if (telemetryQueue.length > 0) {
      const items = telemetryQueue
      telemetryQueue = []
      void fetchFn(`${base}/ingest`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Artracker-Key': config.apiKey,
        },
        body: JSON.stringify({
          sent_at: new Date().toISOString(),
          sdk: { name: '@artracker/web-sdk', version: '0.1.0' },
          context: { environment },
          items,
        }),
      }).catch(() => undefined)
    }

    if (buffer.length === 0) return

    const eventsBatch = buffer
    buffer = []

    const seq = chunkSeq++
    const flags = frustration.consumeChunkFlags()
    const hasError = chunkHasError
    chunkHasError = false

    const durationMs =
      firstEventTs !== null && latestEventTs !== null
        ? Math.max(0, latestEventTs - firstEventTs)
        : Math.max(0, Date.now() - startedAtMs)

    try {
      const gzBytes = await gzipString(JSON.stringify(eventsBatch))
      const sizeBytes = gzBytes.byteLength

      // 1. Request presigned PUT URL
      const urlRes = await fetchFn(`${base}/replay/chunk-url`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Artracker-Key': config.apiKey,
        },
        body: JSON.stringify({
          session_id: sessionId,
          chunk_seq: seq,
          size_bytes: sizeBytes,
          user_id: config.userId ?? '',
          environment,
          ...(seq === 0 ? { start_time: new Date(firstEventTs ?? startedAtMs).toISOString() } : {}),
        }),
      })
      if (!urlRes.ok) {
        throw new Error(`chunk-url failed: ${urlRes.status}`)
      }
      const { upload_url: uploadUrl } = (await urlRes.json()) as {
        upload_url: string
        object_key: string
      }

      // 2. Upload compressed .gz stream directly to Cloudflare R2 / S3
      const putRes = await fetchFn(uploadUrl, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/gzip',
        },
        body: gzBytes as unknown as BodyInit,
      })
      if (!putRes.ok) {
        throw new Error(`R2 presigned PUT failed: ${putRes.status}`)
      }

      // 3. Acknowledge chunk upload + aggregate frustration & error flags
      const ackRes = await fetchFn(`${base}/replay/chunk-ack`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Artracker-Key': config.apiKey,
        },
        body: JSON.stringify({
          session_id: sessionId,
          chunk_seq: seq,
          size_bytes: sizeBytes,
          duration_ms: durationMs,
          event_count: eventsBatch.length,
          has_error: hasError,
          has_rage_click: flags.hasRageClick,
          has_dead_click: flags.hasDeadClick,
        }),
      })
      if (!ackRes.ok) {
        throw new Error(`chunk-ack failed: ${ackRes.status}`)
      }

      config.onChunkFlushed?.({
        chunkSeq: seq,
        eventCount: eventsBatch.length,
        sizeBytes,
      })
    } catch (err) {
      config.onError?.(err)
    }
  }

  const flush = (): Promise<void> => {
    flushChain = flushChain.then(flushOnce, flushOnce)
    return flushChain
  }

  const onEmit = (event: eventWithTime) => {
    if (stopped) return
    if (firstEventTs === null || event.timestamp < firstEventTs) {
      firstEventTs = event.timestamp
    }
    if (latestEventTs === null || event.timestamp > latestEventTs) {
      latestEventTs = event.timestamp
    }
    buffer.push(event)
    if (buffer.length >= maxEventsPerChunk) {
      void flush()
    }
  }

  const stopRrweb = config.recordImpl
    ? config.recordImpl({ emit: onEmit })
    : record({
        emit: onEmit,
        maskAllInputs: config.maskAllInputs ?? true,
        blockClass: config.blockClass ?? 'art-block',
      })

  const intervalId = setInterval(() => {
    void flush()
  }, flushIntervalMs)

  const onVisibilityChange = () => {
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
      void flush()
    }
  }
  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', onVisibilityChange)
  }

  return {
    sessionId,
    flush,
    markError(message?: string) {
      chunkHasError = true
      if (message) {
        enqueueTelemetry({
          type: 'log',
          name: 'captured_error',
          level: 'error',
          message,
        })
      }
    },
    async stop() {
      if (stopped) return
      stopped = true
      clearInterval(intervalId)
      stopRrweb?.()
      detachFrustration()
      if (typeof window !== 'undefined') {
        window.removeEventListener('error', onWindowError)
        window.removeEventListener('unhandledrejection', onUnhandledRejection)
      }
      if (typeof document !== 'undefined') {
        document.removeEventListener('visibilitychange', onVisibilityChange)
      }
      await flush()
    },
  }
}
