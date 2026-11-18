export const MAX_EXCLUDED = 200
export const MAX_NAME_LEN = 200

/** Trim entries, drop empties and duplicates (first occurrence wins). */
export function normalizeExcluded(list: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of list) {
    const s = raw.trim()
    if (!s || seen.has(s)) continue
    seen.add(s)
    out.push(s)
  }
  return out
}
