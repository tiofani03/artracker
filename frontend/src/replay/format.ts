/** mm:ss for a session length. */
export function formatDurationMs(ms: number): string {
  const totalSec = Math.max(0, Math.round(ms / 1000))
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

export function formatSessionId(id: string, maxLen = 16): string {
  if (!id) return ''
  if (id.length <= maxLen) return id
  const half = Math.floor((maxLen - 3) / 2)
  return `${id.slice(0, half + 2)}…${id.slice(-half)}`
}
