/** Display helpers for People profiles and session device context (Android + iOS session_start keys). */

type Ctx = Record<string, unknown>

const str = (v: unknown): string => {
  if (v === undefined || v === null || v === '') return ''
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

/** Keys that are plumbing, not facts about the device. */
const HIDDEN_KEYS = new Set(['session_id', 'launch_type'])

/** Keys consumed by the known rows below (both SDK spellings). */
const KNOWN_KEYS = new Set([
  'platform',
  'os_name',
  'os_version',
  'api_level',
  'model',
  'device_model',
  'manufacturer',
  'app_version',
  'app_build',
  'build_id',
  'locale',
  'tz',
  'timezone',
  'network',
  'is_emulator',
  'sdk_version',
])

function osName(ctx: Ctx): string {
  const raw = str(ctx.os_name) || str(ctx.platform)
  switch (raw.toLowerCase()) {
    case 'android':
      return 'Android'
    case 'ios':
      return 'iOS'
    case 'web':
      return 'Web'
    default:
      return raw
  }
}

function device(ctx: Ctx): string {
  const model = str(ctx.model) || str(ctx.device_model)
  const maker = str(ctx.manufacturer)
  if (!model) return maker
  if (!maker || maker.toLowerCase() === 'apple' || model.toLowerCase().startsWith(maker.toLowerCase())) return model
  return `${maker} ${model}`
}

function os(ctx: Ctx): string {
  return [osName(ctx), str(ctx.os_version)].filter(Boolean).join(' ')
}

function app(ctx: Ctx): string {
  const v = str(ctx.app_version)
  const b = str(ctx.app_build)
  if (v && b && !v.includes(b)) return `${v} (${b})`
  return v || b
}

/** Ordered label/value rows for a session's device context. Unknown keys follow, sorted by key. */
export function contextRows(ctx: Ctx): { label: string; value: string }[] {
  const rows: { label: string; value: string }[] = []
  const push = (label: string, value: string) => {
    if (value) rows.push({ label, value })
  }
  push('Device', device(ctx))
  push('OS', os(ctx))
  push('App', app(ctx))
  push('Locale', str(ctx.locale))
  push('Timezone', str(ctx.tz) || str(ctx.timezone))
  push('Network', str(ctx.network))
  if (ctx.is_emulator === true) push('Emulator', 'Yes')
  push('SDK', str(ctx.sdk_version))
  for (const key of Object.keys(ctx).sort()) {
    if (KNOWN_KEYS.has(key) || HIDDEN_KEYS.has(key)) continue
    push(key, str(ctx[key]))
  }
  return rows
}

/** One line such as "Pixel 8 · Android 14 · v2.3.1 (231)". */
export function deviceSummary(ctx: Ctx): string {
  const model = str(ctx.model) || str(ctx.device_model)
  const a = app(ctx)
  const parts = [model, os(ctx), a ? `v${a}` : ''].filter(Boolean)
  return parts.length ? parts.join(' · ') : 'Unknown device'
}

/** Trait key/value pairs sorted by key, values stringified, at most `max` entries. */
export function traitEntries(traits: Record<string, unknown>, max = Infinity): [string, string][] {
  return Object.keys(traits)
    .sort()
    .slice(0, max)
    .map((k) => [k, str(traits[k])])
}

export function peopleLink(userId: string): string {
  return `/dashboard/people?user=${encodeURIComponent(userId)}`
}

export function sessionReplayLink(sessionId: string): string {
  return `/dashboard/replays?session=${encodeURIComponent(sessionId)}`
}
