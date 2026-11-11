export type ReplayHotkeyAction =
  | 'toggle-play'
  | 'seek-back'
  | 'seek-forward'
  | 'prev-event'
  | 'next-event'
  | 'cycle-speed'

export type HotkeyInput = {
  key: string
  ctrlKey: boolean
  metaKey: boolean
  altKey: boolean
  target: { tagName?: string; isContentEditable?: boolean } | null
}

export const SEEK_STEP_MS = 5000

const TEXT_TAGS = new Set(['INPUT', 'TEXTAREA', 'SELECT'])
const KEY_ACTIONS: Record<string, ReplayHotkeyAction> = {
  ' ': 'toggle-play',
  ArrowLeft: 'seek-back',
  ArrowRight: 'seek-forward',
  '[': 'prev-event',
  ']': 'next-event',
  s: 'cycle-speed',
}

/** The player action a key press maps to, or null when it must be left alone (typing, browser shortcuts). */
export function resolveHotkey(e: HotkeyInput): ReplayHotkeyAction | null {
  if (e.ctrlKey || e.metaKey || e.altKey) return null
  const t = e.target
  if (t && (t.isContentEditable || (t.tagName && TEXT_TAGS.has(t.tagName.toUpperCase())))) return null
  return KEY_ACTIONS[e.key] ?? null
}

const NEXT_MIN_GAP_MS = 50
const PREV_MIN_GAP_MS = 500

/** Offset of the previous (-1) or next (1) event relative to the playhead, or null at either end. */
export function adjacentEventOffset(
  markers: { offsetMs: number }[],
  currentMs: number,
  dir: 1 | -1,
): number | null {
  let best: number | null = null
  for (const { offsetMs } of markers) {
    if (dir === 1 && offsetMs > currentMs + NEXT_MIN_GAP_MS && (best === null || offsetMs < best)) best = offsetMs
    if (dir === -1 && offsetMs < currentMs - PREV_MIN_GAP_MS && (best === null || offsetMs > best)) best = offsetMs
  }
  return best
}
