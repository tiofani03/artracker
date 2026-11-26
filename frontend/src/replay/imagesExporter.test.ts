import { describe, expect, it } from 'vitest'
import { exportImagesZip } from './imagesExporter'
import type { ReplaySessionItem } from '../api'

describe('exportImagesZip', () => {
  it('extracts base64 images from screen chunks into zip files with manifest', async () => {
    const session: ReplaySessionItem = {
      id: 'session-screen-test',
      project_id: 1,
      user_id: 'alice@example.com',
      environment: 'production',
      start_time: '2026-11-26T10:00:00Z',
      end_time: '2026-11-26T10:05:00Z',
      duration_ms: 300000,
      chunk_count: 1,
      event_count: 2,
      total_size_bytes: 1000,
      has_error: false,
      has_rage_click: false,
      has_dead_click: false,
      storage_prefix: '',
      created_at: '2026-11-26T10:00:00Z',
      updated_at: '2026-11-26T10:05:00Z',
      has_replay: true,
    }

    // btoa("fake-jpeg-content") = "ZmFrZS1qcGVnLWNvbnRlbnQ="
    const b64Data = btoa('fake-jpeg-content')
    const payload0 = {
      format: 'artracker-screen-v1',
      platform: 'android',
      seq: 0,
      frames: [
        { ts: 1000, screen: 'Home', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg', data: b64Data },
        { ts: 3000, screen: 'Settings', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg', data: b64Data },
      ],
      touches: [],
    }

    const payloadsMap = new Map<number, unknown>()
    payloadsMap.set(0, payload0)

    const screenFrames = [
      { ts: 1000, screen: 'Home', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg' as const, chunkSeq: 0, index: 0, masked: 0 },
      { ts: 3000, screen: 'Settings', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg' as const, chunkSeq: 0, index: 1, masked: 0 },
    ]

    const blob = await exportImagesZip({
      session,
      mode: 'screen',
      screenFrames,
      screenPayloadsMap: payloadsMap,
    })

    expect(blob).toBeInstanceOf(Blob)
    expect(blob.type).toBe('application/zip')
    expect(blob.size).toBeGreaterThan(100)
  })
})
