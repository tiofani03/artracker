import { describe, expect, it } from 'vitest'
import type { MobileFrame, ReplayModel } from './model'
import { activeFrameAt, activeTouchAt, fallbackNotice, TOUCH_WINDOW_MS } from './playback'

const f = (ts: number, screen: string): MobileFrame => ({ ts, screen, viewTree: { cls: 'UIView' } })

describe('activeFrameAt', () => {
  const frames = [f(1000, 'A'), f(2000, 'B'), f(3000, 'C')]
  it('returns the last frame at or before ts', () => {
    expect(activeFrameAt(frames, 2500)?.screen).toBe('B')
    expect(activeFrameAt(frames, 2000)?.screen).toBe('B')
  })
  it('returns the first frame before the first ts', () => {
    expect(activeFrameAt(frames, 0)?.screen).toBe('A')
  })
  it('returns null for no frames', () => {
    expect(activeFrameAt([], 0)).toBeNull()
  })
})

describe('activeTouchAt', () => {
  const touches = [{ ts: 1000, x: 1, y: 1 }, { ts: 5000, x: 2, y: 2 }]
  it('returns a touch within the window', () => {
    expect(activeTouchAt(touches, 1000 + TOUCH_WINDOW_MS)?.x).toBe(1)
    expect(activeTouchAt(touches, 5000 - TOUCH_WINDOW_MS)?.x).toBe(2)
  })
  it('returns null outside the window', () => {
    expect(TOUCH_WINDOW_MS).toBe(600)
    expect(activeTouchAt(touches, 1000 + TOUCH_WINDOW_MS + 1)).toBeNull()
  })
})

describe('fallbackNotice', () => {
  it('explains each none reason', () => {
    expect(fallbackNotice({ kind: 'none', reason: 'no-chunks' })).toBe(
      'No replay chunks were uploaded for this session.',
    )
    expect(fallbackNotice({ kind: 'none', reason: 'no-visual-data' })).toBe(
      'Chunks contain no DOM snapshot or view hierarchy.',
    )
    expect(fallbackNotice({ kind: 'none', reason: 'load-error', detail: 'boom' })).toBe('boom')
  })
  it('returns null for renderable models', () => {
    const web: ReplayModel = { kind: 'web', events: [], startMs: 0, endMs: 1 }
    expect(fallbackNotice(web)).toBeNull()
  })
})
