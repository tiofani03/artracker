/** Minimal slice of `Storage`, so tests can pass an in-memory fake and callers can pass nothing. */
export type KV = Pick<Storage, 'getItem' | 'setItem'> | null

/** `window.localStorage`, or null when the browser blocks it (private mode, disabled site data). */
export function browserStorage(): KV {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

export function readBool(kv: KV, key: string, fallback: boolean): boolean {
  try {
    const v = kv?.getItem(key)
    if (v === 'true') return true
    if (v === 'false') return false
  } catch {
    // blocked storage: fall through to the default
  }
  return fallback
}

export function writeBool(kv: KV, key: string, value: boolean): void {
  try {
    kv?.setItem(key, value ? 'true' : 'false')
  } catch {
    // preference is a convenience; losing it is fine
  }
}

export function readStringSet(kv: KV, key: string): Set<string> {
  try {
    const raw = kv?.getItem(key)
    if (!raw) return new Set()
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.filter((x): x is string => typeof x === 'string'))
  } catch {
    return new Set()
  }
}

export function writeStringSet(kv: KV, key: string, value: Set<string>): void {
  try {
    kv?.setItem(key, JSON.stringify([...value]))
  } catch {
    // preference is a convenience; losing it is fine
  }
}

/** The session queue rail starts open only when there is room for rail + stage + side panel. */
export function defaultQueueOpen(viewportW: number): boolean {
  return viewportW >= 1600
}
