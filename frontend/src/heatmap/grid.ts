import type { HeatmapCell, HeatmapResponse } from '../api'

export const MIN_RELIABLE_SESSIONS = 20

export function cellShare(cell: HeatmapCell, screenSessions: number): number {
  if (screenSessions <= 0) return 0
  return cell.sessions / screenSessions
}

export function cellAt(res: HeatmapResponse, xNorm: number, yNorm: number): HeatmapCell | null {
  if (res.cols <= 0 || res.rows <= 0) return null
  if (!(xNorm >= 0 && xNorm <= 1 && yNorm >= 0 && yNorm <= 1)) return null
  const c = Math.min(res.cols - 1, Math.floor(xNorm * res.cols))
  const r = Math.min(res.rows - 1, Math.floor(yNorm * res.rows))
  return res.cells.find((cell) => cell.c === c && cell.r === r) ?? null
}

export function shareLabel(share: number): string {
  if (share <= 0) return '0%'
  const pct = Math.round(share * 100)
  return pct < 1 ? '<1%' : `${pct}%`
}
