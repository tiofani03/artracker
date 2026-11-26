import { describe, expect, it, vi } from 'vitest'
import { runReplayExport } from './exportController'
import type { ReplaySessionItem } from '../api'

// Mock saveExportFile
vi.mock('../utils/saveExportFile', () => ({
  saveExportFile: vi.fn().mockResolvedValue(undefined),
}))

describe('runReplayExport', () => {
  it('orchestrates raw bundle export and calls saveExportFile', async () => {
    const { saveExportFile } = await import('../utils/saveExportFile')

    const session: ReplaySessionItem = {
      id: 'session-orchestrator-test',
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

    const progressLogs: string[] = []

    await runReplayExport({
      options: {
        format: 'raw-bundle',
        rangeMode: 'full',
        clipStartMs: 0,
        clipEndMs: 300000,
      },
      session,
      model: { kind: 'none', reason: 'no-visual-data' },
      chunks: [],
      timelineEvents: [],
      perf: null,
      baseTimestampMs: 1000,
      durationMs: 300000,
      refreshUrls: vi.fn(),
      onProgress: (p) => progressLogs.push(p.message),
      signal: new AbortController().signal,
    })

    expect(saveExportFile).toHaveBeenCalledWith(
      expect.any(Blob),
      expect.stringContaining('session_session-orch_bundle.zip'),
    )
    expect(progressLogs).toContain('Export complete!')
  })
})
