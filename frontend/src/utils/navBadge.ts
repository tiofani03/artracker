/** Sidebar badge text for a count: hidden when empty, capped at "99+". */
export function formatNavBadge(count: number | undefined): string | undefined {
  if (count === undefined || !Number.isFinite(count) || count <= 0) return undefined
  return count > 99 ? '99+' : String(Math.floor(count))
}
