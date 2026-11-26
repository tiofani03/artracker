import type { MobileFrame, TouchPoint, ViewNode } from './model'
import { displayScreenName, nodeKinds, type NodeKind } from './wireframe'
import { parseFrame } from '../components/MobileWireframeRenderer'

export type WireframeCanvasOptions = {
  theme?: 'light' | 'dark'
  width?: number
  height?: number
  scale?: number
}

export type WireframeColors = {
  bg: string
  stageBg: string
  headerBg: string
  headerBorder: string
  textColor: string
  secondaryText: string
  nodeDefaultBg: string
  nodeDefaultBorder: string
  buttonBg: string
  buttonBorder: string
  buttonText: string
  modalBg: string
  modalBorder: string
  cellBg: string
  cellBorder: string
  toolbarBg: string
  toolbarBorder: string
  touchColor: string
}

export const THEME_DARK: WireframeColors = {
  bg: '#090d16',
  stageBg: '#0a0f1d',
  headerBg: '#131b2e',
  headerBorder: 'rgba(255, 255, 255, 0.08)',
  textColor: '#f1f5f9',
  secondaryText: '#94a3b8',
  nodeDefaultBg: 'rgba(255, 255, 255, 0.02)',
  nodeDefaultBorder: 'rgba(255, 255, 255, 0.06)',
  buttonBg: 'rgba(59, 130, 246, 0.15)',
  buttonBorder: 'rgba(59, 130, 246, 0.35)',
  buttonText: '#60a5fa',
  modalBg: 'rgba(15, 23, 42, 0.95)',
  modalBorder: 'rgba(255, 255, 255, 0.16)',
  cellBg: 'rgba(255, 255, 255, 0.035)',
  cellBorder: 'rgba(255, 255, 255, 0.06)',
  toolbarBg: 'rgba(15, 23, 42, 0.9)',
  toolbarBorder: 'rgba(255, 255, 255, 0.08)',
  touchColor: '#38bdf8',
}

export const THEME_LIGHT: WireframeColors = {
  bg: '#f2f2f7',
  stageBg: '#ffffff',
  headerBg: '#ffffff',
  headerBorder: 'rgba(0, 0, 0, 0.08)',
  textColor: '#111827',
  secondaryText: '#6b7280',
  nodeDefaultBg: 'rgba(0, 0, 0, 0.02)',
  nodeDefaultBorder: 'rgba(0, 0, 0, 0.06)',
  buttonBg: 'rgba(0, 122, 255, 0.08)',
  buttonBorder: 'rgba(0, 122, 255, 0.28)',
  buttonText: '#007aff',
  modalBg: 'rgba(255, 255, 255, 0.97)',
  modalBorder: 'rgba(0, 0, 0, 0.1)',
  cellBg: '#ffffff',
  cellBorder: 'rgba(0, 0, 0, 0.08)',
  toolbarBg: 'rgba(249, 249, 251, 0.94)',
  toolbarBorder: 'rgba(0, 0, 0, 0.08)',
  touchColor: '#0284c7',
}

/**
 * Draws a rounded rectangle path on Canvas context.
 */
function roundRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  radius: number,
): void {
  const r = Math.min(radius, w / 2, h / 2)
  ctx.beginPath()
  ctx.moveTo(x + r, y)
  ctx.lineTo(x + w - r, y)
  ctx.quadraticCurveTo(x + w, y, x + w, y + r)
  ctx.lineTo(x + w, y + h - r)
  ctx.quadraticCurveTo(x + w, y + h, x + w - r, y + h)
  ctx.lineTo(x + r, y + h)
  ctx.quadraticCurveTo(x, y + h, x, y + h - r)
  ctx.lineTo(x, y + r)
  ctx.quadraticCurveTo(x, y, x + r, y)
  ctx.closePath()
}

/**
 * Renders a complete mobile wireframe frame to a 2D Canvas context.
 */
