import type { FlowPathsResponse, FlowScreen } from '../api'
import { displayScreen } from '../screens/name'
import { isSelectableNode, OTHER_SCREEN } from './view'

/** One screen directly before or after the focused screen. share is of the focused screen's sessions (0..1). */
export type Neighbour = { screen: string; sessions: number; share: number }

/** Screen of a node id "<step>:<screen>" (the screen itself may contain colons). */
function nodeScreen(id: string): { step: number; screen: string } {
  const i = id.indexOf(':')
  return { step: Number(id.slice(0, i)), screen: id.slice(i + 1) }
}

/**
 * The screens one step away from the focused screen, from a 1-step /flow/paths response:
 * real screens by sessions (descending), then Other, then the Exit/Start terminal.
 */
export function neighbours(res: FlowPathsResponse | null): Neighbour[] {
  if (!res || !(res.sessions > 0)) return []
  const out: Neighbour[] = []
  for (const l of res.links) {
    if (nodeScreen(l.source).step !== 0 || !(l.sessions > 0)) continue
    out.push({ screen: nodeScreen(l.target).screen, sessions: l.sessions, share: l.sessions / res.sessions })
  }
  const rank = (s: string) => (isSelectableNode(s) ? 0 : s === OTHER_SCREEN ? 1 : 2)
  return out.sort((a, b) => rank(a.screen) - rank(b.screen) || b.sessions - a.sessions || a.screen.localeCompare(b.screen))
}

/** Human name for a neighbour: the synthetic nodes get words, real screens drop their platform scheme. */
export function neighbourLabel(screen: string): string {
  if (screen === 'Exit') return 'Left the app'
  if (screen === 'Start') return 'Session start'
  if (screen === OTHER_SCREEN) return 'Other screens'
  return displayScreen(screen)
}

export type SortKey = 'screen' | 'sessions' | 'median' | 'exit'
export type SortDir = 'asc' | 'desc'

/** part/total as 0..1, 0 when total is not positive. */
export function rateOf(part: number, total: number): number {
  return total > 0 ? part / total : 0
}

/** Sorted copy of the screen table; ties keep the displayed name order. */
export function sortScreens(rows: FlowScreen[], key: SortKey, dir: SortDir): FlowScreen[] {
  const name = (r: FlowScreen) => displayScreen(r.screen).toLowerCase()
  const value = (r: FlowScreen): number =>
    key === 'sessions' ? r.sessions : key === 'median' ? r.median_ms : rateOf(r.exit_sessions, r.sessions)
  const sign = dir === 'asc' ? 1 : -1
  return [...rows].sort((a, b) => {
    const byKey = key === 'screen' ? name(a).localeCompare(name(b)) : value(a) - value(b)
    return sign * byKey || name(a).localeCompare(name(b))
  })
}

/** Colour band for an exit rate: half or more of the sessions leave → high, a quarter → mid. */
export function exitTone(rate: number): 'high' | 'mid' | 'low' {
  if (rate >= 0.5) return 'high'
  if (rate >= 0.25) return 'mid'
  return 'low'
}

/** Heatmap page preselected on this screen (raw name, as the API knows it). */
export function heatmapHref(screen: string): string {
  return `/dashboard/heatmap?screen=${encodeURIComponent(screen)}`
}
