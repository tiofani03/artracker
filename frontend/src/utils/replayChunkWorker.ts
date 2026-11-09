import { fetchChunkPayloads } from '../replay/parseChunk'

type WorkerRequest = { id: number; urls: string[] }

type WorkerResponse =
  | { id: number; ok: true; payloads: unknown[]; failed: number }
  | { id: number; ok: false; error: string }

self.onmessage = async (ev: MessageEvent<WorkerRequest>) => {
  const { id, urls } = ev.data
  let msg: WorkerResponse
  try {
    const { payloads, failed } = await fetchChunkPayloads(urls)
    msg = { id, ok: true, payloads, failed }
  } catch (err) {
    msg = { id, ok: false, error: err instanceof Error ? err.message : 'Failed to load replay chunks' }
  }
  self.postMessage(msg)
}
