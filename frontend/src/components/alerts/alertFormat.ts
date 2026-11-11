import type { AlertBot, AlertBotMode, AlertChannel, AlertChannelsResponse, AlertDetailLevel, AlertIncident, AlertRule, AlertRuleKind, AlertRulePatch } from '../../api'

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

const CHAT_ID = /^-?[1-9][0-9]{4,19}$/
const CHAT_USERNAME = /^@[A-Za-z][A-Za-z0-9_]{4,31}$/
const BOT_TOKEN = /^[0-9]{5,15}:[A-Za-z0-9_-]{30,}$/

/** Mirrors the backend; surrounding whitespace is not allowed. */
export function validateChat(input: string): string | null {
  if (CHAT_ID.test(input) || CHAT_USERNAME.test(input)) return null
  return 'Enter a numeric chat id (e.g. -1001234567890) or a @channel_username.'
}

/**
 * 1 to 80 characters on one line. The label is not trimmed: surrounding spaces are accepted as
 * typed, but an all-whitespace label counts as empty. Like the backend, any control character
 * (NUL, newline, tab, DEL, C1) is rejected.
 */
export function validateLabel(input: string): string | null {
  if (input.trim() === '') return 'Enter a label.'
  if ([...input].length > 80) return 'Label must be at most 80 characters.'
  if (/\p{Cc}/u.test(input)) return 'The label cannot contain line breaks, tabs or control characters.'
  return null
}

/** The token is a secret: messages are fixed strings and never include the input. */
export function validateBotToken(input: string): string | null {
  if (BOT_TOKEN.test(input)) return null
  return 'That does not look like a bot token. Copy it from BotFather (e.g. 123456789:AA...).'
}

/** Banner for the alerts view; null when the bot is fine. */
export function botBannerText(bot: AlertBot, canManage: boolean): string | null {
  if (bot.mode === 'shared') return null
  if (!bot.configured) {
    return canManage
      ? 'Connect a Telegram bot in Settings to receive alerts.'
      : 'An organisation admin needs to connect a Telegram bot.'
  }
  if (bot.status === 'invalid') {
    const reason = bot.status_reason === '' ? 'Telegram rejected the token' : bot.status_reason
    return canManage
      ? `The Telegram bot stopped working: ${reason}. Reconnect it in Settings.`
      : `The Telegram bot stopped working: ${reason}. An organisation admin needs to reconnect it.`
  }
  return null
}

/** Why a channel is not receiving alerts in the current mode; null when it is active. */
export function channelActivityLabel(c: AlertChannel, mode: AlertBotMode): string | null {
  if (c.active !== false) return null
  if (mode === 'tenant' && c.origin === 'linked') return 'belongs to the shared bot'
  if (mode === 'shared' && c.origin === 'manual') return 'inactive in shared mode'
  return null
}

export function detailLevelLabel(level: AlertDetailLevel): string {
  return level === 'full' ? 'Full (includes error text)' : 'Minimal (no error text)'
}

export function canShowTest(c: AlertChannel): boolean {
  return c.active !== false
}

/** Channels that actually receive alerts. */
export function countActiveChannels(channels: AlertChannel[]): number {
  return channels.filter((c) => c.active !== false && c.enabled).length
}

/** The shared-bot link flow exists only when the server owns the bot. */
export function showLinkFlow(mode: AlertBotMode): boolean {
  return mode === 'shared'
}

export function botBadge(bot: AlertBot): { className: string; text: string } {
  if (bot.status === 'invalid') {
    return { className: 'badge--revoked', text: bot.status_reason === '' ? 'Invalid' : `Invalid: ${bot.status_reason}` }
  }
  return { className: 'badge--active', text: 'Active' }
}

type RawChannel = Omit<AlertChannel, 'origin' | 'destination' | 'active'> &
  Partial<Pick<AlertChannel, 'origin' | 'destination' | 'active'>>
export type RawChannelsResponse = Omit<AlertChannelsResponse, 'mode' | 'bot' | 'items'> & {
  mode?: AlertBotMode
  bot?: AlertBot
  items: RawChannel[]
}

/**
 * Fills the fields an older backend does not send: no mode means shared, no bot stays absent,
 * a channel without active is active, without origin is linked, without destination is empty.
 * A full payload is returned unchanged in value.
 */
export function normalizeChannelsResponse(raw: RawChannelsResponse): NormalizedChannels {
  const mode: AlertBotMode = raw.mode ?? 'shared'
  return {
    ...raw,
    mode,
    bot: raw.bot,
    items: raw.items.map((c) => ({
      ...c,
      origin: c.origin ?? 'linked',
      destination: c.destination ?? '',
      active: c.active ?? true,
    })),
  }
}

/** Like AlertChannelsResponse, but the bot is absent when the backend predates it. */
export type NormalizedChannels = Omit<AlertChannelsResponse, 'bot'> & { bot?: AlertBot }

/** The banner for a possibly absent bot. */
export function channelsBanner(data: NormalizedChannels, canManage: boolean): string | null {
  return data.bot ? botBannerText(data.bot, canManage) : null
}

/** The org bot response, or null when it is unusable (an older backend). */
export function normalizeBot(raw: Partial<AlertBot> | null | undefined): AlertBot | null {
  if (!raw || (raw.mode !== 'shared' && raw.mode !== 'tenant')) return null
  return {
    mode: raw.mode,
    configured: raw.configured ?? false,
    bot_username: raw.bot_username ?? '',
    status: raw.status ?? '',
    status_reason: raw.status_reason ?? '',
    detail_level: raw.detail_level ?? 'minimal',
    updated_at: raw.updated_at ?? null,
  }
}

/** True for the error an older backend gives for the missing bot endpoint. */
export function isBotEndpointMissing(err: unknown): boolean {
  return err instanceof Error && /^404\b/.test(err.message)
}
