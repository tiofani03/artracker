const SCHEME_PREFIXES = ['android://', 'ios://']

/** Screen name as shown in the dashboard: the SDK's platform scheme (`android://MainActivity`) is dropped.
 * The raw name stays the identity for API calls; projects are single-platform, so nothing collides. */
export function displayScreen(name: string): string {
  for (const p of SCHEME_PREFIXES) {
    if (name.startsWith(p) && name.length > p.length) return name.slice(p.length)
  }
  return name
}

/** Case-insensitive search over the displayed name (the raw name contains the same text). */
export function screenMatches(name: string, query: string): boolean {
  const q = query.trim().toLowerCase()
  return !q || displayScreen(name).toLowerCase().includes(q)
}
