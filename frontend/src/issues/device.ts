import type { DeviceInfo } from '../api'

export type EventContext = { device: string; os: string; app: string }

function str(d: DeviceInfo, ...keys: string[]): string {
  for (const k of keys) {
    const v = d[k]
    if (typeof v === 'string' && v.trim()) return v.trim()
  }
  return ''
}

/**
 * Display labels for one occurrence. Mirrors the server-side breakdown in
 * issues.go (issueEventContext) so per-event labels match the summary buckets.
 */
export function eventContext(d: DeviceInfo, releaseVersion: string): EventContext {
  const osName = str(d, 'os')
  const osVer = str(d, 'os_version', 'osVersion')
  let os = osName || osVer
  if (osName && osVer && !osName.includes(osVer)) os = `${osName} ${osVer}`

  const device = [str(d, 'manufacturer', 'brand'), str(d, 'model', 'device_model', 'device')]
    .filter(Boolean)
    .join(' ')

  return {
    device: device || 'Unknown',
    os: os || 'Unknown',
    app: releaseVersion || str(d, 'app_version', 'appVersion') || 'Unknown',
  }
}
