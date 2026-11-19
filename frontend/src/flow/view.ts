import type { FlowScreen } from '../api'

export type ScreenRef = { screen: string; platform: string }

export const TERMINAL_SCREENS = ['Exit', 'Start']
export const OTHER_SCREEN = 'Other'

export function screenKey(s: ScreenRef): string {
  return `${s.platform}|${s.screen}`
}

/** Exit/Start are drop-off terminals, not real screens. */
export function isTerminal(screen: string): boolean {
  return TERMINAL_SCREENS.includes(screen)
}

/** Nodes that can become the selected screen: real screens only. */
export function isSelectableNode(screen: string): boolean {
  return !isTerminal(screen) && screen !== OTHER_SCREEN
}

/** Formats part/total as a whole percentage; a non-zero share below 1% reads "<1%". */
export function percentLabel(part: number, total: number): string {
  if (!(total > 0) || !(part > 0)) return '0%'
  const pct = (part / total) * 100
  if (pct < 1) return '<1%'
  return `${Math.round(pct)}%`
}

/** Share of a screen's sessions that ended on it. */
export function exitRateLabel(s: Pick<FlowScreen, 'exit_sessions' | 'sessions'>): string {
  return percentLabel(s.exit_sessions, s.sessions)
}

/** Hover text for a Sankey link; the share is of the sessions that reached the step-0 screen. */
export function linkLabel(sessions: number, stepZeroSessions: number): string {
  return `${sessions} ${sessions === 1 ? 'session' : 'sessions'} (${percentLabel(sessions, stepZeroSessions)})`
}

/** The screen with the most entry sessions (ties: more sessions, then list order). */
export function defaultScreen(screens: FlowScreen[]): FlowScreen | null {
  let best: FlowScreen | null = null
  for (const s of screens) {
    if (!best || s.entry_sessions > best.entry_sessions || (s.entry_sessions === best.entry_sessions && s.sessions > best.sessions)) {
      best = s
    }
  }
  return best
}

/**
 * The screen the path explorer shows: the user's pick while it is still in the list,
 * otherwise the default. A pick made by clicking a node is kept as long as it is listed.
 */
export function resolveSelection(screens: FlowScreen[], picked: ScreenRef | null): ScreenRef | null {
  if (picked && screens.some((s) => screenKey(s) === screenKey(picked))) return picked
  const d = defaultScreen(screens)
  return d ? { screen: d.screen, platform: d.platform } : null
}

/** For direction=prev, step 0 goes on the right so the chart reads left to right in time. */
export function mirrorX(x: number, width: number, nodeWidth: number): number {
  return width - x - nodeWidth
}

/**
 * SVG path for a link band between two nodes whose left edges are at sx and tx. The band
 * leaves the side of the source that faces the target, so it works in either direction.
 * Drawn as a stroke whose width is the band width.
 */
export function linkPath(sx: number, sy: number, tx: number, ty: number, nodeWidth: number): string {
  const forward = sx <= tx
  const x0 = forward ? sx + nodeWidth : sx
  const x1 = forward ? tx : tx + nodeWidth
  const mx = (x0 + x1) / 2
  return `M${x0},${sy} C${mx},${sy} ${mx},${ty} ${x1},${ty}`
}

/** Shortens a label to max characters with a trailing ellipsis. */
export function truncate(text: string, max: number): string {
  if (text.length <= max) return text
  return `${text.slice(0, Math.max(0, max - 1))}…`
}
