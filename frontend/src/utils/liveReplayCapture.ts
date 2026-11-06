import { addCustomEvent, record, type eventWithTime } from 'rrweb'
import { BASE, type TelemetryEvent } from '../api'

export type LiveCaptureResult = {
  sessionId: string
  events: eventWithTime[]
  timelineEvents: TelemetryEvent[]
  uploadedToR2: boolean
  hasError: boolean
  hasRageClick: boolean
  hasDeadClick: boolean
}

async function gzipText(input: string): Promise<Uint8Array> {
  const bytes = new TextEncoder().encode(input)
  const cs = new CompressionStream('gzip')
  const writer = cs.writable.getWriter()
  void writer.write(bytes)
  void writer.close()
  const out = await new Response(cs.readable).arrayBuffer()
  return new Uint8Array(out)
}

/**
 * Records a short interactive browser session using rrweb + client-side Rage Click (3+ clicks
 * in 500ms) & Dead Click (interactive click with no DOM/network activity in 1500ms) detection.
 * Attempts presigned R2 upload if apiKey is provided, and returns captured events for instant playback.
 */
export function startInteractiveCapture(opts: {
  apiKey?: string | null
  userId?: string
  environment?: string
}): {
  sessionId: string
  stop: () => Promise<LiveCaptureResult>
} {
  const rand =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID().replace(/-/g, '').slice(0, 14)
      : Math.random().toString(36).slice(2, 12)
  const sessionId = `sess_${rand}`
  const events: eventWithTime[] = []
  const timelineEvents: TelemetryEvent[] = []

  let hasError = false
  let hasRageClick = false
  let hasDeadClick = false

  // Sliding 500ms window for rage clicks
  let recentClicks: Array<{ ts: number; x: number; y: number; selector: string }> = []
  const pendingDeadTimers = new Map<number, ReturnType<typeof setTimeout>>()
  let nextDeadId = 1

  const clearPendingDeadClicks = () => {
    for (const timer of pendingDeadTimers.values()) {
      clearTimeout(timer)
    }
    pendingDeadTimers.clear()
  }

  const pushTimeline = (kind: 'event' | 'log', name: string, level: string, message: string) => {
    timelineEvents.push({
      id: `${sessionId}_${timelineEvents.length + 1}`,
      kind,
      name,
      level,
      message,
      session_id: sessionId,
      user_id: opts.userId ?? 'console_operator',
      environment: opts.environment ?? 'production',
      release_version: '1.0.0',
      event_timestamp: new Date().toISOString(),
      attributes: {},
    })
  }

  const onClick = (ev: MouseEvent) => {
    const el = ev.target instanceof Element ? ev.target : null
    if (!el || el.closest('[data-replay-recorder-ignore="true"]')) return

    const tag = el.tagName.toLowerCase()
    const id = el.id ? `#${el.id}` : ''
    const selector = `${tag}${id}`
    const now = Date.now()

    recentClicks = recentClicks.filter((c) => c.ts >= now - 500)
    recentClicks.push({ ts: now, x: ev.clientX, y: ev.clientY, selector })

    const cluster = recentClicks.filter(
      (c) => c.selector === selector || Math.hypot(c.x - ev.clientX, c.y - ev.clientY) <= 30,
    )
    if (cluster.length >= 3) {
      hasRageClick = true
      const span = Math.max(1, now - cluster[0].ts)
      recentClicks = []
      try {
        addCustomEvent('warn', { kind: 'rage_click', selector, count: cluster.length })
      } catch {
        // ignore if recorder already stopped
      }
      pushTimeline(
        'log',
        'rage_click',
        'warn',
        `Rage click ×${cluster.length} on ${selector} within ${span}ms`,
      )
    }

    const interactive = el.closest(
      'button, a[href], summary, [role="button"], input[type="button"], input[type="submit"]',
    )
    if (interactive && !interactive.hasAttribute('disabled')) {
      const deadId = nextDeadId++
      const timer = setTimeout(() => {
        pendingDeadTimers.delete(deadId)
        hasDeadClick = true
        try {
          addCustomEvent('warn', { kind: 'dead_click', selector })
        } catch {
          // ignore
        }
        pushTimeline(
          'log',
          'dead_click',
          'warn',
          `Dead click on ${selector} (no DOM mutation or network call in 1.5s)`,
        )
      }, 1500)
      pendingDeadTimers.set(deadId, timer)
    } else {
      pushTimeline('event', 'ui_click', 'info', `Clicked ${selector}`)
    }
  }

  const mutationObs = new MutationObserver(() => clearPendingDeadClicks())
  if (document.documentElement) {
    mutationObs.observe(document.documentElement, {
      childList: true,
      subtree: true,
      attributes: true,
      characterData: true,
    })
  }

  let perfObs: PerformanceObserver | null = null
  if (typeof PerformanceObserver !== 'undefined') {
    try {
      perfObs = new PerformanceObserver((list) => {
        if (list.getEntries().length > 0) clearPendingDeadClicks()
      })
      perfObs.observe({ type: 'resource', buffered: false })
    } catch {
      perfObs = null
    }
  }

  const onError = (ev: ErrorEvent) => {
    hasError = true
    const msg = ev.message || 'Unhandled runtime error'
    try {
      addCustomEvent('error', { message: msg })
    } catch {
      // ignore
    }
    pushTimeline('log', 'runtime_error', 'error', msg)
  }

  document.addEventListener('click', onClick, true)
  window.addEventListener('error', onError)

  const stopRecord = record({
    emit(event) {
      events.push(event)
    },
    maskAllInputs: true,
    blockClass: 'art-block',
  })

  return {
    sessionId,
    async stop(): Promise<LiveCaptureResult> {
      stopRecord?.()
      document.removeEventListener('click', onClick, true)
      window.removeEventListener('error', onError)
      mutationObs.disconnect()
      perfObs?.disconnect()
      clearPendingDeadClicks()

      let uploadedToR2 = false
      if (opts.apiKey && events.length >= 2) {
        try {
          const gz = await gzipText(JSON.stringify(events))
          const firstTs = events[0].timestamp
          const lastTs = events[events.length - 1].timestamp
          const durationMs = Math.max(100, lastTs - firstTs)

          const urlRes = await fetch(`${BASE}/replay/chunk-url`, {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              'X-Artracker-Key': opts.apiKey,
            },
            body: JSON.stringify({
              session_id: sessionId,
              chunk_seq: 0,
              size_bytes: gz.byteLength,
              user_id: opts.userId ?? 'console_operator',
              environment: opts.environment ?? 'production',
              start_time: new Date(firstTs).toISOString(),
            }),
          })
          if (urlRes.ok) {
            const { upload_url: uploadUrl } = (await urlRes.json()) as { upload_url: string }
            const putRes = await fetch(uploadUrl, {
              method: 'PUT',
              headers: { 'Content-Type': 'application/gzip' },
              body: gz as unknown as BodyInit,
            })
            if (putRes.ok) {
              const ackRes = await fetch(`${BASE}/replay/chunk-ack`, {
                method: 'POST',
                headers: {
                  'Content-Type': 'application/json',
                  'X-Artracker-Key': opts.apiKey,
                },
                body: JSON.stringify({
                  session_id: sessionId,
                  chunk_seq: 0,
                  size_bytes: gz.byteLength,
                  duration_ms: durationMs,
                  event_count: events.length,
                  has_error: hasError,
                  has_rage_click: hasRageClick,
                  has_dead_click: hasDeadClick,
                }),
              })
              uploadedToR2 = ackRes.ok
            }
          }
        } catch {
          uploadedToR2 = false
        }
      }

      return {
        sessionId,
        events,
        timelineEvents,
        uploadedToR2,
        hasError,
        hasRageClick,
        hasDeadClick,
      }
    },
  }
}
