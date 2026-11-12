import type { IssueBucket } from '../api'

export type ShareRow = { label: string; count: number; pct: number }

/**
 * Turns breakdown buckets into percentage rows. Buckets past `limit`, plus any
 * events the server did not return a bucket for, are folded into "Other".
 */
export function toShareRows(buckets: IssueBucket[], total: number, limit = 5): ShareRow[] {
  if (total <= 0) return []
  const pct = (n: number) => Math.round((n / total) * 100)
  const top = buckets.slice(0, limit)
  const rows = top.map((b) => ({ label: b.value, count: b.count, pct: pct(b.count) }))
  const rest = total - top.reduce((sum, b) => sum + b.count, 0)
  if (rest > 0) rows.push({ label: 'Other', count: rest, pct: pct(rest) })
  return rows
}

function compareVersions(a: string, b: string): number {
  const pa = a.split(/[.-]/)
  const pb = b.split(/[.-]/)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? ''
    const y = pb[i] ?? ''
    const nx = Number(x)
    const ny = Number(y)
    const diff = Number.isNaN(nx) || Number.isNaN(ny) ? x.localeCompare(y) : nx - ny
    if (diff !== 0) return diff
  }
  return 0
}

/** "lowest – highest" of the known app versions, e.g. "2.4.0 – 2.5.1". */
export function versionRange(buckets: IssueBucket[]): string {
  const versions = buckets.map((b) => b.value).filter((v) => v && v !== 'Unknown')
  if (versions.length === 0) return '—'
  versions.sort(compareVersions)
  const lo = versions[0]
  const hi = versions[versions.length - 1]
  return lo === hi ? lo : `${lo} – ${hi}`
}