export function renderWireframeToCanvas(
  ctx: CanvasRenderingContext2D,
  frame: MobileFrame | null,
  touch: TouchPoint | null,
  options: WireframeCanvasOptions = {},
): void {
  const theme = options.theme === 'light' ? THEME_LIGHT : THEME_DARK
  const width = options.width ?? 402
  const height = options.height ?? 874

  // Clear background
  ctx.fillStyle = theme.bg
  ctx.fillRect(0, 0, width, height)

  const bannerHeight = 36
  // Render Top Screen Banner
  ctx.fillStyle = theme.headerBg
  ctx.fillRect(0, 0, width, bannerHeight)
  ctx.strokeStyle = theme.headerBorder
  ctx.lineWidth = 1
  ctx.beginPath()
  ctx.moveTo(0, bannerHeight)
  ctx.lineTo(width, bannerHeight)
  ctx.stroke()

  // Screen indicator dot
  ctx.fillStyle = '#38bdf8'
  ctx.beginPath()
  ctx.arc(14, bannerHeight / 2, 3.5, 0, Math.PI * 2)
  ctx.fill()

  // Screen Title
  const screenTitle = frame?.screen ? displayScreenName(frame.screen) : 'App Screen'
  ctx.fillStyle = theme.textColor
  ctx.font = 'bold 12px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
  ctx.textAlign = 'left'
  ctx.textBaseline = 'middle'
  ctx.fillText(screenTitle, 26, bannerHeight / 2)

  // Presentation / Masking Badges
  let rightOffset = width - 12
  if (frame?.maskedCount && frame.maskedCount > 0) {
    ctx.fillStyle = 'rgba(168, 85, 247, 0.2)'
    ctx.strokeStyle = '#c084fc'
    const badgeText = `🛡️ ${frame.maskedCount}`
    ctx.font = 'bold 10px -apple-system, BlinkMacSystemFont, sans-serif'
    const tw = ctx.measureText(badgeText).width + 12
    rightOffset -= tw
    roundRect(ctx, rightOffset, bannerHeight / 2 - 8, tw, 16, 3)
    ctx.fill()
    ctx.stroke()
    ctx.fillStyle = '#c084fc'
    ctx.fillText(badgeText, rightOffset + 6, bannerHeight / 2)
    rightOffset -= 6
  }

  if (frame?.presentation && frame.presentation !== 'push') {
    const presText = frame.presentation.toUpperCase()
    ctx.font = 'bold 9px -apple-system, BlinkMacSystemFont, sans-serif'
    const tw = ctx.measureText(presText).width + 10
    rightOffset -= tw
    ctx.fillStyle = 'rgba(14, 165, 233, 0.2)'
    ctx.strokeStyle = '#38bdf8'
    roundRect(ctx, rightOffset, bannerHeight / 2 - 8, tw, 16, 3)
    ctx.fill()
    ctx.stroke()
    ctx.fillStyle = '#38bdf8'
    ctx.fillText(presText, rightOffset + 5, bannerHeight / 2)
  }

  // View hierarchy stage
  const stageTop = bannerHeight
  const stageHeight = height - bannerHeight
  ctx.fillStyle = theme.stageBg
  ctx.fillRect(0, stageTop, width, stageHeight)

  if (!frame || !frame.viewTree) {
    ctx.fillStyle = theme.secondaryText
    ctx.font = '12px -apple-system, BlinkMacSystemFont, sans-serif'
    ctx.textAlign = 'center'
    ctx.fillText('Awaiting view hierarchy snapshot…', width / 2, stageTop + stageHeight / 2)
    return
  }

  // Parse root frame to calculate scale
  const rootParsed = parseFrame(frame.viewTree.frame)
  const rootW = rootParsed && rootParsed[2] > 100 ? rootParsed[2] : width
  const rootH = rootParsed && rootParsed[3] > 100 ? rootParsed[3] : stageHeight
  const scale = Math.min(width / rootW, stageHeight / rootH)
  const offsetX = Math.max(0, (width - rootW * scale) / 2)
  const offsetY = stageTop + Math.max(0, (stageHeight - rootH * scale) / 2)

  ctx.save()
  ctx.translate(offsetX, offsetY)
  ctx.scale(scale, scale)

  // Recursive node renderer
  const renderNode = (node: ViewNode, inherited: ReadonlySet<NodeKind> = new Set()) => {
    if (node.hidden) return
    const f = parseFrame(node.frame)
    if (!f) return
    const [x, y, w, h] = f
    if (w <= 0 && h <= 0 && (!node.children || node.children.length === 0)) return

    const cls = node.cls || 'UIView'
    const kinds = nodeKinds(cls, inherited)
    const childInherited = new Set(inherited)
    for (const k of kinds) childInherited.add(k)

    const isButton = kinds.has('button')
    const isModal = kinds.has('modal')
    const isToolbar = kinds.has('toolbar')
    const isCell = kinds.has('cell')
    const isSeparator = kinds.has('separator') || (h <= 2 && w > 20)
    const isText = kinds.has('text')
    const isImage = kinds.has('image')

    // Determine colors
    if (isModal) {
      ctx.fillStyle = theme.modalBg
      ctx.strokeStyle = theme.modalBorder
      roundRect(ctx, x, y, w, h, 14)
      ctx.fill()
      ctx.stroke()
    } else if (isButton) {
      ctx.fillStyle = theme.buttonBg
      ctx.strokeStyle = theme.buttonBorder
      roundRect(ctx, x, y, w, h, 6)
      ctx.fill()
      ctx.stroke()
    } else if (isToolbar) {
      ctx.fillStyle = theme.toolbarBg
      ctx.strokeStyle = theme.toolbarBorder
      ctx.fillRect(x, y, w, h)
      ctx.strokeRect(x, y, w, h)
    } else if (isCell) {
      ctx.fillStyle = theme.cellBg
      ctx.strokeStyle = theme.cellBorder
      ctx.fillRect(x, y, w, h)
      ctx.beginPath()
      ctx.moveTo(x, y + h)
      ctx.lineTo(x + w, y + h)
      ctx.stroke()
    } else if (isSeparator) {
      ctx.fillStyle = theme.nodeDefaultBorder
      ctx.fillRect(x, y, w, Math.max(1, h))
    }

    // Text content
    const textContent = typeof node.text === 'string' && node.text.trim() ? node.text.trim() : null
    if (textContent) {
      ctx.fillStyle = isButton ? theme.buttonText : theme.textColor
      ctx.font = '13px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.textAlign = 'left'
      ctx.textBaseline = 'middle'
      ctx.fillText(textContent, x + 6, y + h / 2, w - 12)
    } else if (isText && (!node.children || node.children.length === 0)) {
      // Skeleton line
      ctx.fillStyle = theme.nodeDefaultBorder
      roundRect(ctx, x + 4, y + h / 2 - 4, Math.min(w - 8, Math.max(30, w * 0.7)), 8, 3)
      ctx.fill()
    } else if (isImage && (!node.children || node.children.length === 0)) {
      ctx.fillStyle = theme.nodeDefaultBg
      ctx.strokeStyle = theme.nodeDefaultBorder
      roundRect(ctx, x + 2, y + 2, w - 4, h - 4, 4)
      ctx.fill()
      ctx.stroke()
    }

    // Children
    if (node.children) {
      for (const child of node.children) {
        renderNode(child, childInherited)
      }
    }
  }

  renderNode(frame.viewTree)

  // Touch gesture ripple
  if (touch) {
    ctx.fillStyle = theme.touchColor
    ctx.beginPath()
    ctx.arc(touch.x, touch.y, 4, 0, Math.PI * 2)
    ctx.fill()

    ctx.strokeStyle = theme.touchColor
    ctx.lineWidth = 2
    ctx.beginPath()
    ctx.arc(touch.x, touch.y, 14, 0, Math.PI * 2)
    ctx.stroke()
  }

  ctx.restore()
}
