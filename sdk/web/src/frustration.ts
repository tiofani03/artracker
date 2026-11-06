export type RageClickSignal = {
  kind: 'rage_click'
  selector: string
  count: number
  windowMs: number
  x: number
  y: number
  timestamp: number
}

export type DeadClickSignal = {
  kind: 'dead_click'
  selector: string
  waitedMs: number
  x: number
  y: number
  timestamp: number
}

export type FrustrationOptions = {
  /** Sliding window in ms to detect rapid repeated clicks (default: 500ms) */
  rageWindowMs?: number
  /** Minimum clicks within rageWindowMs to flag a rage click (default: 3) */
  rageMinClicks?: number
  /** Max pointer distance in CSS pixels to treat clicks as targeting same spot (default: 30px) */
  rageRadiusPx?: number
  /** Observation window in ms after an interactive click to wait for DOM/network/nav (default: 1500ms) */
  deadClickWindowMs?: number
  onRageClick?: (signal: RageClickSignal) => void
  onDeadClick?: (signal: DeadClickSignal) => void
  /** Injectable clock & timer scheduling for deterministic unit testing */
  now?: () => number
  setTimer?: (fn: () => void, ms: number) => unknown
  clearTimer?: (handle: unknown) => void
}

export type ClickInput = {
  x: number
  y: number
  selector: string
  isInteractive: boolean
  timestamp?: number
}

type RecordedClick = {
  x: number
  y: number
  selector: string
  timestamp: number
}

type PendingDeadClick = {
  id: number
  selector: string
  x: number
  y: number
  timestamp: number
  timer: unknown
}

export type ChunkFrustrationFlags = {
  hasRageClick: boolean
  hasDeadClick: boolean
}

/**
 * Stateful frustration detector implementing:
 * 1. Rage Click: 3+ clicks within 500ms on the same element selector or within 30px radius.
 * 2. Dead Click: Interactive button/trigger click with no DOM mutation, network call,
 *    or URL navigation within 1500ms.
 */
export class FrustrationEngine {
  private readonly rageWindowMs: number
  private readonly rageMinClicks: number
  private readonly rageRadiusPx: number
  private readonly deadClickWindowMs: number
  private readonly onRageClick?: (signal: RageClickSignal) => void
  private readonly onDeadClick?: (signal: DeadClickSignal) => void
  private readonly nowFn: () => number
  private readonly setTimerFn: (fn: () => void, ms: number) => unknown
  private readonly clearTimerFn: (handle: unknown) => void

  private recentClicks: RecordedClick[] = []
  private pendingDeadClicks = new Map<number, PendingDeadClick>()
  private nextDeadId = 1

  private chunkRageFlag = false
  private chunkDeadFlag = false

  constructor(opts: FrustrationOptions = {}) {
    this.rageWindowMs = opts.rageWindowMs ?? 500
    this.rageMinClicks = opts.rageMinClicks ?? 3
    this.rageRadiusPx = opts.rageRadiusPx ?? 30
    this.deadClickWindowMs = opts.deadClickWindowMs ?? 1500
    this.onRageClick = opts.onRageClick
    this.onDeadClick = opts.onDeadClick
    this.nowFn = opts.now ?? (() => Date.now())
    this.setTimerFn = opts.setTimer ?? ((fn, ms) => setTimeout(fn, ms))
    this.clearTimerFn = opts.clearTimer ?? ((h) => clearTimeout(h as ReturnType<typeof setTimeout>))
  }

  recordClick(input: ClickInput): void {
    const ts = input.timestamp ?? this.nowFn()
    const cutoff = ts - this.rageWindowMs

    // Prune expired clicks outside the 500ms sliding window
    this.recentClicks = this.recentClicks.filter((c) => c.timestamp >= cutoff)
    this.recentClicks.push({
      x: input.x,
      y: input.y,
      selector: input.selector,
      timestamp: ts,
    })

    // Check Rage Click: 3+ clicks on same selector OR within coordinate radius
    const cluster = this.recentClicks.filter((c) => {
      if (input.selector && c.selector === input.selector) return true
      const dx = c.x - input.x
      const dy = c.y - input.y
      return Math.hypot(dx, dy) <= this.rageRadiusPx
    })

    if (cluster.length >= this.rageMinClicks) {
      this.chunkRageFlag = true
      const spanMs = Math.max(1, ts - cluster[0].timestamp)
      // Reset matched cluster so subsequent clicks start a new count instead of firing on every extra click
      const matchedSet = new Set(cluster)
      this.recentClicks = this.recentClicks.filter((c) => !matchedSet.has(c))

      this.onRageClick?.({
        kind: 'rage_click',
        selector: input.selector || 'document',
        count: cluster.length,
        windowMs: spanMs,
        x: input.x,
        y: input.y,
        timestamp: ts,
      })
    }

    // Arm Dead Click observer if target is an interactive element (button, link, etc.)
    if (input.isInteractive) {
      const id = this.nextDeadId++
      const timer = this.setTimerFn(() => {
        const pending = this.pendingDeadClicks.get(id)
        if (!pending) return
        this.pendingDeadClicks.delete(id)
        this.chunkDeadFlag = true
        this.onDeadClick?.({
          kind: 'dead_click',
          selector: pending.selector || 'interactive',
          waitedMs: this.deadClickWindowMs,
          x: pending.x,
          y: pending.y,
          timestamp: pending.timestamp,
        })
      }, this.deadClickWindowMs)

      this.pendingDeadClicks.set(id, {
        id,
        selector: input.selector,
        x: input.x,
        y: input.y,
        timestamp: ts,
        timer,
      })
    }
  }

