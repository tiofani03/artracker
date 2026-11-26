import { describe, expect, it, vi } from 'vitest'
import { renderWireframeToCanvas } from './wireframeCanvas'
import type { MobileFrame } from './model'

describe('renderWireframeToCanvas', () => {
  it('renders wireframe view tree, banner, and touches into canvas context', () => {
    const fillRectMock = vi.fn()
    const fillTextMock = vi.fn()
    const strokeMock = vi.fn()
    const fillMock = vi.fn()
    const arcMock = vi.fn()

    const mockCtx = {
      fillStyle: '',
      strokeStyle: '',
      lineWidth: 1,
      font: '',
      textAlign: '',
      textBaseline: '',
      fillRect: fillRectMock,
      strokeRect: vi.fn(),
      fillText: fillTextMock,
      stroke: strokeMock,
      fill: fillMock,
      beginPath: vi.fn(),
      closePath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      quadraticCurveTo: vi.fn(),
      arc: arcMock,
      measureText: (text: string) => ({ width: text.length * 8 }),
      save: vi.fn(),
      restore: vi.fn(),
      translate: vi.fn(),
      scale: vi.fn(),
    } as unknown as CanvasRenderingContext2D

    const sampleFrame: MobileFrame = {
      ts: 1000,
      screen: 'CheckoutViewController',
      presentation: 'modal',
      maskedCount: 2,
      viewTree: {
        cls: 'UIView',
        frame: [0, 0, 400, 800],
        children: [
          {
            cls: 'UIButton',
            frame: [20, 100, 200, 44],
            text: 'Pay Now',
          },
        ],
      },
    }

    renderWireframeToCanvas(mockCtx, sampleFrame, { ts: 1050, x: 50, y: 120 }, { theme: 'light' })

    // Verify background fill was called
    expect(fillRectMock).toHaveBeenCalled()
    // Verify screen title and button text were rendered
    expect(fillTextMock).toHaveBeenCalledWith('CheckoutViewController', expect.any(Number), expect.any(Number))
    expect(fillTextMock).toHaveBeenCalledWith('Pay Now', expect.any(Number), expect.any(Number), expect.any(Number))
    // Verify touch ripple arc was drawn
    expect(arcMock).toHaveBeenCalledWith(50, 120, 4, 0, Math.PI * 2)
  })
})
