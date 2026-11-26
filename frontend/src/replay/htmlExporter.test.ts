import { describe, expect, it } from 'vitest'
import { exportOfflineHtmlPlayer } from './htmlExporter'
import type { ReplaySessionItem } from '../api'

describe('exportOfflineHtmlPlayer', () => {
  it('generates a valid, self-contained HTML document with escaped script payload', async () => {
    const session: ReplaySessionItem = {
      id: 'session-offline-html',
      project_id: 1,
      user_id: 'alice@example.com',
      environment: 'production',
      start_time: '2026-11-26T10:00:00Z',
      end_time: '2026-11-26T10:05:00Z',
      duration_ms: 300000,
      chunk_count: 1,
      event_count: 1,
      total_size_bytes: 1000,
      has_error: false,
      has_rage_click: false,
      has_dead_click: false,
      storage_prefix: '',
      created_at: '2026-11-26T10:00:00Z',
      updated_at: '2026-11-26T10:05:00Z',
      has_replay: true,
    }

    const blob = await exportOfflineHtmlPlayer({
      session,
      mode: 'screen',
      startAbsMs: 1000,
      endAbsMs: 5000,
      timelineEvents: [
        {
          id: '1',
          kind: 'custom',
          name: 'Dangerous </script> Injection Attempt',
          level: 'info',
          message: '',
          session_id: 'session-offline-html',
          user_id: 'alice@example.com',
          environment: 'production',
          release_version: '1',
          event_timestamp: '2026-11-26T10:01:00Z',
          attributes: {},
        },
      ],
      screenFrames: [
        { ts: 1000, screen: 'Home', w: 100, h: 200, vw: 100, vh: 200, mime: 'image/jpeg', chunkSeq: 0, index: 0, masked: 0 },
      ],
      screenTouches: [],
      screenImagesMap: new Map([[1000, 'mock-base64']]),
    })

    expect(blob).toBeInstanceOf(Blob)
    expect(blob.type).toBe('text/html;charset=utf-8')

    const htmlText = await blob.text()
    expect(htmlText).toContain('<!DOCTYPE html>')
    expect(htmlText).toContain('Content-Security-Policy')
    expect(htmlText).toContain('session-offline-html')
    // Ensure dangerous script tag was escaped
    expect(htmlText).not.toContain('</script> Injection Attempt')
    expect(htmlText).toContain('<\\/script> Injection Attempt')
  })
})