  /** Resolves all pending dead-click checks because a UI/network/route reaction occurred. */
  notifyActivity(): void {
    for (const pending of this.pendingDeadClicks.values()) {
      this.clearTimerFn(pending.timer)
    }
    this.pendingDeadClicks.clear()
  }

  /** Returns current chunk flags without resetting them. */
  peekFlags(): ChunkFrustrationFlags {
    return {
      hasRageClick: this.chunkRageFlag,
      hasDeadClick: this.chunkDeadFlag,
    }
  }

  /** Consumes and resets per-chunk frustration flags when flushing a chunk. */
  consumeChunkFlags(): ChunkFrustrationFlags {
    const flags: ChunkFrustrationFlags = {
      hasRageClick: this.chunkRageFlag,
      hasDeadClick: this.chunkDeadFlag,
    }
    this.chunkRageFlag = false
    this.chunkDeadFlag = false
    return flags
  }

  dispose(): void {
    this.notifyActivity()
    this.recentClicks = []
  }
}

const INTERACTIVE_SELECTOR =
  'button, a[href], summary, [role="button"], [role="link"], [role="menuitem"], [role="tab"], input[type="button"], input[type="submit"], input[type="reset"]'

export function describeElementSelector(el: Element | null): string {
  if (!el) return 'unknown'
  const tag = el.tagName.toLowerCase()
  const id = el.id ? `#${el.id}` : ''
  let cls = ''
  if (typeof el.className === 'string' && el.className.trim()) {
    const tokens = el.className
      .trim()
      .split(/\s+/)
      .slice(0, 2)
      .filter(Boolean)
    if (tokens.length > 0) cls = `.${tokens.join('.')}`
  }
  return `${tag}${id}${cls}`
}

export function findInteractiveTarget(target: EventTarget | null): Element | null {
  if (!target || typeof (target as Element).closest !== 'function') return null
  const match = (target as Element).closest(INTERACTIVE_SELECTOR)
  if (!match) return null
  if (
    match.hasAttribute('disabled') ||
    match.getAttribute('aria-disabled') === 'true'
  ) {
    return null
  }
  return match
}

/**
 * Attaches browser DOM click, MutationObserver, network (fetch/XHR/PerformanceObserver),
 * and navigation listeners to feed a FrustrationEngine instance.
 */
export function attachFrustrationListeners(
  engine: FrustrationEngine,
  options: { ignoreSelector?: string } = {},
): () => void {
  if (typeof window === 'undefined' || typeof document === 'undefined') {
    return () => engine.dispose()
  }

  const onClick = (ev: MouseEvent) => {
    const rawTarget = ev.target instanceof Element ? ev.target : null
    if (options.ignoreSelector && rawTarget?.closest(options.ignoreSelector)) {
      return
    }
    const interactive = findInteractiveTarget(rawTarget)
    const targetForSelector = interactive ?? rawTarget
    engine.recordClick({
      x: ev.clientX,
      y: ev.clientY,
      selector: describeElementSelector(targetForSelector),
      isInteractive: interactive !== null,
    })
  }

  document.addEventListener('click', onClick, true)

  const mutationObs = new MutationObserver((records) => {
    // Ignore empty or purely rrweb-internal cursor/overlay mutations if any
    if (records.length > 0) {
      engine.notifyActivity()
    }
  })
  if (document.documentElement) {
    mutationObs.observe(document.documentElement, {
      childList: true,
      subtree: true,
      attributes: true,
      characterData: true,
    })
  }

  // Observe network requests via PerformanceObserver + fetch/XHR hook for immediate detection
  let perfObs: PerformanceObserver | null = null
  if (typeof PerformanceObserver !== 'undefined') {
    try {
      perfObs = new PerformanceObserver((list) => {
        if (list.getEntries().length > 0) {
          engine.notifyActivity()
        }
      })
      perfObs.observe({ type: 'resource', buffered: false })
    } catch {
      perfObs = null
    }
  }

  const origFetch = window.fetch
  if (typeof origFetch === 'function') {
    window.fetch = function patchedFetch(...args: Parameters<typeof fetch>) {
      engine.notifyActivity()
      return origFetch.apply(this, args)
    }
  }

  const origXhrSend =
    typeof XMLHttpRequest !== 'undefined' ? XMLHttpRequest.prototype.send : undefined
  if (origXhrSend) {
    XMLHttpRequest.prototype.send = function patchedSend(
      ...args: Parameters<XMLHttpRequest['send']>
    ) {
      engine.notifyActivity()
      return origXhrSend.apply(this, args)
    }
  }

  const onNav = () => engine.notifyActivity()
  window.addEventListener('popstate', onNav)
  window.addEventListener('hashchange', onNav)

  const origPushState = window.history.pushState
  const origReplaceState = window.history.replaceState
  window.history.pushState = function patchedPush(...args: Parameters<History['pushState']>) {
    engine.notifyActivity()
    return origPushState.apply(this, args)
  }
  window.history.replaceState = function patchedReplace(
    ...args: Parameters<History['replaceState']>
  ) {
    engine.notifyActivity()
    return origReplaceState.apply(this, args)
  }

  return () => {
    document.removeEventListener('click', onClick, true)
    mutationObs.disconnect()
    perfObs?.disconnect()
    if (typeof origFetch === 'function') {
      window.fetch = origFetch
    }
    if (origXhrSend && typeof XMLHttpRequest !== 'undefined') {
      XMLHttpRequest.prototype.send = origXhrSend
    }
    window.removeEventListener('popstate', onNav)
    window.removeEventListener('hashchange', onNav)
    window.history.pushState = origPushState
    window.history.replaceState = origReplaceState
    engine.dispose()
  }
}
