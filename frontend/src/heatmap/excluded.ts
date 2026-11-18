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

/** Names in `next` (normalized) that are not in `saved`: adding these deletes existing heatmap data. */
export function addedScreens(saved: string[], next: string[]): string[] {
  return normalizeExcluded(next).filter((s) => !saved.includes(s))
}

/** True when both lists hold the same names after normalization, ignoring order. */
export function sameExcluded(a: string[], b: string[]): boolean {
  const x = normalizeExcluded(a)
  const y = normalizeExcluded(b)
  return x.length === y.length && x.every((s) => y.includes(s))
}
