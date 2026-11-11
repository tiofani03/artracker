import type { AlertChannel, AlertIncident, AlertRule, AlertRuleKind, AlertRulePatch } from '../../api'

export const SPIKE_WINDOWS = [15, 30, 60, 180] as const

const TITLES: Record<AlertRuleKind, string> = {
  new_issue: 'New issue',
  regression: 'Regression',
  issue_spike: 'Issue spike',
}

export function ruleTitle(kind: AlertRuleKind): string {
  return TITLES[kind]
}

/** One-line summary of a saved rule. */
export function ruleDescription(rule: AlertRule): string {
  const scope = rule.environment === '' ? 'all environments' : rule.environment
  switch (rule.kind) {
    case 'new_issue':
      return `The first time an error is seen · ${scope}`
    case 'regression':
      return `A resolved issue fails again and is reopened · ${scope}`
    case 'issue_spike':
      return `≥ ${rule.spike_min_count} events and ${rule.spike_multiplier}× the usual rate within ${rule.spike_window_minutes} min · ${scope}`
  }
}

function inRange(n: number, min: number, max: number): boolean {
  return Number.isFinite(n) && n >= min && n <= max
}

/** Mirrors the backend limits; returns a message or null when the patch is acceptable. */
export function validateRulePatch(kind: AlertRuleKind, patch: AlertRulePatch): string | null {
  if (Object.keys(patch).length === 0) return 'Nothing to save.'
  if (patch.environment !== undefined && patch.environment.length > 64) {
    return 'Environment must be at most 64 characters.'
  }
  const spikeFieldSet =
    patch.spike_multiplier !== undefined ||
    patch.spike_min_count !== undefined ||
    patch.spike_window_minutes !== undefined ||
    patch.cooldown_minutes !== undefined
  if (spikeFieldSet && kind !== 'issue_spike') return 'Spike settings only apply to the Issue spike rule.'
  if (patch.spike_multiplier !== undefined && !inRange(patch.spike_multiplier, 1.5, 100)) {
    return 'Multiplier must be between 1.5 and 100.'
  }
  if (
    patch.spike_min_count !== undefined &&
    !(Number.isInteger(patch.spike_min_count) && inRange(patch.spike_min_count, 1, 100000))
  ) {
    return 'Minimum events must be a whole number between 1 and 100000.'
  }
  if (
    patch.spike_window_minutes !== undefined &&
    !(SPIKE_WINDOWS as readonly number[]).includes(patch.spike_window_minutes)
  ) {
    return 'Window must be 15, 30, 60 or 180 minutes.'
  }
  if (
    patch.cooldown_minutes !== undefined &&
    !(Number.isInteger(patch.cooldown_minutes) && inRange(patch.cooldown_minutes, 0, 1440))
  ) {
    return 'Cooldown must be a whole number between 0 and 1440 minutes.'
  }
  return null
}

/** Mirrors the backend formatBaseline: one decimal, no trailing zeros. */
export function formatBaseline(n: number): string {
  return String(Math.round(n * 10) / 10)
}

export function incidentHeadline(incident: AlertIncident): string {
  if (incident.rule_kind === 'issue_spike' && incident.spike) {
    const { count, baseline, window_minutes } = incident.spike
    return `${count} events in ${window_minutes} min (usual ${formatBaseline(baseline)})`
  }
  return incident.title
}

/** Delivery state worth showing next to an incident; null when it was sent or the state is unknown. */
export function notifyLabel(state: string): string | null {
  switch (state) {
    case 'pending':
      return 'sending…'
    case 'skipped':
      return 'not sent (no channel or Telegram off)'
    case 'failed':
      return 'delivery failed'
    default:
      return null
  }
}

/**
 * True when a link code was used: a channel id appeared, or a channel that was disabled is
 * enabled again (re-linking an existing chat re-enables it instead of adding a row).
 */
export function detectLinked(before: AlertChannel[], after: AlertChannel[]): boolean {
  const was = new Map(before.map((c) => [c.id, c.enabled]))
  return after.some((c) => !was.has(c.id) || (was.get(c.id) === false && c.enabled))
}

export function countEnabledChannels(channels: AlertChannel[]): number {
  return channels.filter((c) => c.enabled).length
}

export function timeAgo(iso: string, now: Date): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return 'just now'
  const minutes = Math.floor((now.getTime() - then) / 60_000)
  if (minutes < 1) return 'just now'
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} h ago`
  return `${Math.floor(hours / 24)} d ago`
}

/** mm:ss for a remaining duration in ms, rounded up, clamped at zero. */
export function formatCountdown(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

/** Editable copy of a rule; numbers are strings so a half-typed value is not coerced. */
export type RuleDraft = {
  enabled: boolean
  environment: string
  multiplier: string
  minCount: string
  window: string
  cooldown: string
}

export function draftFromRule(rule: AlertRule): RuleDraft {
  return {
    enabled: rule.enabled,
    environment: rule.environment,
    multiplier: String(rule.spike_multiplier),
    minCount: String(rule.spike_min_count),
    window: String(rule.spike_window_minutes),
    cooldown: String(rule.cooldown_minutes),
  }
}

/** Only the fields that differ from the saved rule. Spike fields are ignored for other kinds. */
export function buildRulePatch(rule: AlertRule, draft: RuleDraft): AlertRulePatch {
  const patch: AlertRulePatch = {}
  if (draft.enabled !== rule.enabled) patch.enabled = draft.enabled
  const env = draft.environment.trim()
  if (env !== rule.environment) patch.environment = env
  if (rule.kind !== 'issue_spike') return patch
  const num = (s: string) => (s.trim() === '' ? Number.NaN : Number(s))
  if (num(draft.multiplier) !== rule.spike_multiplier) patch.spike_multiplier = num(draft.multiplier)
  if (num(draft.minCount) !== rule.spike_min_count) patch.spike_min_count = num(draft.minCount)
  if (num(draft.window) !== rule.spike_window_minutes) patch.spike_window_minutes = num(draft.window)
  if (num(draft.cooldown) !== rule.cooldown_minutes) patch.cooldown_minutes = num(draft.cooldown)
  return patch
}
