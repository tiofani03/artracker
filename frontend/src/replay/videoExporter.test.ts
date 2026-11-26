import { describe, expect, it, vi } from 'vitest'
import { exportReplayVideo, getSupportedVideoMimeType } from './videoExporter'

describe('getSupportedVideoMimeType', () => {
  it('detects available video mime types', () => {
    const res = getSupportedVideoMimeType()
    expect(['mp4', 'webm']).toContain(res.extension)
    expect(res.mimeType).toContain('video/')
  })
})

describe('exportReplayVideo', () => {
  it('drives canvas frames into MediaRecorder and yields video blob', async () => {
    let recorderStarted = false
    let recorderStopped = false
    let dataCallback: ((e: { data: Blob }) => void) | null = null
    let stopCallback: (() => void) | null = null

    class MockMediaRecorder {
      static isTypeSupported = vi.fn().mockReturnValue(true)
      state = 'inactive'
      start() {
        recorderStarted = true
        this.state = 'recording'
      }
      stop() {
        recorderStopped = true
        this.state = 'inactive'
        if (dataCallback) {
          dataCallback({ data: new Blob(['fake-video-chunk'], { type: 'video/webm' }) })
        }
        if (stopCallback) {
          stopCallback()
        }
      }
      set ondataavailable(fn: typeof dataCallback) {
        dataCallback = fn
      }
      set onstop(fn: typeof stopCallback) {
        stopCallback = fn
      }
    }

    // @ts-expect-error mock MediaRecorder in test env
    globalThis.MediaRecorder = MockMediaRecorder

    const mockCtx = {
      fillStyle: '',
      strokeStyle: '',
      lineWidth: 1,
      font: '',
      textAlign: '',
      textBaseline: '',
      fillRect: vi.fn(),
      strokeRect: vi.fn(),
      fillText: vi.fn(),
      beginPath: vi.fn(),
      closePath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      quadraticCurveTo: vi.fn(),
      arc: vi.fn(),
      fill: vi.fn(),
      stroke: vi.fn(),
      drawImage: vi.fn(),
      save: vi.fn(),
      restore: vi.fn(),
      translate: vi.fn(),
      scale: vi.fn(),
      measureText: () => ({ width: 50 }),
    }

    const mockCanvas = {
      width: 400,
      height: 800,
      getContext: () => mockCtx,
      captureStream: () => ({}),
    }

    const origDoc = globalThis.document
    // @ts-expect-error mock document.createElement for canvas
    globalThis.document = {
      createElement: (tag: string) => (tag === 'canvas' ? mockCanvas : {}),
    }

    try {
      const result = await exportReplayVideo({
        mode: 'mobile',
        startAbsMs: 1000,
        endAbsMs: 1400,
        fps: 5,
        width: 400,
        height: 800,
        mobileFrames: [{ ts: 1000, screen: 'Home', viewTree: { cls: 'UIView' } }],
        mobileTouches: [],
      })

      expect(recorderStarted).toBe(true)
      expect(recorderStopped).toBe(true)
      expect(result.blob).toBeInstanceOf(Blob)
      expect(result.blob.size).toBeGreaterThan(0)
    } finally {
      globalThis.document = origDoc
    }
  })
})
