import { describe, expect, it } from 'vitest'
import { exportRawSessionBundle } from './bundleExporter'
import type { ReplaySessionItem } from '../api'

describe('exportRawSessionBundle', () => {
  it('packages session manifest, chunks, timeline, and perf into a zip blob', async () => {
    const session: ReplaySessionItem = {
      id: 'session-12345678',
      project_id: 1,
      user_id: 'alice@example.com',
      environment: 'production',
      start_time: '2026-11-26T10:00:00Z',
      end_time: '2026-11-26T10:05:00Z',
      duration_ms: 300000,
      chunk_count: 2,
      event_count: 10,
      total_size_bytes: 5000,
      has_error: false,
      has_rage_click: false,
      has_dead_click: false,
      storage_prefix: '',
      created_at: '2026-11-26T10:00:00Z',
      updated_at: '2026-11-26T10:05:00Z',
      has_replay: true,
    }

    const range = {
      startAbsMs: 1000,
      endAbsMs: 5000,
      startOffsetMs: 0,
      endOffsetMs: 4000,
    }

    const blob = await exportRawSessionBundle({
      session,
      range,
      timelineEvents: [],
      perf: null,
      chunkPayloads: [
        { seq: 0, payload: { format: 'artracker-screen-v1', seq: 0 } },
        { seq: 1, payload: { format: 'artracker-screen-v1', seq: 1 } },
      ],
    })

    expect(blob).toBeInstanceOf(Blob)
    expect(blob.type).toBe('application/zip')
    expect(blob.size).toBeGreaterThan(100)
  })
})
