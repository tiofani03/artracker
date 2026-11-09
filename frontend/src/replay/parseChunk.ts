/**
 * Decodes one replay chunk: gunzips when the gzip magic (0x1f 0x8b) is present (the CDN or
 * browser may already have decoded it via Content-Encoding), then parses JSON.
 * Returns null on any failure so callers can skip unreadable chunks.
 */
export async function parseChunkBytes(buf: ArrayBuffer): Promise<unknown | null> {
  try {
    const bytes = new Uint8Array(buf)
    if (bytes.byteLength === 0) return null
    let text: string
    if (bytes.byteLength >= 2 && bytes[0] === 0x1f && bytes[1] === 0x8b) {
      const stream = new Response(buf).body!.pipeThrough(new DecompressionStream('gzip'))
      text = await new Response(stream).text()
    } else {
      text = new TextDecoder().decode(bytes)
    }
    return JSON.parse(text) as unknown
  } catch {
    return null
  }
}

/** Download-chunk proxy URLs are rewritten to same-origin paths so the dev proxy / nginx serves them. */
export function chunkFetchUrl(rawUrl: string): string {
  try {
    const u = new URL(rawUrl)
    if (u.pathname.includes('/api/v1/replay/download-chunk')) return `${u.pathname}${u.search}`
  } catch {
    // relative or malformed: use as-is
  }
  return rawUrl
}

/**
 * Fetches and parses every URL in order. One failed chunk becomes `null` and is counted;
 * the load only rejects when every chunk fails.
 */
export async function fetchChunkPayloads(
  urls: string[],
  signal?: AbortSignal,
): Promise<{ payloads: unknown[]; failed: number }> {
  const results = await Promise.all(
    urls.map(async (url) => {
      try {
        const res = await fetch(chunkFetchUrl(url), { signal })
        if (!res.ok) return null
        return await parseChunkBytes(await res.arrayBuffer())
      } catch (err) {
        if (signal?.aborted) throw err
        return null
      }
    }),
  )
  const failed = results.filter((r) => r === null).length
  if (urls.length > 0 && failed === urls.length) {
    throw new Error(`All ${urls.length} replay chunk(s) failed to download or parse`)
  }
  return { payloads: results, failed }
}
