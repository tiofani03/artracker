import type { TelemetryEvent } from '../api'

/** Buckets a telemetry event into the tone/label/category the replay timeline and session timeline show. */
export function classifyMarker(ev: TelemetryEvent): {
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
