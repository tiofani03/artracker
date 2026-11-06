import type { eventWithTime } from 'rrweb'

type WorkerRequest = {
  id: number
  urls: string[]
}

type WorkerResponse =
  | { id: number; ok: true; events: eventWithTime[] }
  | { id: number; ok: false; error: string }

async function decompressBytes(buf: ArrayBuffer): Promise<string> {
  const bytes = new Uint8Array(buf)
  // Check GZIP magic header (0x1f 0x8b); if CDN/browser already gunzipped via Content-Encoding, decode directly
  if (bytes.byteLength >= 2 && bytes[0] === 0x1f && bytes[1] === 0x8b) {
    const ds = new DecompressionStream('gzip')
    const writer = ds.writable.getWriter()
    void writer.write(bytes as unknown as BufferSource)
    void writer.close()
    const decompressed = await new Response(ds.readable).arrayBuffer()
    return new TextDecoder().decode(decompressed)
  }
  return new TextDecoder().decode(bytes)
}

function extractEvents(parsed: unknown): eventWithTime[] {
  if (Array.isArray(parsed)) {
    return parsed as eventWithTime[]
  }
  if (
    parsed &&
    typeof parsed === 'object' &&
    'events' in parsed &&
    Array.isArray((parsed as { events: unknown }).events)
  ) {
    return (parsed as { events: eventWithTime[] }).events
  }
  return []
}

self.onmessage = async (ev: MessageEvent<WorkerRequest>) => {
  const { id, urls } = ev.data
  try {
    const chunkEventArrays = await Promise.all(
      urls.map(async (url) => {
        const res = await fetch(url)
        if (!res.ok) {
          throw new Error(`Chunk fetch failed (${res.status} ${res.statusText})`)
        }
        const buf = await res.arrayBuffer()
        const jsonText = await decompressBytes(buf)
        return extractEvents(JSON.parse(jsonText))
      }),
    )

    const merged = chunkEventArrays.flat()
    merged.sort((a, b) => a.timestamp - b.timestamp)

    const msg: WorkerResponse = { id, ok: true, events: merged }
    self.postMessage(msg)
  } catch (err) {
    const msg: WorkerResponse = {
      id: ev.data.id,
      ok: false,
      error: err instanceof Error ? err.message : 'Failed to load replay chunks',
    }
    self.postMessage(msg)
  }
}
